package httpapi

import (
	"io/fs"
	"net/http"
	"strings"
)

// spaHandler serves fsys (the built web frontend), falling back to
// index.html for any path that doesn't resolve to a real file — harmless
// today since the Svelte app has no client-side routing, but cheap
// insurance against a hard refresh on a future deep link 404ing.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "."
		}
		if _, err := fs.Stat(fsys, p); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}
