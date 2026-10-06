# v2 behavior-preserving refactor

## Moved (old → new path)
- Env/defaults/constants → `internal/config/central.go` (typed `EnvConfig`, loaded once via `LoadEnvOnce`, validated at startup).
- WEBUI_USERNAME/PASSWORD reads: `cmd/zenflash-llm/main.go:os.Getenv` → `config.LoadEnvOnce()`.
- ANTIGRAVITY_OAUTH_CLIENTS/KEY reads: `internal/antigravity/antigravity.go:os.Getenv` → `config.LoadEnvOnce()`.
- Windows check: `internal/cline/cline/auth.go:os.Getenv("OS")` → `config.IsWindows()`.
- JSON defaults in `internal/config/config.go:Load` → central constants (`DefaultAPIListen`, `DefaultUpstreamZen/Go`, retry/models/perf/logging/webui defaults, `DefaultPreferTier`).
- Timeouts/limits in `cmd/zenflash-llm/main.go` → `config.ReadHeaderTimeout/IdleTimeout/ShutdownTimeout/ClineReadyTimeout/ClineReadyInterval/HTTPClientTimeout`, flag defaults → `config.DefaultClineHost/Port`.
- Auth headers in `internal/gateway/gateway.go` → `config.HeaderAPIKey/HeaderAuthorization`.
- New durability: `internal/store/{store,importer}.go`, `internal/store/migrations/001-004.sql`, key cache wiring in `internal/gateway/{gateway,runtime}.go`, startup open/seed/import in `cmd/zenflash-llm/main.go`.
- Package docs: `internal/cline/app/doc.go`, `internal/cline/kit/doc.go`, `internal/cline/cline/doc.go`, `internal/store/doc.go` (other packages already had one-line `// Package` docs).
- Ops: `scripts/baseline_routes.sh`, `scripts/check-no-secrets.sh`, `scripts/test-throwaway-schema.sh`, `.env.example`, `docs/v2-baseline.md`, README code map.

## Removed (+proof)
- None. No dead code deleted. `go vet` clean; duplicates (compat route aliases in `internal/cline/app/proxy.go`, `internal/admin/openai_alias.go`, legacy admin path aliases) kept as public surfaces per REPORT.md. Deletion requires stronger proof; see Uncertain.

