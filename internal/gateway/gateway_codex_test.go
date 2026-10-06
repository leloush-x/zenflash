package gateway

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zenflash-llm/internal/config"
	"zenflash-llm/internal/telemetry"
)

// The Codex upstream is Responses-only and SSE-only. The gateway must drive it
// with a Codex-shaped request, force streaming on the wire, and collapse the
// returned event stream back into a JSON document for non-streaming clients.
func TestCodexTierRequestShapeAndCollapse(t *testing.T) {
	var capturedAuth, capturedUA string
	var capturedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("upstream path = %s, want /responses", r.URL.Path)
		}
		capturedAuth = r.Header.Get("Authorization")
		capturedUA = r.Header.Get("User-Agent")
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: response.created\n")
		_, _ = io.WriteString(w, `data: {"type":"response.created","response":{"id":"resp_1","model":"m"}}`+"\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\n")
		_, _ = io.WriteString(w, `data: {"type":"response.output_text.delta","delta":"Hi","item_id":"msg_1","output_index":0,"content_index":0,"sequence_number":1}`+"\n\n")
		_, _ = io.WriteString(w, "event: response.completed\n")
		_, _ = io.WriteString(w, `data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","model":"m","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}},"sequence_number":2}`+"\n\n")
	}))
	defer upstream.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	cfgJSON := `{
	  "listen": "127.0.0.1:0",
	  "server_keys": [],
	  "zen_keys": [],
	  "go_keys": [],
	  "codex_keys": ["tok"],
	  "anonymous": false,
	  "proxies": ["direct"],
	  "upstream": {
	    "zen": "https://opencode.ai/zen",
	    "go": "https://opencode.ai/zen/go",
	    "codex": "` + upstream.URL + `"
	  },
	  "retry": {"max_attempts": 1, "timeout_seconds": 30},
	  "models": {"refresh_seconds": 3600, "protocols": {}},
	  "performance": {
	    "max_idle_conns": 64, "max_idle_conns_per_host": 16, "max_conns_per_host": 0,
	    "idle_conn_timeout_seconds": 30, "connect_timeout_seconds": 5, "failure_cooldown_seconds": 15,
	    "attempt_timeout_seconds": 0
	  },
	  "logging": {"level": "error", "ring_size": 100},
	  "webui": {"enabled": false, "listen": "127.0.0.1:0", "username": "u", "password": "password", "session_ttl_minutes": 60},
	  "prefer": "codex"
	}`
	if err := os.WriteFile(configPath, []byte(cfgJSON), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g, err := New(cfg, logger, telemetry.NewMonitor(), configPath)
	if err != nil {
		t.Fatal(err)
	}
	g.catalog.Replace(nil, nil, []string{"m"})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"m","input":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer free")
	req = req.WithContext(telemetry.WithRequestMeta(req.Context(), &telemetry.RequestMeta{}))
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if capturedAuth != "Bearer tok" || capturedUA != "codex-cli/0.91.0" {
		t.Fatalf("captured auth=%q ua=%q", capturedAuth, capturedUA)
	}
	var upstreamBody map[string]any
	if err := json.Unmarshal(capturedBody, &upstreamBody); err != nil {
		t.Fatalf("upstream body not JSON: %v", err)
	}
	if upstreamBody["stream"] != true {
		t.Fatalf("upstream stream = %v, want true", upstreamBody["stream"])
	}
	var downstream map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &downstream); err != nil {
		t.Fatalf("downstream not JSON: %v body=%s", err, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"Hi"`) {
		t.Fatalf("collapsed body missing text: %s", rec.Body.String())
	}
}
