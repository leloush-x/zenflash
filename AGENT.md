# Agent notes for zenflash-llm

## Layout

- `cmd/zenflash-llm`: process setup, flags, servers.
- `internal/gateway`: HTTP routes, authentication, retries, upstream/proxy/key pools, refresh, health.
- `internal/protocol`: OpenAI Chat, OpenAI Responses, Anthropic Messages, and System One conversion and SSE handling.
- `internal/models`: model discovery/catalog/cache and pricing/free-model metadata.
- `internal/config`: config schema, validation, secrets, proxy resolution, persistence.
- `internal/admin`: management API and serving the embedded UI.
- `internal/cline`: embedded Cline-compatible proxy and state.
- `internal/telemetry`, `internal/httpx`, `internal/identity`, `internal/jsonutil`: shared support.
- `webui/`: frontend source and embedded distribution. Backend-only tasks must not edit it.

## Conventions and commands

This is a Go 1.25 module, not a Bun project. Config is strict JSON: unknown keys fail validation. Keep external route names, response fields, config keys, environment variables, and defaults stable. Keep secrets out of model caches and logs; use existing redaction helpers. Preserve both streaming and non-streaming protocol conversion.

- Build: `go build -trimpath ./cmd/zenflash-llm`
- Tests: `go test ./...`
- Vet: `go vet ./...`
- Format Go: `gofmt -w <changed .go files>`
- Container entrypoint syntax: `sh -n docker-entrypoint.sh`

Default listeners are API `127.0.0.1:8080`, admin `127.0.0.1:8081`, and embedded Cline `127.0.0.1:3457`. These have CLI/config overrides; zero Cline port disables the embedded proxy.

## Invariants

- Do not rename public API routes or alter existing response/config JSON shapes without explicit migration.
- Keep config JSON keys, environment variables, and defaults compatible.
- `/v1/chat/completions`, `/v1/responses`, and `/v1/messages` share gateway routing and conversion. Preserve their protocol-specific errors, headers, streaming events, and usage accounting.
- Preserve stale model/pricing cache on refresh failure; model discovery runs asynchronously.
- Keep model routing protocol-aware per upstream tier; the same model ID may differ between Zen and Go.
- Do not edit `webui/` for backend-only tasks.
- Model capability fields must come from upstream data. Missing values stay unknown/omitted; never manufacture context sizes, output limits, or effort lists.
- FREEZE (user 2026-10-06): keep the Cline fix as-is — never alter `internal/cline` register/proxy behavior (`cline/cline/auth.go` RegisterWithCline, `cline/kit/http.go` proxy handling, embedded proxy state). Only env/config (`HTTPS_PROXY`, `proxies`) may change for retries.

## How to log changes

Use one commit per logical change, with subject `[NN] area: what/why`. After committing, append an entry to `CHANGELOG-AGENT.md` containing the ID, files, reason, test result, and exact rollback command (`git revert <sha>`). Keep that log update in a follow-up commit if the SHA is needed. Use `pre-upgrade` as the baseline tag and work on `backend-upgrade` for the current task.
