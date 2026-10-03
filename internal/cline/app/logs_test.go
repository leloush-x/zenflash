package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStatusWriterImplementsFlusher guards the regression behind
// "HTTP 200 with an empty streaming body": statusWriter embeds the
// http.ResponseWriter *interface*, so Flush() is not promoted automatically.
// Without this method the type assertion w.(http.Flusher) inside
// handleStreamResponseWithUsage fails and every SSE response is silently
// truncated to zero bytes.
func TestStatusWriterImplementsFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}

	if _, ok := http.ResponseWriter(sw).(http.Flusher); !ok {
		t.Fatal("statusWriter must implement http.Flusher; streaming responses would be empty")
	}
}

func TestStatusWriterUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	if sw.Unwrap() != http.ResponseWriter(rec) {
		t.Fatal("Unwrap must return the underlying writer")
	}
}

// TestStreamingThroughMiddleware exercises the real failure mode: an SSE body
// written from inside requestLogMiddleware must reach the client intact.
func TestStreamingThroughMiddleware(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	handler := requestLogMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("wrapped writer lost http.Flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, chunk := range []string{"data: {\"a\":1}\n\n", "data: [DONE]\n\n"} {
			if _, err := w.Write([]byte(chunk)); err != nil {
				t.Fatalf("write: %v", err)
			}
			flusher.Flush()
		}
	}))
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if body == "" {
		t.Fatal("streamed body was swallowed by middleware")
	}
	if !strings.Contains(body, "[DONE]") {
		t.Fatalf("stream body incomplete, got %q", body)
	}
}