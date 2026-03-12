// Package embedfs exposes the embedded templates filesystem.
// This file lives at the module root so that //go:embed can reference
// the templates/ directory without using ".." path elements (which are
// forbidden by the Go embed spec).
package embedfs

import "embed"

// FS contains all files under templates/, accessible as "templates/<name>".
//
//go:embed templates
var FS embed.FS
