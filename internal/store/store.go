// Package store implements optional Postgres durability for v2.
package store

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	_embed "embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"zenflash-llm/internal/config"
)

//go:embed migrations/*.sql
var migrationFS _embed.FS

// Store is nil-safe: a nil *Store disables persistence without behavior change.
type Store struct {
	pool   *pgxpool.Pool
	logger *slog.Logger

	mu        sync.RWMutex
	keyHash   map[string]struct{}
	keyOrder  []string
	flagSet   map[string]struct{}
	lastSync  time.Time
	slotCount int

	statsCh chan statEvent
	quit    chan struct{}
	wg      sync.WaitGroup
}

type statEvent struct {
	Model      string
	Tier       string
	Success    bool
	DurationMS int
	Input      int64
	Output     int64
}

// HashKey returns the stable hex hash stored for an API key.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// DisplayKey returns the UI-safe suffix (mirrors config.MaskValue behavior).
func DisplayKey(key string) string {
	r := []rune(key)
	if len(r) <= 5 {
		return string(r)
	}
	return "\u2022\u2022\u2022\u2022" + string(r[len(r)-5:])
}

// Open connects to Postgres via databaseURL only. Empty URL returns (nil, nil).
// It retries for ~30s with backoff for Neon autosuspend, enforces TLS,
// reports unsupported params, runs embedded migrations under an advisory lock.
func Open(ctx context.Context, databaseURL string, logger *slog.Logger) (*Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, nil
	}
	u, err := url.Parse(strings.TrimSpace(databaseURL))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("DATABASE_URL must be a valid URL with a host")
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, fmt.Errorf("DATABASE_URL must use postgres:// or postgresql:// scheme")
	}
	q := u.Query()
	if ssl := strings.ToLower(strings.TrimSpace(q.Get("sslmode"))); ssl == "disable" || ssl == "allow" {
		return nil, fmt.Errorf("DATABASE_URL must not weaken TLS (sslmode=%q is forbidden)", ssl)
	}
	reportUnsupportedParams(u, logger)
	cfg, err := pgxpool.ParseConfig(strings.TrimSpace(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	// PgBouncer transaction mode: no prepared statements or session state.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	cfg.MaxConns = 8
	cfg.MinConns = 0
	cfg.MaxConnLifetime = 25 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	deadline := time.Now().Add(config.DBConnectRetryBudget)
	backoff := 500 * time.Millisecond
	var pool *pgxpool.Pool
	for {
		cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		pool, err = pgxpool.NewWithConfig(cctx, cfg)
		cancel()
		if err == nil {
			pctx, pcancel := context.WithTimeout(ctx, 8*time.Second)
			err = pool.Ping(pctx)
			pcancel()
		}
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("postgres connect after ~30s backoff: %w", err)
		}
		if logger != nil {
			logger.Warn("postgres connect failed; retrying", "error", err, "backoff", backoff.String())
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 8*time.Second {
			backoff = 8 * time.Second
		}
	}
	s := &Store{pool: pool, logger: logger, keyHash: map[string]struct{}{}, statsCh: make(chan statEvent, 1024), quit: make(chan struct{})}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	s.wg.Add(2)
	go s.keyRefreshLoop()
	go s.statsLoop()
	return s, nil
}

func reportUnsupportedParams(u *url.URL, logger *slog.Logger) {
	if logger == nil {
		return
	}
	known := map[string]struct{}{
		"sslmode": {}, "sslrootcert": {}, "sslcert": {}, "sslkey": {},
		"channel_binding": {}, "connect_timeout": {}, "application_name": {},
		"search_path": {}, "options": {}, "pool_max_conns": {}, "pool_min_conns": {},
		"host": {}, "port": {}, "dbname": {}, "user": {}, "password": {},
	}
	q := u.Query()
	var unknown []string
	for k := range q {
		if _, ok := known[strings.ToLower(k)]; !ok {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		logger.Warn("DATABASE_URL contains unsupported params ignored by v2", "params", strings.Join(unknown, ","))
	}
}

func (s *Store) migrate(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return nil
	}
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, "migrations/"+e.Name())
		}
	}
	sort.Strings(files)
	txCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return err
	}
	defer tx.Rollback(txCtx)
	var locked bool
	if err := tx.QueryRow(txCtx, "SELECT pg_try_advisory_xact_lock(hashtext('zenflash_v2_migrations'))").Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("could not acquire migration advisory lock")
	}
	if _, err := tx.Exec(txCtx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for i, f := range files {
		version := i + 1
		var exists bool
		if err := tx.QueryRow(txCtx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		data, err := migrationFS.ReadFile(f)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(txCtx, string(data)); err != nil {
			return fmt.Errorf("migration %s: %w", f, err)
		}
		if _, err := tx.Exec(txCtx, `INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, version); err != nil {
			return err
		}
	}
	return tx.Commit(txCtx)
}

// SeedKeys inserts today's file keys on first boot (idempotent, hashed).
func (s *Store) SeedKeys(ctx context.Context, keys []string) error {
	if s == nil || s.pool == nil || len(keys) == 0 {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var count int
	if err := s.pool.QueryRow(cctx, `SELECT COUNT(*) FROM server_keys`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		_, _ = s.pool.Exec(cctx, `INSERT INTO server_keys (key_hash, display) VALUES ($1,$2) ON CONFLICT DO NOTHING`, HashKey(k), DisplayKey(k))
	}
	return nil
}

func (s *Store) keyRefreshLoop() {
	defer s.wg.Done()
	t := time.NewTicker(config.AuthKeyTTL)
	defer t.Stop()
	s.refreshKeys()
	s.refreshFlags()
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
			s.refreshKeys()
			s.refreshFlags()
		}
	}
}

func (s *Store) refreshKeys() {
	if s == nil || s.pool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT key_hash FROM server_keys`)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("auth key refresh failed; keep serving from memory", "error", err)
		}
		return
	}
	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err == nil && h != "" {
			hashes = append(hashes, h)
		}
	}
	rows.Close()
	m := make(map[string]struct{}, len(hashes))
	for _, h := range hashes {
		m[h] = struct{}{}
	}
	s.mu.Lock()
	s.keyHash = m
	s.keyOrder = hashes
	s.mu.Unlock()
}

