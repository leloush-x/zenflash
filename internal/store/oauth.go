package store

import (
	"context"
	"time"
)

// OAuthSave replaces all stored rows for provider with the given payloads.
// Each payload is raw JSON for one account; accountIDs must align with them.
// Best-effort: failures are silent so token refreshes never break on DB loss.
func (s *Store) OAuthSave(ctx context.Context, provider string, accountIDs []string, payloads [][]byte) {
	if s == nil || s.pool == nil || provider == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(cctx)
	if err != nil {
		return
	}
	defer tx.Rollback(cctx)
	if _, err := tx.Exec(cctx, `DELETE FROM oauth_tokens WHERE provider=$1`, provider); err != nil {
		return
	}
	for i, raw := range payloads {
		id := ""
		if i < len(accountIDs) {
			id = accountIDs[i]
		}
		if id == "" {
			continue
		}
		if _, err := tx.Exec(cctx, `INSERT INTO oauth_tokens (provider, account_id, payload) VALUES ($1,$2,$3::jsonb) ON CONFLICT (provider, account_id) DO UPDATE SET payload=EXCLUDED.payload, updated_at=now()`, provider, id, string(raw)); err != nil {
			return
		}
	}
	_ = tx.Commit(cctx)
}

// OAuthList returns stored raw account payloads for provider, oldest first.
// Empty means absent or unreachable; callers fall back to files.
func (s *Store) OAuthList(ctx context.Context, provider string) [][]byte {
	if s == nil || s.pool == nil || provider == "" {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	rows, err := s.pool.Query(cctx, `SELECT payload::text FROM oauth_tokens WHERE provider=$1 ORDER BY updated_at`, provider)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err == nil && v != "" {
			out = append(out, []byte(v))
		}
	}
	return out
}

// SessionPut upserts one dashboard session row. Best-effort.
func (s *Store) SessionPut(ctx context.Context, tokenHash, payload string, expires time.Time) {
	if s == nil || s.pool == nil || tokenHash == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, _ = s.pool.Exec(cctx, `INSERT INTO webui_sessions (token_hash, payload, expires_at) VALUES ($1,$2::jsonb,$3) ON CONFLICT (token_hash) DO UPDATE SET payload=EXCLUDED.payload, expires_at=EXCLUDED.expires_at`, tokenHash, payload, expires.UTC())
}

// SessionDelete removes one dashboard session row. Best-effort.
func (s *Store) SessionDelete(ctx context.Context, tokenHash string) {
	if s == nil || s.pool == nil || tokenHash == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, _ = s.pool.Exec(cctx, `DELETE FROM webui_sessions WHERE token_hash=$1`, tokenHash)
}

// SessionList returns live rows as token-hash to raw payload pairs.
func (s *Store) SessionList(ctx context.Context) map[string]string {
	if s == nil || s.pool == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	_, _ = s.pool.Exec(cctx, `DELETE FROM webui_sessions WHERE expires_at < now()`)
	rows, err := s.pool.Query(cctx, `SELECT token_hash, payload::text FROM webui_sessions WHERE expires_at >= now()`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var h, v string
		if err := rows.Scan(&h, &v); err == nil && h != "" {
			out[h] = v
		}
	}
	return out
}
