#!/bin/sh
# Baseline route smoke: hits every route (normal, stream, tools, bad auth, bad input).
# Upstream is stubbed where possible by pointing at a local mock.
# Usage: BASE_URL=http://127.0.0.1:8080 API_KEY=free sh scripts/baseline_routes.sh
set -eu
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
API_KEY="${API_KEY:-free}"
pass=0; fail=0
hit() {
  desc="$1"; shift
  code=$(curl -s -o /tmp/baseline_body.txt -w "%{http_code}" "$@" || true)
  echo "$desc -> $code"
  echo "$code $desc" >> /tmp/baseline_results.txt
}
rm -f /tmp/baseline_results.txt /tmp/baseline_body.txt
echo "== baseline $(date -u +%FT%TZ) base=$BASE_URL =="
hit "GET /healthz (no auth)" "$BASE_URL/healthz"
hit "GET /v1/models (good auth)" -H "Authorization: Bearer $API_KEY" "$BASE_URL/v1/models"
hit "GET /v1/models (bad auth)" -H "Authorization: Bearer bad-key" "$BASE_URL/v1/models"
hit "POST /v1/chat/completions (bad input)" -X POST -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" -d '{"model":""}' "$BASE_URL/v1/chat/completions"
hit "POST /v1/chat/completions (normal)" -X POST -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" -d '{"model":"big-pickle","messages":[{"role":"user","content":"hi"}]}' "$BASE_URL/v1/chat/completions"
hit "POST /v1/chat/completions (stream)" -N -X POST -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" -d '{"model":"big-pickle","stream":true,"messages":[{"role":"user","content":"hi"}]}' "$BASE_URL/v1/chat/completions"
hit "POST /v1/chat/completions (tools)" -X POST -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" -d '{"model":"big-pickle","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"get_time","description":"time","parameters":{"type":"object","properties":{}}}}]}' "$BASE_URL/v1/chat/completions"
hit "POST /v1/responses (normal)" -X POST -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" -d '{"model":"big-pickle","input":"hi"}' "$BASE_URL/v1/responses"
hit "POST /v1/messages (normal)" -X POST -H "x-api-key: $API_KEY" -H "anthropic-version: 2023-06-01" -H "Content-Type: application/json" -d '{"model":"big-pickle","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}' "$BASE_URL/v1/messages"
hit "POST /v1/messages (bad auth)" -X POST -H "x-api-key: bad-key" -H "anthropic-version: 2023-06-01" -H "Content-Type: application/json" -d '{"model":"big-pickle","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}' "$BASE_URL/v1/messages"
hit "POST /v1/systemone (bad input)" -X POST -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" -d '{}' "$BASE_URL/v1/systemone"
echo "== done =="
cat /tmp/baseline_results.txt || true
