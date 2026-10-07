-- v2 005: per-model deprecated flags (admin kill-switch). Additive, idempotent.
CREATE TABLE IF NOT EXISTS model_flags (
  model_id TEXT PRIMARY KEY,
  deprecated BOOLEAN NOT NULL DEFAULT FALSE,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
