package codex

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"strings"
	wire "zenflash-llm/internal/protocol"
)

// WrapAPI inserts account-authorized models into the existing OpenAI model
// list and routes only those Responses requests to the Codex account pool.
func (s *Service) WrapAPI(base http.Handler, keys func() []string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		base.ServeHTTP(recorder, r)
		if recorder.Code < 200 || recorder.Code >= 300 {
			copyHTTP(w, recorder)
			return
		}
		var listing map[string]any
		if json.Unmarshal(recorder.Body.Bytes(), &listing) != nil {
			copyHTTP(w, recorder)
			return
		}
		if r.URL.Query().Get("free") == "1" || r.URL.Query().Get("free") == "true" {
			copyHTTP(w, recorder)
			return
		}
		data, _ := listing["data"].([]any)
		indices := map[string]int{}
		for i, item := range data {
			if m, ok := item.(map[string]any); ok {
				if id, ok := m["id"].(string); ok {
					indices[id] = i
				}
			}
		}
		for _, model := range s.Models() {
			entry := map[string]any{"id": model.ID, "object": "model", "created": time.Now().Unix(), "owned_by": model.OwnedBy, "display_name": model.DisplayName, "source": "codex-account", "provider": "codex", "route_protocol": "responses"}
			if i, exists := indices[model.ID]; exists {
				data[i] = entry
			} else {
				indices[model.ID] = len(data)
				data = append(data, entry)
			}
		}
		listing["data"] = data
		body, err := json.Marshal(listing)
		if err != nil {
			copyHTTP(w, recorder)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(body)
	})
	proxy := func(external wire.Protocol) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
			if err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			var payload map[string]any
			if json.Unmarshal(body, &payload) != nil {
				http.Error(w, "request body must be JSON", http.StatusBadRequest)
				return
			}
			model, _ := payload["model"].(string)
			if !s.HasModel(model) {
				r.Body = io.NopCloser(strings.NewReader(string(body)))
				base.ServeHTTP(w, r)
				return
			}
			if !s.AuthorizeLocalKey(keys(), r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"error":{"type":"authentication_error","message":"invalid local API key"}}`)
				return
			}
			stream, _ := payload["stream"].(bool)
			converted, err := wire.ConvertRequest(external, wire.Responses, payload)
			if err != nil {
				wire.WriteError(w, external, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
				return
			}
			converted["model"] = model
			s.ProxyResponses(w, r, model, converted, external, stream)
		}
	}
	mux.HandleFunc("POST /v1/responses", proxy(wire.Responses))
	mux.HandleFunc("POST /v1/chat/completions", proxy(wire.Chat))
	mux.HandleFunc("POST /v1/messages", proxy(wire.Anthropic))
	mux.Handle("/", base)
	return mux
}

func (s *Service) HasModel(id string) bool {
	for _, m := range s.Models() {
		if m.ID == id {
			return true
		}
	}
	return false
}
func copyHTTP(w http.ResponseWriter, r *httptest.ResponseRecorder) {
	for k, v := range r.Header() {
		for _, x := range v {
			w.Header().Add(k, x)
		}
	}
	w.WriteHeader(r.Code)
	_, _ = w.Write(r.Body.Bytes())
}
