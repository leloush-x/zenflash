package store

import (
	"context"
	"time"
)

// refreshFlags reloads the deprecated-model set. Failures keep the previous
// set so listings never flap on DB trouble.
func (s *Store) refreshFlags() {
	if s == nil || s.pool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT model_id FROM model_flags WHERE deprecated`)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("model flag refresh failed; keep serving from memory", "error", err)
		}
		return
	}
	m := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil && id != "" {
			m[id] = struct{}{}
		}
	}
	rows.Close()
	s.mu.Lock()
	s.flagSet = m
	s.mu.Unlock()
}

// Deprecated reports whether a raw model ID was switched off by the admin.
// Nil-safe and never touches the DB: reads the background-refreshed cache.
func (s *Store) Deprecated(id string) bool {
	if s == nil || id == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.flagSet[id]
	return ok
}

// SetDeprecated switches a raw model ID off (or back on). The cache updates
// immediately so the toggle takes effect everywhere without waiting for TTL.
func (s *Store) SetDeprecated(ctx context.Context, id string, off bool) error {
	if s == nil || s.pool == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if _, err := s.pool.Exec(cctx, `INSERT INTO model_flags (model_id, deprecated) VALUES ($1,$2) ON CONFLICT (model_id) DO UPDATE SET deprecated=EXCLUDED.deprecated, updated_at=now()`, id, off); err != nil {
		return err
	}
	s.mu.Lock()
	if s.flagSet == nil {
		s.flagSet = map[string]struct{}{}
	}
	if off {
		s.flagSet[id] = struct{}{}
	} else {
		delete(s.flagSet, id)
	}
	s.mu.Unlock()
	return nil
}

// FlagList returns every flagged raw model ID the admin has ever toggled.
func (s *Store) FlagList(ctx context.Context) []Flag {
	if s == nil || s.pool == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	rows, err := s.pool.Query(cctx, `SELECT model_id, deprecated, updated_at FROM model_flags ORDER BY updated_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Flag
	for rows.Next() {
		var f Flag
		if err := rows.Scan(&f.ID, &f.Deprecated, &f.UpdatedAt); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// Flag is one admin model switch.
type Flag struct {
	ID         string    `json:"id"`
	Deprecated bool      `json:"deprecated"`
	UpdatedAt  time.Time `json:"updated_at"`
}
