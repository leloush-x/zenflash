# Backend architecture report

## Architecture and request flow

`cmd/zenflash-llm/main.go` loads and normalizes JSON configuration, starts the optional embedded Cline proxy, creates a runtime manager, and serves the API (default `127.0.0.1:8080`). The gateway is under `internal/gateway`; its model catalog and upstream metadata are in `internal/models`; JSON protocol conversion is under `internal/protocol`. `internal/admin` adds the management API and serves the already-built web UI. The Cline-compatible upstream proxy is implemented in `internal/cline`.

The runtime manager atomically swaps gateway instances when configuration changes. Each gateway owns the upstream HTTP transports, Zen and Go key pools, anonymous lane, and model catalog. Refresh jobs run asynchronously; the model catalog is loaded from disk at startup and refresh failures leave the previous snapshot usable.

Routes registered by the inference gateway:

| Route | Flow |
| --- | --- |
| `GET /v1/models` | Authenticates, selects currently available routes, and returns OpenAI list form (`object: list`, `data`). Each item includes provider/protocol and available upstream context, output, reasoning, and effort metadata. |
| `POST /v1/chat/completions` | Parses model and stream flag, resolves a tier/protocol route, maps request into the upstream protocol, retries eligible pools, and returns native or converted JSON/SSE. |
| `POST /v1/responses` | Same route selection and retry flow; Responses payloads are converted to/from the selected upstream protocol as needed. |
| `POST /v1/messages` | Anthropic Messages ingress; request/response and stream events are converted when the selected upstream protocol differs. |
| `POST /v1/systemone` | System One ingress; only supports a model routed to the System One native protocol. |
| `GET /healthz` | Reports readiness, model catalog freshness, key counts, and proxy health. |

All OpenAI and Anthropic inference routes share `Gateway.handleInference`. It validates JSON/model, resolves protocol-specific tier routing, derives request IDs, applies reasoning configuration, handles upstream retries and errors, then forwards or transcodes the result. `/v1/messages` errors use Anthropic error envelopes. Authentication accepts bearer tokens and `x-api-key`; an empty `server_keys` list disables local auth.

## Data stores and refresh

There is no SQL database in this checkout. Runtime configuration is JSON (`config.json`, ignored by Git); updates use atomic JSON persistence. The model capability catalog, including upstream-declared reasoning efforts, is persisted beside the config as `config.json.models.catalog.json`; pricing metadata from `models.dev` is persisted as `config.json.models.dev.json`. Both caches use background refresh and retain the last good snapshot if refresh fails. The Cline account/key state is managed by `internal/cline/app` and is stored outside the gateway catalog. Request logs/metrics are process memory.

Model IDs come from configured upstream `/v1/models` endpoints. Protocol/capability metadata comes from OpenCode's public `https://models.opencode.ai/api.json` catalog, with provider endpoint docs as a supplement. Pricing/free status uses `https://models.dev/api.json`. Declared `reasoning_options` are copied exactly into catalog metadata and the OpenAI-shaped `/v1/models` response. Explicit effort requests are checked against the selected tier's declared values; when the source omits the field, the gateway preserves pass-through behavior. No inference requests are sent to probe effort support.

## Configuration keys

The canonical keys are in `config.example.json` and `internal/config/config.go`:

- `listen`, `server_keys`, `zen_keys`, `go_keys`, `codex_keys`, `anonymous`
- `proxies`, `proxyfile`, `upstream.zen`, `upstream.go`, `upstream.codex`
- `retry.max_attempts`, `retry.timeout_seconds`
- `models.refresh_seconds`, `models.protocols`
- `performance.max_idle_conns`, `max_idle_conns_per_host`, `max_conns_per_host`, `idle_conn_timeout_seconds`, `connect_timeout_seconds`, `failure_cooldown_seconds`, `attempt_timeout_seconds`
- `logging.level`, `logging.ring_size`, `logging.dump_request_bodies`
- `webui.enabled`, `webui.listen`, `webui.username`, `webui.password`, `webui.password_hash`, `webui.session_ttl_minutes`
- `prefer`, `reasoning.effort`, `reasoning.effort_by_model`

CLI flags: `-config` (default `config.json`), `-listen`, `-web-listen`, `-version`, `-cline-host` (default `127.0.0.1`), and `-cline-port` (default `3457`; zero disables Cline). The `login` subcommand (`zenflash-llm login -config config.json`) runs the ChatGPT device-code sign-in and saves refreshable Codex credentials to `<config>.codex-auth.json`. `WEBUI_USERNAME` and `WEBUI_PASSWORD` override the corresponding settings. No `config.json` is checked in; `config.example.json` is the template.

## Known risks

- This project is Go 1.25 (`go.mod`), not a Bun application. No `package.json`, Bun `typecheck` script, SQLite dependency, or Anthropic-specific models-list route exists. The Anthropic equivalent is the shared `/v1/models` list plus `/v1/messages` inference.
- Catalog persistence is atomic JSON, not SQLite. This format is established runtime state, so a SQLite migration needs a compatibility plan before changing it.
- Capability model entries represent missing numeric fields as zero and omit them from output. They do not distinguish upstream `unknown` from absent fields as explicit values.
- The UI is embedded at build time. Do not rebuild or edit `webui/` for backend-only changes.
- No end-to-end smoke environment or credentials are present in the checkout, so live upstream smoke calls cannot be assumed reproducible.

## Duplicate/dead code review

No confidently dead backend code was established in this pass. Similar route registrations in `internal/cline/app/proxy.go` (`/v1/...` and unprefixed aliases), and `/v1/...` aliases in `internal/admin/openai_alias.go`, are compatibility aliases and should not be removed without usage evidence. Legacy-looking `opencode`/`zen` admin path aliases in `internal/cline/app/admin.go` are also public compatibility surfaces. No deduplication was made.
