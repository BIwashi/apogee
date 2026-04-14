// Package apogee exposes the embedded Python hook library as an
// io/fs.FS. The package is otherwise empty — its sole purpose is to provide
// an embed target whose directory is an ancestor of ``hooks/``.
//
// The consumer of this package is internal/cli, which extracts the embedded
// files into ``~/.apogee/hooks/<version>/`` during ``apogee init``.
package apogee

import "embed"

//go:embed all:hooks
var embeddedHooks embed.FS

// HooksFS returns the embedded Python hook library as an [embed.FS]. The
// root of the returned filesystem is the repository root; callers typically
// walk into the ``hooks`` subdirectory.
func HooksFS() embed.FS {
	return embeddedHooks
}