// ValidKey reports whether candidate is authorized. DB hashes are checked
// first; file keys remain valid so behavior never regresses. Never blocks on DB.
func (s *Store) ValidKey(candidate string, fileKeys []string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	if s != nil {
		s.mu.RLock()
		_, ok := s.keyHash[HashKey(candidate)]
		s.mu.RUnlock()
		if ok {
			return true
		}
	}
	for _, k := range fileKeys {
		if len(candidate) == len(k) && subtle.ConstantTimeCompare([]byte(candidate), []byte(k)) == 1 {
			return true
		}
	}
	return false
}

// Status reports durable-store health for the dashboard. Never exposes secrets.
type Status struct {
	Enabled   bool      `json:"enabled"`
	Keys      int       `json:"keys_cached"`
	LastSync  time.Time `json:"last_sync,omitempty"`
	Slots     int       `json:"slots"`
	Reachable bool      `json:"reachable"`
}

// Status snapshots store health without touching request paths.
func (s *Store) Status(ctx context.Context) Status {
	if s == nil || s.pool == nil {
		return Status{Enabled: false}
	}
	s.mu.RLock()
	keys, last, slots := len(s.keyHash), s.lastSync, s.slotCount
	s.mu.RUnlock()
	reachable := true
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.pool.Ping(cctx); err != nil {
		reachable = false
	}
	return Status{Enabled: true, Keys: keys, LastSync: last, Slots: slots, Reachable: reachable}
}

// HasDBKeys reports whether the DB holds any key (for status).
func (s *Store) HasDBKeys() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.keyHash) > 0
}

// Close stops background work. Nil-safe.
func (s *Store) Close() {
	if s == nil {
		return
	}
	select {
	case <-s.quit:
	default:
		close(s.quit)
	}
	s.wg.Wait()
	if s.pool != nil {
		s.pool.Close()
	}
}

// RecordStat enqueues async stats; never blocks or fails the request.
func (s *Store) RecordStat(ev statEvent) {
	if s == nil || s.pool == nil {
		return
	}
	select {
	case s.statsCh <- ev:
	default:
	}
}

func (s *Store) statsLoop() {
	defer s.wg.Done()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	var buf []statEvent
	flush := func() {
		if len(buf) == 0 || s.pool == nil {
			buf = buf[:0]
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		for _, ev := range buf {
			_, _ = s.pool.Exec(ctx, `INSERT INTO request_events (model, tier, success, duration_ms) VALUES ($1,$2,$3,$4)`, ev.Model, ev.Tier, ev.Success, ev.DurationMS)
		}
		if len(buf) > 0 {
			_, _ = s.pool.Exec(ctx, `INSERT INTO request_stats_daily (day, requests) VALUES (CURRENT_DATE, $1) ON CONFLICT (day) DO UPDATE SET requests = request_stats_daily.requests + EXCLUDED.requests, updated_at = now()`, len(buf))
		}
		buf = buf[:0]
	}
	for {
		select {
		case <-s.quit:
			flush()
			return
		case ev := <-s.statsCh:
			buf = append(buf, ev)
			if len(buf) >= 100 {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

// GetSetting fetches a JSON setting best-effort; empty means absent.
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	if s == nil || s.pool == nil {
		return "", nil
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var v string
	err := s.pool.QueryRow(cctx, `SELECT value::text FROM app_settings WHERE key=$1`, key).Scan(&v)
	if err != nil {
		return "", nil
	}
	return v, nil
}

// SetSetting upserts a JSON setting best-effort (async-safe, never fails requests).
func (s *Store) SetSetting(ctx context.Context, key, jsonValue string) {
	if s == nil || s.pool == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, _ = s.pool.Exec(cctx, `INSERT INTO app_settings (key, value) VALUES ($1, $2::jsonb) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`, key, jsonValue)
}

// CatalogSave stores a catalog blob best-effort.
func (s *Store) CatalogSave(ctx context.Context, slot, jsonValue string) {
	if s == nil || s.pool == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, _ = s.pool.Exec(cctx, `INSERT INTO model_catalog (slot, payload) VALUES ($1, $2::jsonb) ON CONFLICT (slot) DO UPDATE SET payload=EXCLUDED.payload, updated_at=now()`, slot, jsonValue)
}

// CatalogLoad loads a catalog blob best-effort.
func (s *Store) CatalogLoad(ctx context.Context, slot string) string {
	if s == nil || s.pool == nil {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var v string
	if err := s.pool.QueryRow(cctx, `SELECT payload::text FROM model_catalog WHERE slot=$1`, slot).Scan(&v); err != nil {
		return ""
	}
	return v
}
