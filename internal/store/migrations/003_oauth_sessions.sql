-- v2 003: oauth tokens + webui sessions. Additive, idempotent.
CREATE TABLE IF NOT EXISTS oauth_tokens (
  provider TEXT NOT NULL,
  account_id TEXT NOT NULL,
  payload JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (provider, account_id)
);
CREATE TABLE IF NOT EXISTS webui_sessions (
  token_hash TEXT PRIMARY KEY,
  payload JSONB NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS webui_sessions_expires_idx ON webui_sessions (expires_at);
