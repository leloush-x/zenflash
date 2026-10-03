package admin

import (
	"net/http"
	"strings"
)

// openAI compatible aliases: many SDKs and the OpenCode client get a base URL
// that lacks the "/v1" prefix. Reach the same manager handler under both
// /<path> and /v1/<path> so whichever port is pointed at, /{chat,completions}
// or /{responses} works regardless of whether the admin SPA fallback is the
// first handler in the chain.
func (a *Server) serveAPIAs(prefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rewritten := r.Clone(r.Context())
		rewritten.URL.Path = prefix + r.URL.Path
		a.manager.Handler().ServeHTTP(w, rewritten)
	}
}

// registerOpenAIAlias mounts the manager handler at the bare "/v1"-less
// OpenAI endpoint names (chat/completions, responses, messages, models,
// systemone) so base URLs without a "/v1" prefix still route.
func (a *Server) registerOpenAIAlias(mux *http.ServeMux) {
	const proxyPaths = "/chat/completions /responses /messages /systemone"
	for _, p := range strings.Split(proxyPaths, " ") {
		mux.Handle(p, a.serveAPIAs("/v1"))
	}
	mux.Handle("/models", a.serveAPIAs("/v1"))
}

// aliasAPI maps the bare OpenAI paths ("/chat/completions", "/responses",
// "/messages", "/systemone", "/models") and bare "/healthz" onto the manager
// handler directly on the admin mux, so the OpenCode/vercel client can be
// aimed at either the public gateway port or the admin port without a "/v1".
func (a *Server) aliasRouting(mux *http.ServeMux) {
	mux.Handle("/v1/", a.manager.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		a.manager.Handler().ServeHTTP(w, r)
	})
	rewrittenPaths := []string{"/chat/completions", "/responses", "/messages", "/systemone", "/models"}
	for _, path := range rewrittenPaths {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/v1" + r.URL.Path
			a.manager.Handler().ServeHTTP(w, r2)
		})
	}
}
