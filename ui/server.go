package ui

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web
var webFS embed.FS

// Server holds the console's dependencies.
type Server struct {
	Base       string         // workspace base path
	Addr       string         // temporal address (for CLI + display)
	Temporal   TemporalReader // read-only Temporal access (may be nil if offline)
	Control    Controller     // mutations via temporal CLI
	FFmpegPath string         // "ffmpeg" by default
}

// Handler builds the route table. The register* methods are filled in by later
// tasks; the static file server is the catch-all.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	s.registerRunRoutes(mux)
	s.registerSegmentRoutes(mux)
	s.registerControlRoutes(mux)

	static, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(static)))
	return mux
}

// Temporary route-group stub — replaced in Task 15.
func (s *Server) registerControlRoutes(mux *http.ServeMux) {}

// reqCtx is a small helper for handlers needing a request-scoped context.
func reqCtx(r *http.Request) context.Context { return r.Context() }
