package admin

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"zenflash-llm/internal/buildinfo"
	"zenflash-llm/internal/httpx"
	"zenflash-llm/webui"
)

// serveSPA serves the embedded dashboard for every non-API route, falling back
// to index.html so client-side state (active tab) survives a reload.
func (a *Server) serveSPA() http.HandlerFunc {
	root, err := webui.Dist()
	if err != nil {
		return func(w http.ResponseWriter, r *http.Request) {
			writeAdminError(w, http.StatusInternalServerError, "ui_unavailable", "dashboard assets are not embedded")
		}
	}
	fileServer := http.FileServer(http.FS(root))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeAdminError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api") {
			writeAdminError(w, http.StatusNotFound, "not_found", "unknown management route")
			return
		}
		cleaned := path.Clean("/" + r.URL.Path)
		if cleaned != "/" {
			if info, err := fs.Stat(root, strings.TrimPrefix(cleaned, "/")); err == nil && !info.IsDir() {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		http.ServeFileFS(w, r2, root, "index.html")
	}
}

// handleEvents streams a live snapshot (metrics/resources/usage) to the
// dashboard every ~2 seconds as `tick` events.
func (a *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAdminError(w, http.StatusInternalServerError, "stream_unsupported", "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	payload := func() map[string]any {
		metrics := a.monitor.Snapshot()
		return map[string]any{
			"version":   buildinfo.Version,
			"metrics":   metrics,
			"usage":     metrics.Usage,
			"upstream":  metrics.Upstream,
			"resources": a.manager.Resources(),
		}
	}
	write := func() error {
		return httpx.WriteSSE(w, "tick", 0, payload())
	}
	if err := write(); err != nil {
		return
	}
	flusher.Flush()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	cookie, _ := r.Cookie(adminCookieName)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if err := write(); err != nil {
				return
			}
			flusher.Flush()
			// In credential-free mode there is no cookie to watch; otherwise
			// drop the stream once the session is no longer valid.
			if !a.openAdmin() && (cookie == nil || !a.sessionTokenValid(cookie.Value)) {
				return
			}
		}
	}
}