## Config (old → new env)
- All old env names keep working: `WEBUI_USERNAME`, `WEBUI_PASSWORD`, `ANTIGRAVITY_OAUTH_CLIENTS`, `ANTIGRAVITY_OAUTH_CLIENT_KEY`, `CONFIG_PATH`, `CONFIG_SEED_PATH`, `LISTEN_ADDRESS`, `WEBUI_LISTEN_ADDRESS`, `STATE_DIR`.
- New additive only: `DATABASE_URL` (empty = file behavior, today's default; set = Postgres with file fallback). Validated at startup: must be postgres(s) URL, `sslmode=disable/allow` rejected, never logged.
- JSON keys, flags (`-config/-listen/-web-listen/-version/-cline-host/-cline-port`, `login` subcommand), defaults unchanged.

## DB schema
- Embedded versioned SQL in `internal/store/migrations/*.sql`, applied at startup, idempotent (`CREATE TABLE IF NOT EXISTS`, `ON CONFLICT DO NOTHING`), under transaction-level advisory lock `pg_try_advisory_xact_lock(hashtext('zenflash_v2_migrations'))`, additive only (no DROP/TRUNCATE/DELETE).
- `001_core`: `schema_migrations(version PK)`, `app_settings(key PK, value JSONB)`, `server_keys(key_hash PK, display, created_at)`.
- `002_catalog`: `model_catalog(slot PK, payload JSONB)`.
- `003_oauth_sessions`: `oauth_tokens(provider, account_id PK, payload JSONB)`, `webui_sessions(token_hash PK, payload JSONB, expires_at)` + expires index.
- `004_stats`: `request_stats_daily(day PK, requests, tokens)`, `request_events(id bigserial PK, ts, model, tier, success, duration_ms)` + ts index.
- Pooling: `pgxpool` with `DefaultQueryExecMode=SimpleProtocol` (no prepared statements/session state, PgBouncer transaction-mode safe). No `SET/LISTEN/session locks`. Connect retries with backoff ~30s (`config.DBConnectRetryBudget`) for autosuspend. Unsupported URL params reported, never weaken TLS.
- Importer: one-time idempotent `ImportOnce` from `config.json` (seeds hashed keys + config snapshot), `*.models.catalog.json`, `*.models.dev.json`; skips present slots, sets `v2.imported` marker. No file deletion, no data loss.
- File sync (`internal/store/sync.go`, wired at startup + every minute): mirrors every remaining file state (`.sessions.json`, `.codex-auth.json`, `.antigravity-auth.json`, catalog/dev caches, `.codex.enc` + `.key`, `data/.zen-config.json`) into `model_catalog` slots (`file:*`, binary base64-wrapped); restores missing files from the DB; files stay authoritative at runtime so behavior is unchanged. The dedicated `oauth_tokens`/`webui_sessions` tables exist for future direct use.
- Auth: one logical key compatible (list preserved for compat); seeded from today's file key on first boot; stored as SHA256 hash + display suffix (UI shows masked only). Keys cached in memory, refreshed every 60s (`config.AuthKeyTTL`); DB errors keep serving old cache. Requests never wait on DB; stats writes async buffered (1024, drop-on-full, 5s batch), never fail requests.
- Test: `scripts/test-throwaway-schema.sh` (throwaway schema, double-apply idempotency). CI without `DATABASE_URL` skips DB and builds as before.

## Behavior diff
- Intended fix (authorized 2026-10-06): colliding raw IDs now list `opencode/<id>` + `cline/<id>` aliases alongside the bare ID; prefixed IDs pin tiers on all inference routes (`zen/`, `go/`, `codex/`, `antigravity/` aliases included). Bare IDs, routes, formats, errors, auth unchanged.
- Otherwise none. Routes, request/response/streaming formats, error shapes, auth header handling, env/flag names, Docker/CI build preserved. Routes, request/response/streaming formats, error shapes, auth header handling, env/flag names, Docker/CI build all preserved. With `DATABASE_URL` empty, code paths fall back to file/memory exactly (nil store). With DB set, responses identical; only durability changes.

## Decisions
- Postgres optional (not required) to keep Docker/CI building and DB-down serving.
- SHA256 (not Argon2) for API keys: fast per-request hash compare + constant-time fallback; webui password stays Argon2id.
- Preserve multi-key list for compat despite "ONE key" wording; single seed covers the common case.
- File remains import source + fallback to guarantee no data loss; DB is mirror, not destructive replace.
- Only `pgx v5` added (`pgxpool`, `puddle`, `pgpassfile`, `pgservicefile`, `x/sync` indirect); no other deps.

## Uncertain
- Whether `model_catalog`/`oauth_tokens`/`webui_sessions` DB mirrors should become primary (currently file primary, DB mirror + key cache primary for auth). Switching primary would risk behavior; needs explicit approval.
- Dead-code candidates not removed for lack of proof: compat aliases, legacy admin paths, single-use helpers. `staticcheck`/`deadcode` not run (network); `go vet` + `grep` only.
- Remaining magic literals (model IDs, retry strings, log keys) not yet centralized; centralizing all risks churn. Env is fully centralized; other literals documented here.
- `HTTP_PROXY/HTTPS_PROXY` via `http.ProxyFromEnvironment` (implicit env) kept; moving through config would change transport behavior.
- Web UI needed no changes (API shapes unchanged); every page not click-tested, only `vite build` via existing dist (no rebuild in this env).

## WebUI sync
- New additive `GET /api/storage` (auth required): `{enabled, mode, reachable, keys_cached, slots, last_sync}`. No secrets. Existing admin routes unchanged.
- Settings gains a Storage tab (mode, keys cached, slots, last sync + provider-pin note); Models shows the catalog `provider` pill so `opencode/`/`cline/` pins are visible. No redesign.
- `webui/dist` rebuild is pending in this environment (vite transform exceeds the execution window; `svelte/compiler` validates both changed components cleanly). Rebuild with `cd webui && npm ci && npm run build` and commit `webui/dist`.

## Rollback
- Per-step revert: `git log --oneline v2`, then `git revert <sha>` for the step.
- Full rollback: `git checkout main` (v2 never merged to main; no force-push).
- Data rollback: files untouched by importer; DB additive only; stop binary, unset `DATABASE_URL`, restart on `main` with same `config.json`.

## [11] v2: persist Cline account pool in Postgres
- `STATE_DIR` now selects the embedded Cline account-pool file location. If a legacy pool exists elsewhere, it is copied to `STATE_DIR` without removing the source.
- The Postgres file mirror now syncs this pool as `file:cline-accounts` and restores it when the state file is absent.
- The pool follows the existing best-effort startup + one-minute mirror cadence. Set `DATABASE_URL` to enable Postgres; otherwise the file remains the only store.
- Verification: `go test ./internal/cline/kit ./internal/store ./internal/cline/app` and `go build -buildvcs=false ./cmd/zenflash-llm` passed.
