// Package webassets embeds the apogee dashboard static export.
//
// The Next.js build writes its static output into `web/out/`. A Makefile step
// (`make web-build`) copies that tree into `internal/webassets/dist/` so the
// `//go:embed all:dist` directive below can pick it up at compile time. The
// directory is git-tracked with a small placeholder `index.html` so
// `go build ./...` works out of the box — including from `go install` users
// who have never run the web build. When the placeholder is detected the
// collector logs a warning at startup.
package webassets

import (
	"bytes"
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// placeholderMarker is a unique string emitted by the checked-in placeholder
// `dist/index.html`. IsPlaceholder reads it to decide whether the binary was
// built without the real dashboard bundle.
const placeholderMarker = "APOGEE_WEBASSETS_PLACEHOLDER"

// Assets returns the embedded filesystem rooted at `dist/`. Callers get an
// io/fs.FS that looks like the top of the Next.js export:
//
//	/index.html, /session/index.html, /_next/static/..., etc.
func Assets() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// The embed directive is compile-time; a nil sub at runtime would
		// be a programmer error in this package.
		panic(err)
	}
	return sub
}

// IsPlaceholder reports whether the embedded dist is still the checked-in
// stub (i.e. the dashboard was never built). The collector surfaces this as
// a startup warning so `go install` users understand why the UI looks bare.
func IsPlaceholder() bool {
	data, err := fs.ReadFile(Assets(), "index.html")
	if err != nil {
		return true
	}
	return bytes.Contains(data, []byte(placeholderMarker))
}
