package serve

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// dashboardHandler serves the embedded SPA: real files when they exist, else
// index.html (client-side routing fallback).
func dashboardHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(sub, trimLeadingSlash(r.URL.Path)); err == nil && r.URL.Path != "/" {
			fileServer.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "dashboard not built", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}
