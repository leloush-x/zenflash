-- v2 002: model catalog + pricing caches. Additive, idempotent.
CREATE TABLE IF NOT EXISTS model_catalog (
  slot TEXT PRIMARY KEY,
  payload JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
