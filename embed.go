// Package embedfs exposes the embedded templates, skills, and agents filesystem.
// This file lives at the module root so that //go:embed can reference
// these directories without using ".." path elements (which are
// forbidden by the Go embed spec).
package embedfs

import "embed"

// FS contains:
// - templates/
// - claude/skills/
// - claude/agents/
//
//go:embed templates claude/skills claude/agents bin/serena-query
var FS embed.FS
