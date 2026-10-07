# zenflash

[![Deploy to Koyeb](https://www.koyeb.com/static/images/deploy/button.svg)](https://www.koyeb.com/deploy?name=zenflash&type=git&repository=https%3A%2F%2Fgithub.com%2Fleloush-x%2Fzenflash&branch=main&dockerfilePath=Dockerfile&ports=8080&healthChecks=%2Fhealthz)
[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy?repo=https://github.com/leloush-x/zenflash)


<p align="center">
  <img src="https://img.shields.io/badge/zenflash--llm-OpenCode%20free%20gateway-8b5cf6?style=for-the-badge&logo=lightning&logoColor=white" alt="zenflash-llm" />
</p>

<p align="center">
  <a href="https://github.com/leloush-x/zenflash/actions/workflows/checks.yml"><img src="https://img.shields.io/github/actions/workflow/status/leloush-x/zenflash-llm/checks.yml?branch=main&style=for-the-badge&label=checks" alt="CI" /></a>
  <a href="https://github.com/leloush-x/zenflash/releases"><img src="https://img.shields.io/github/v/release/leloush-x/zenflash-llm?style=for-the-badge" alt="release" /></a>
  <img src="https://img.shields.io/github/license/leloush-x/zenflash-llm?style=for-the-badge" alt="license" />
  <img src="https://img.shields.io/badge/go-1.24%2B-00ADD8?style=for-the-badge&logo=go" alt="go" />
</p>

**zenflash-llm** is a single static Go binary that exposes OpenCode Zen / Zen Go and Cline account models through familiar APIs: OpenAI Chat Completions, OpenAI Responses, Anthropic Messages, and OpenCode System One for Jev. It keeps an embedded Cline account backend, health/model refreshes, key/proxy pools, and request conversion in the same process — no separate Cline binary is required.

## Experimental Codex account pool

ZenFlash includes a backend-only Sign in with ChatGPT provider for Responses,
Chat Completions, and Anthropic Messages requests. It polls each linked
account's `GET /v1/models` catalog and exposes entries OpenAI marks with
`visibility: "list"`. Requests for those models are assigned round-robin to
accounts that list them; a 401 refreshes that account and a 429 tries the next
eligible account. Upstream requests use `store: false` and Responses streaming.

Use the authenticated admin API to manage accounts:

- `POST /api/codex/login/start` returns `auth_url`.
- Open it, authorize the app, then post the full callback URL to
  `POST /api/codex/login/complete` as `{"callback_url":"..."}`.
- `GET /api/codex/accounts`, `/api/codex/models`, and `/api/codex/usage`
  show linked accounts, the merged eligible catalog, and local rolling 30-day
  request/token totals. `POST /api/codex/models/refresh` polls immediately;
  `POST /api/codex/accounts/delete` accepts the account `id` returned above.
- Use the normal ZenFlash API key at `/v1/models`, `/v1/responses`,
  `/v1/chat/completions`, or `/v1/messages`.

OAuth tokens and cached account data are encrypted in `<config>.codex.enc`;
the 0600 key is stored beside it. OpenAI does not expose a numeric remaining
monthly allowance through this integration, so `/api/codex/usage` reports
ZenFlash's observed usage and links to ChatGPT Usage settings.

The gateway management UI is embedded in the binary and served at `/` on the main API port — log in with the configured admin account. A dedicated admin listener remains available via `webui.listen` (default loopback `127.0.0.1:8081`). Cline admin is also available at the embedded Cline port (`-cline-port`, default `3457`) when that downstream server is started.

## Feature grid

| Area | What you get |
| --- | --- |
| Protocols | `POST /v1/chat/completions`, `POST /v1/responses`, `POST /v1/messages`, `POST /v1/systemone`, `GET /v1/models` |
| Streaming | JSON and SSE for all three protocols, with a shared internal request/response model |
| Conversion | Text, images, tools, tool calls, tool results, usage, stop reasons, reasoning where representable |
| Upstream | Anonymous Zen lane, optional Zen/Go keys, and embedded Cline account-pool proxy |
| Routing | Per-model native protocol, key/proxy affinity, retries, exponentially growing cooldowns |
| Resilience | Fallback across proxies/tiers, stale reasoning reference repair, no retry after bytes stream |
| Ops | Health endpoint, structured slog JSON logs, Prometheus-friendly counters via management API |
| Config | JSONC, validation, atomic save + `.bak`, hot reload for new requests |
| Distribution | One static binary, Docker image, compose file, GitHub Release artifacts |

## Quick start

Requirements: **Go 1.24+**. No Node runtime, no database, no external service.

```bash
git clone https://github.com/leloush-x/zenflash.git
cd zenflash
cp config.example.json config.json
go build -o zenflash-llm ./cmd/zenflash-llm
```

Edit `config.json` before the first useful run:

```json
{
  "server_keys": ["sk-local-change-me"],
  "anonymous": true,
  "zen_keys": [],
  "go_keys": ["free"],
  "upstream": {
    "zen": "https://opencode.ai/zen",
    "go": "http://127.0.0.1:3457"
  }
}
```

The `go` upstream URL can point at the embedded Cline proxy started inside the same binary. Use `cline-proxy` directly only when you deliberately want a separate process.

Start it:

```bash
./zenflash-llm -config config.json
```

The same process also starts the embedded Cline proxy on `127.0.0.1:3457` by default. Set `-cline-port 0` to disable that downstream server.

Check readiness:

```bash
curl http://127.0.0.1:8080/healthz
```

### Binary

Download a release archive from [GitHub Releases](https://github.com/leloush-x/zenflash/releases), then:

```bash
./zenflash-llm -config config.json
```

Windows:

```powershell
Copy-Item config.example.json config.json
go build -o zenflash-llm.exe ./cmd/zenflash-llm
.\zenflash-llm.exe -config config.json
```

Flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `-config` | `config.json` | Path to configuration file |
| `-listen` | unset | Override API listen address |
| `-web-listen` | unset | Override management listen address |
| `-version` | unset | Print version and exit |

The configuration directory must be writable for password migration, config saves, and model caches.

## Docker

```bash
cp config.example.json config.json
# edit config.json first
docker compose up -d
docker compose logs -f
```

Compose imports the host configuration into the `zenflash-llm-state` volume on first startup. Later edits should use the management API or re-import:

```bash
docker compose cp config.json zenflash-llm:/var/lib/zenflash-llm/config.json
docker compose restart
```

Build locally:

```bash
docker build -t zenflash-llm:local .
docker volume create zenflash-llm-state
docker run -d --name zenflash-llm \
  -p 8080:8080 \
  -e CONFIG_SEED_PATH=/run/config/zenflash-llm.json \
  -v "$(pwd)/config.json:/run/config/zenflash-llm.json:ro" \
  -v zenflash-llm-state:/var/lib/zenflash-llm \
  zenflash-llm:local
```

## Authentication

There are two separate auth layers. Do not mix them up.

### 1. Client → gateway

`server_keys` protect your gateway. New configurations default to the client key `free`; replace it with any non-empty value you choose.

Send either:

```http
Authorization: Bearer sk-local-change-me
```

or:

```http
x-api-key: sk-local-change-me
```

Health checks do not require auth.

### 2. Gateway → upstream

Upstream credentials are **never** the client key. From `zen_keys` / `go_keys`:

- Zen tier uses `https://opencode.ai/zen`
- Go-like local Cline tier uses the embedded Cline proxy at `http://127.0.0.1:3457`
- `anonymous: true` allows free Zen routing with the OpenCode `public` credential

For Anthropic-native upstream requests the token is sent as `x-api-key: ...`; for OpenAI-shaped upstreams it is `Authorization: Bearer ...`.

## API

Base URL example: `http://127.0.0.1:8080`

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| GET | `/v1/models` | Bearer or x-api-key | Live union of Zen + embedded Cline models, with `provider` labels |
| POST | `/v1/chat/completions` | Bearer or x-api-key | OpenAI Chat Completions across the unified catalog |
| POST | `/v1/responses` | Bearer or x-api-key | OpenAI Responses |
| POST | `/v1/messages` | x-api-key + anthropic-version | Anthropic Messages |
| POST | `/v1/systemone` | Bearer or x-api-key | Jev/System One structured decisions |
| GET | `/healthz` | none | readiness + summary |

### List models

```bash
curl http://127.0.0.1:8080/v1/models \
  -H "Authorization: Bearer sk-local-change-me"
```

When only anonymous access is configured, this returns only anonymous-eligible free models.

### Chat Completions

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-local-change-me" \
  -H "Content-Type: application/json" \
  -d '{"model":"big-pickle","messages":[{"role":"user","content":"Hello"}]}'
```

Streaming:

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-local-change-me" \
  -H "Content-Type: application/json" \
  -d '{"model":"big-pickle","stream":true,"messages":[{"role":"user","content":"Hello"}]}'
```

### Responses

```bash
curl http://127.0.0.1:8080/v1/responses \
  -H "Authorization: Bearer sk-local-change-me" \
  -H "Content-Type: application/json" \
  -d '{"model":"big-pickle","input":"Hello"}'
```

With reasoning:

```bash
curl http://127.0.0.1:8080/v1/responses \
  -H "Authorization: Bearer sk-local-change-me" \
  -H "Content-Type: application/json" \
  -d '{"model":"big-pickle","input":"Explain briefly","reasoning":{"effort":"high"}}'
```

### Anthropic Messages

```bash
curl http://127.0.0.1:8080/v1/messages \
  -H "x-api-key: sk-local-change-me" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"big-pickle","max_tokens":512,"messages":[{"role":"user","content":"Hello"}]}'
```

Every response includes `x-request-id` for log correlation. API bodies are capped at 32 MiB.

## Free model routing

A model is considered eligible for the anonymous Zen lane when either:

1. Its id contains `free` (case-insensitive), or
2. models.dev reports zero input and output cost and it is not deprecated.

Routing order for an eligible model:

1. Try each available anonymous proxy once.
2. Try authenticated tiers in `prefer` order (`zen` or `go`).
3. Apply `retry.max_attempts` per authenticated tier.

Anonymous attempts are not truncated by `retry.max_attempts`, but they share the overall request timeout. Once tokens have been streamed to the client, the gateway does not retry on another node.

## Sessions and proxies

Session affinity prefers an explicit header:

- `x-session-id`
- `x-opencode-session`
- `x-session-affinity`
- `conversation-id`
- `conversation_id`
- `metadata.session_id`

If absent, the first user message seeds affinity. Consecutive requests in the same conversation should send the same id to hit the same key/proxy when the pool has not changed.

Proxies accept `direct`, `http://`, `https://`, `socks5://`, and `socks5h://`, including credentials. Inline `proxies` and `proxyfile` are merged and deduplicated.

```text
# Primary proxy
http://user:password@127.0.0.1:7890
socks5://127.0.0.1:1080  # backup
direct
```

Healthy proxies are rechecked every 15 minutes against Cloudflare trace. Cooldowns grow exponentially up to 8 × `performance.failure_cooldown_seconds`; `Retry-After` is honored when larger.

## Configuration reference

JSON supports `//` and `/* ... */` comments. Unknown fields are rejected. See [config.example.json](config.example.json).

### Core

| Field | Default / rule |
| --- | --- |
| `listen` | `127.0.0.1:8080` |
| `server_keys` | defaults to `free`; at least one client key is retained |
| `zen_keys` | required unless `anonymous: true` provides eligible models |
| `go_keys` | optional unless selected by routing |
| `anonymous` | `false`; example sets `true` |
| `prefer` | `go` or `zen` |
| `upstream.zen` | `https://opencode.ai/zen` |
| `upstream.go` | `http://127.0.0.1:3457` |
| `proxies` | `["direct"]` when empty |
| `proxyfile` | optional, relative to config file |
| `models.refresh_seconds` | `300`, min 1 |
| `models.protocols` | `{}` per-model override: `chat`, `responses`, `anthropic` |

### Reasoning

```json
{
  "reasoning": {
    "effort": "high",
    "effort_by_model": { "claude-opus-5": "max" }
  }
}
```

`reasoning.effort` is applied upstream when the client did not state its own level. A client-stated level always wins. Use `"none"` to suppress reasoning. Levels cross protocols: `budget_tokens` maps to the same rung ladder (`8192` ≈ `high`, `32000` ≈ `xhigh`).

### Retry / performance

| Field | Default | Notes |
| --- | ---: | --- |
| `retry.max_attempts` | `3` | per authenticated tier |
| `retry.timeout_seconds` | `300` | whole request including stream |
| `performance.attempt_timeout_seconds` | `0` | header wait; 0 = request timeout |
| `performance.connect_timeout_seconds` | `5` | dial timeout |
| `performance.failure_cooldown_seconds` | `15` | base cooldown |
| `performance.max_idle_conns` | `2048` | per proxy transport |
| `performance.max_idle_conns_per_host` | `256` | per host per transport |
| `performance.max_conns_per_host` | `0` | unlimited |
| `performance.idle_conn_timeout_seconds` | `120` | idle connection lifetime |

### Logging

| Field | Default | Notes |
| --- | --- | --- |
| `logging.level` | `info` | `debug`, `info`, `warn`, `error` |
| `logging.ring_size` | `2000` | 100–50000 |
| `logging.dump_request_bodies` | `false` | requires `debug`; capped 64 KiB, secrets redacted |

### Management

| Field | Default | Notes |
| --- | --- | --- |
| `webui.enabled` | `false` | example sets `true`; serves the embedded dashboard |
| `webui.listen` | `127.0.0.1:8081` | optional dedicated admin listener, loopback by default; the dashboard is always also served at `/` on the API port. Set equal to `listen` to disable the dedicated port |
| `webui.username` | required if enabled | example `admin` |
| `webui.password` | min length 4 (example: `free`) | bootstrap only; hashed at startup |
| `-` | `WEBUI_USERNAME` / `WEBUI_PASSWORD` env | override admin credentials |
| `-` | `config.json.sessions.json` | session store; browsers stay logged in across restarts |
| `webui.password_hash` | generated | Argon2id; replaces plaintext |
| `webui.session_ttl_minutes` | `720` | 5–10080 |

Management auth uses one admin account, HttpOnly/SameSite cookies, CSRF checks for writes, and login throttling.

## Management API

All routes except login require the session cookie from:

```bash
curl -c cookies.txt -X POST http://127.0.0.1:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"change-this-admin-password"}'
```

| Route | Purpose |
| --- | --- |
| `POST /api/auth/login` | login |
| `GET /api/auth/session` | inspect session |
| `POST /api/auth/logout` | logout |
| `GET /api/config` | masked config |
| `PUT /api/config` | replace/validate config |
| `POST /api/config/reload` | reload from disk |
| `POST /api/config/reveal` | reveal secrets after password check |
| `PUT /api/account` | change credentials |
| `GET /api/monitor` | usage and resource stats |
| `GET /api/debug/models` | model routing diagnostics |
| `POST /api/debug/inference` | Playground single call |
| `GET /api/logs` | recent logs |
| `GET /api/logs/stream` | SSE log stream |
| `GET /api/events` | SSE live dashboard snapshot |
| `GET /` | embedded dashboard (SPA) |

Mutating routes require the `X-CSRF-Token` returned at login.

The dashboard itself is a Svelte single-page app embedded via `go:embed`. It
live-updates through `GET /api/events` (SSE, two-second `tick` snapshots of
metrics/resources) and falls back to the same management API for everything
else. Build assets: `cd webui && npm ci && npm run build`. The compiled `webui/dist/` is committed and embedded via `go:embed`, so `go build` and `docker build` work without Node.

## Health

`/healthz` returns JSON. It does not trigger upstream calls.

- `503` while catalog is pending, no routable models, or no healthy proxies.
- `200` when ready; `degraded` when a stale cache is still usable (staleness threshold is 2 × `models.refresh_seconds`, min 60s).

## Security notes

- Keep `config.json` and `config.json.bak` private; they contain upstream keys and proxy credentials.
- Put the API behind HTTPS when exposed beyond localhost.
- Set a strong `webui.password`; it is migrated to Argon2id on first start and the plaintext backup is removed.
- Do not commit `config.json` with real keys.

## Secrets & environment

One template works everywhere: `.env.example`. Non-sensitive values are
pre-filled; important secrets stay empty with comments.

| Secret | Where it lives | Notes |
| --- | --- | --- |
| `DATABASE_URL` | Koyeb env vars (Secret) or GitHub Secrets, or local `.env` | Empty = file-backed state. Must be `postgres://`, TLS required |
| `WEBUI_PASSWORD` | Same as above | Empty = set it in Settings UI after first boot |
| `ANTIGRAVITY_OAUTH_CLIENTS` | Same as above | `key\|client_id\|client_secret;…` tuples, never committed |

Local runs: copy `.env.example` to `.env` next to `compose.yaml` and fill
the secrets there (gitignored). Koyeb: paste each line under service
Settings → Environment variables and redeploy. Never commit real values —
only secret *names* live in git, values live in Koyeb/GitHub.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| 401 from API | Client must send a configured `server_keys` value |
| No models | anonymous eligibility, configured tiers, `models.protocols` overrides |
| `Model is unavailable` | Model may be temporarily blocked upstream; try another `*-free` id |
| 502/504 | proxies, upstream keys, total/attempt timeouts |
| HTTP 200 but bad stream | inspect SSE error and management logs |
| Docker port unreachable | container listener must be `0.0.0.0`, host mapping separate |

## Development

```bash
gofmt -w cmd internal
go vet ./...
go test ./...
go build -o zenflash-llm ./cmd/zenflash-llm
```

Run locally:

```bash
go run ./cmd/zenflash-llm -config config.json
```

Release builds inject version:

```bash
go build -trimpath -ldflags "-s -w -X main.version=v1.0.0" -o zenflash-llm ./cmd/zenflash-llm
```

## License

MIT.

## Code map (v2)

- `cmd/zenflash-llm`: flags, env validation, servers, embedded Cline boot.
- `internal/config`: single typed config + env (all defaults/constants/headers/routes).
- `internal/gateway`: routes, auth (memory keys, DB-backed when configured), retries.
- `internal/protocol`: Chat/Responses/Anthropic/System One conversion + SSE.
- `internal/models`: discovery, catalog/pricing caches, routing.
- `internal/admin`: management API + embedded UI.
- `internal/cline`: embedded Cline proxy (`app`), registration (`cline`), helpers (`kit`).
- `internal/codex`, `internal/antigravity`: OAuth tiers, token stores.
- `internal/store`: optional Neon Postgres (migrations, importer, key TTL, async stats).
- `internal/httpx`, `internal/identity`, `internal/jsonutil`, `internal/telemetry`: shared support.
- `webui`: dashboard source + embedded dist (rebuild only when UI changes).

Env: see `.env.example`. `DATABASE_URL` empty keeps file behavior; set uses
Postgres with file fallback, memory serving, async stats.

## Endpoints

| Method | Path | Description |
| --- | --- | --- |
| GET | `/v1/models` | Models that can be routed with the current configuration. `?working=1` keeps only models that answer a live probe. |
| POST | `/v1/chat/completions` | Chat Completions. |
| POST | `/v1/responses` | Responses. |
| POST | `/v1/messages` | Anthropic Messages. |
| GET | `/healthz` | Readiness and resource summary. |
| POST | `/v1/systemone` | Jev decision endpoint (System One models only). |

Every model ID listed under `/v1/models` works on every inference ingress:
Chat, Responses, and Anthropic convert through the shared internal protocol,
so an OpenAI-native model answers Anthropic requests and vice versa. Jev
decision models answer only `/v1/systemone` (they have no message-shaped
equivalent); a decision payload sent to a message endpoint is relayed verbatim.

Provider pins: when the same raw ID exists on both OpenCode (zen) and Cline
(go), `/v1/models` shows the bare ID (prefer-order, backward compatible) plus
`opencode/<id>` and `cline/<id>`. Either prefixed ID is accepted on all
inference routes and pins that provider (`zen/`, `go/`, `codex/`,
`antigravity/` aliases also work).

Working-only listing: `GET /v1/models?working=1` sends each listed ID one
tiny reply through the normal inference path and returns only the entries
that answer `2xx`. It is slower (one short reply per model, 8 at a time,
30s cap each), opt-in, and changes nothing about the default listing or the
response shape. Probes run under a diagnostic context, so they never cool
keys, burn metrics, or reshape traffic.
