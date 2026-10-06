# v2 baseline (behavior freeze)

Routes (frozen): GET /v1/models, POST /v1/chat/completions, POST /v1/responses,
POST /v1/messages, POST /v1/systemone, GET /healthz. Admin /api/* unchanged.
Auth: Authorization Bearer or x-api-key, empty server_keys disables auth.
Streaming: SSE transcoding preserved. Error envelopes per protocol preserved.

State today (file-backed, no SQL):
- config.json (+ .bak atomic), <config>.sessions.json, <config>.codex.enc (+ .key),
  <config>.codex-auth.json, <config>.antigravity-auth.json,
  <config>.models.catalog.json, <config>.models.dev.json, data/.zen-config.json.
- Request logs/metrics in memory only.

Baseline script: scripts/baseline_routes.sh (normal, stream, tools, bad auth,
bad input, stub upstream where possible). Re-run after each phase; any diff
reverts that step.

Build: go build -trimpath ./cmd/zenflash-llm, go vet ./..., go test ./...
Docker/CI must keep building (no DB required when DATABASE_URL empty).
