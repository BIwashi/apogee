package collector

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the embedded Next.js static export. It delegates the
// actual byte-slinging to http.FileServer(http.FS(assets)) for any request
// that maps to a real file, and falls back to `/index.html` (or the nearest
// segment directory index) for anything that does not. That mirrors the
// hosting convention every static hosting service uses for client-routed
// SPAs, and handles the trailing-slash directories emitted by
// `output: 'export'` + `trailingSlash: true`.
//
// Cache-Control: hashed `_next/static/*` assets ship with a one-year
// immutable cache, while HTML documents are marked `no-cache` so the dev can
// iterate on the dashboard without browsers pinning stale entrypoints.
//
// The handler is deliberately mounted last, so the `/v1/*` API routes take
// precedence and never fall through to the UI.
func spaHandler(assets fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Strip leading slash for io/fs lookups. io/fs disallows leading
		// slashes but http.FileServer is happy with either form when
		// writing the response.
		clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		if clean == "/" {
			serveIndex(w, r, assets)
			return
		}
		fsPath := strings.TrimPrefix(clean, "/")

		// Try the exact file first.
		if file, err := assets.Open(fsPath); err == nil {
			file.Close()
			setCacheHeaders(w, fsPath)
			fileServer.ServeHTTP(w, r)
			return
		}

		// Try the directory's index.html (Next.js trailing-slash export).
		indexPath := path.Join(fsPath, "index.html")
		if file, err := assets.Open(indexPath); err == nil {
			file.Close()
			setCacheHeaders(w, indexPath)
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fall back to the root index.html so client-side routes resolve.
		// We read it directly rather than rewriting the URL so the path
		// seen by the browser stays identical.
		serveIndex(w, r, assets)
	})
}

// serveIndex writes `index.html` from the embedded FS with no-cache headers.
// Used for `/` and every 404 fallthrough.
func serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	data, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "apogee dashboard bundle not available", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// setCacheHeaders applies long-lived caching to hashed Next.js assets and a
// shorter policy to everything else. The heuristic is simple on purpose:
// Next.js writes every hashed asset under `_next/static/`, so anything under
// that prefix is safe to cache aggressively.
func setCacheHeaders(w http.ResponseWriter, fsPath string) {
	switch {
	case strings.HasPrefix(fsPath, "_next/static/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case strings.HasSuffix(fsPath, ".html"):
		w.Header().Set("Cache-Control", "no-cache")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
}
