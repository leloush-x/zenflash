package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// workingProbeTimeout bounds one model liveness check. Probes run through the
// normal inference path with tiny replies, so most finish in seconds.
const workingProbeTimeout = 30 * time.Second

// workingProbeWorkers caps concurrent upstream probes so a working-filtered
// listing does not burst rate limits on shared keys.
const workingProbeWorkers = 8

type workingRecorder struct {
	header http.Header
	status int
}

func newWorkingRecorder() *workingRecorder {
	return &workingRecorder{header: http.Header{}, status: http.StatusOK}
}

func (rec *workingRecorder) Header() http.Header { return rec.header }

func (rec *workingRecorder) WriteHeader(status int) {
	if rec.status != http.StatusOK || status == http.StatusOK {
		return
	}
	rec.status = status
}

func (rec *workingRecorder) Write(data []byte) (int, error) { return len(data), nil }

// probeBody builds the smallest valid non-streaming request for an entry's
// route protocol. Shapes were verified live: chat/responses/anthropic take a
// one-token "hi", SystemOne takes messages and answers SSE.
func probeBody(id, routeProtocol string) (path string, body []byte) {
	user := []any{map[string]any{"role": "user", "content": "hi"}}
	switch routeProtocol {
	case "responses":
		path = "/v1/responses"
		body, _ = json.Marshal(map[string]any{"model": id, "stream": false, "max_output_tokens": 1, "input": []any{map[string]any{"role": "user", "content": "hi"}}})
	case "anthropic", "messages":
		path = "/v1/messages"
		body, _ = json.Marshal(map[string]any{"model": id, "max_tokens": 1, "messages": user})
	case "systemone":
		path = "/v1/systemone"
		body, _ = json.Marshal(map[string]any{"model": id, "messages": user})
	default:
		path = "/v1/chat/completions"
		body, _ = json.Marshal(map[string]any{"model": id, "stream": false, "max_tokens": 1, "messages": user})
	}
	return path, body
}

// filterWorkingModels keeps only listed entries that answer a live probe.
// Each probe travels the production inference path (routing, conversion,
// retries) under a diagnostic context, so probes never cool keys, burn
// metrics, or reshape traffic. Order is preserved; the response shape is
// unchanged, only the data array is shorter.
func (g *Gateway) filterWorkingModels(r *http.Request, data []map[string]any) []map[string]any {
	type job struct {
		index         int
		id            string
		routeProtocol string
	}
	jobs := make(chan job)
	alive := make([]bool, len(data))
	var wg sync.WaitGroup
	for w := 0; w < workingProbeWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if r.Context().Err() != nil {
					return
				}
				path, body := probeBody(j.id, j.routeProtocol)
				ctx, cancel := context.WithTimeout(WithDiagnosticRequest(r.Context()), workingProbeTimeout)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://gateway.local"+path, bytes.NewReader(body))
				if err != nil {
					cancel()
					continue
				}
				if auth := r.Header.Get("Authorization"); auth != "" {
					req.Header.Set("Authorization", auth)
				}
				if key := r.Header.Get("x-api-key"); key != "" {
					req.Header.Set("x-api-key", key)
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json")
				req.Header.Set("anthropic-version", "2023-06-01")
				rec := newWorkingRecorder()
				g.Handler().ServeHTTP(rec, req)
				cancel()
				if rec.status >= 200 && rec.status < 300 {
					alive[j.index] = true
				}
			}
		}()
	}
feed:
	for i, entry := range data {
		id, _ := entry["id"].(string)
		proto, _ := entry["route_protocol"].(string)
		if id == "" {
			continue
		}
		select {
		case jobs <- job{index: i, id: id, routeProtocol: proto}:
		case <-r.Context().Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	kept := data[:0]
	for i, entry := range data {
		if alive[i] {
			kept = append(kept, entry)
		}
	}
	return kept
}
