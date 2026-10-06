-- v2 004: async stats. Additive, idempotent.
CREATE TABLE IF NOT EXISTS request_stats_daily (
  day DATE PRIMARY KEY,
  requests BIGINT NOT NULL DEFAULT 0,
  input_tokens BIGINT NOT NULL DEFAULT 0,
  output_tokens BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS request_events (
  id BIGSERIAL PRIMARY KEY,
  ts TIMESTAMPTZ NOT NULL DEFAULT now(),
  model TEXT NOT NULL DEFAULT '',
  tier TEXT NOT NULL DEFAULT '',
  success BOOLEAN NOT NULL DEFAULT TRUE,
  duration_ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS request_events_ts_idx ON request_events (ts);
