// Package migrate implements Cluster H's H1 migration engine: copying
// legacy .wip branch-directory state into the work-model store
// (internal/work) via `aidw migrate-state`, without ever deleting the
// legacy source (that is Lane D, a future batch) and without breaking
// wip.go's own direct reads of .wip (D3, "read-through").
//
// migrate never imports internal/wip for logic reuse — the dated-dir and
// legacy-dir pattern matching below is an independent re-implementation
// (see Discover's doc comment), not a call into wip's unexported helpers.
// Only readthrough_test.go references wip, and only its exported
// FindBranchState, to prove D3 holds.
package migrate

import (
	"fmt"
	"os"
	"path/filepath"
)

// globalArchiveDirName mirrors wip.go's own globalArchiveDirName constant
// (".archive") — the one top-level entry under a repo's .wip/ that is never
// a branch directory (it holds ClearWip's/ClearOtherBranches' archived
// content) and must never be treated as a migration source.
const globalArchiveDirName = ".archive"

// SourceKind distinguishes an ordinary per-branch .wip/<branch>/ directory
// from a whole condemned branch tree under the global archive root
// (.wip/.archive/<branch-dir-name>[-N]/, produced by clear-wip/
// clear-others). SourceKindBranch is the zero value so every existing
// SourceDir{...} literal in Discover and all pre-Cluster-I tests stays
// valid with no edits.
type SourceKind int

const (
	SourceKindBranch SourceKind = iota
	SourceKindGlobalArchive
)

// SourceDir pairs one discovered legacy .wip branch directory (or, for
// Kind == SourceKindGlobalArchive, one whole condemned branch tree under
// the global archive root) with the worktree root it was found under.
// WorktreePath and SourceWipDir are kept as two separately-named fields end
// to end (hard rule 4) — they are never merged into one "key" value; the
// mapping file (mapping.go) is the only place they and workID meet.
type SourceDir struct {
	WorktreePath string
	SourceWipDir string
	Kind         SourceKind
}

// Discover implements D2's bounded inventory: for each worktree root in
// roots, scans that worktree's .wip/ directory (if present, otherwise
// silently contributes nothing for that root) and collects EVERY top-level
// directory entry as a migration source, except the one reserved name that
// is never a branch directory (globalArchiveDirName, ".archive").
//
// This deliberately collects both dated branch dirs (the
// `^(\d{8}(?:\d{6})?)-<branch>$` pattern from wip.go's findExistingWipDir,
// wip.go:174) AND legacy unprefixed <branch> dirs (wip.go:204-208's
// fallback) — every match, not just the newest one findExistingWipDir
// returns per branch. findExistingWipDir is, by design, a "pick the newest"
// lookup for a single already-known branch name (wip.go:198-202); reusing
// that "newest-only" semantics here would silently skip older dated dirs
// for branches that were worked on more than once, which is a real
// instance of the "nothing dropped" rule this migration is supposed to
// uphold. Because Discover has no target branch name to filter by (it is
// scanning generically, not resolving one specific branch), and a dated
// dir and a legacy dir are structurally indistinguishable once you strip
// away the specific-branch-name context findExistingWipDir has, every
// non-reserved entry is collected as one SourceDir rather than
// re-deriving wip.go's two-phase, single-branch lookup. This is an
// independent re-implementation of the pattern-matching, not a call into
// wip — internal/migrate never imports internal/wip for logic reuse.
//
// Never walks any path outside the given roots.
func Discover(roots []string) ([]SourceDir, error) {
	var out []SourceDir
	for _, root := range roots {
		wipBase := filepath.Join(root, ".wip")
		entries, err := os.ReadDir(wipBase)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", wipBase, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if e.Name() == globalArchiveDirName {
				continue
			}
			out = append(out, SourceDir{
				WorktreePath: root,
				SourceWipDir: filepath.Join(wipBase, e.Name()),
			})
		}
	}
	return out, nil
}

// DiscoverArchived is Discover's counterpart for the global archive root:
// for each root, lists .wip/.archive/'s top-level entries (a missing
// .archive dir contributes nothing for that root, same convention as
// Discover) and returns one SourceDir per entry, Kind ==
// SourceKindGlobalArchive. One level deep only, no recursion — archive
// entries are never nested inside each other.
func DiscoverArchived(roots []string) ([]SourceDir, error) {
	var out []SourceDir
	for _, root := range roots {
		archiveBase := filepath.Join(root, ".wip", globalArchiveDirName)
		entries, err := os.ReadDir(archiveBase)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", archiveBase, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			out = append(out, SourceDir{
				WorktreePath: root,
				SourceWipDir: filepath.Join(archiveBase, e.Name()),
				Kind:         SourceKindGlobalArchive,
			})
		}
	}
	return out, nil
}
