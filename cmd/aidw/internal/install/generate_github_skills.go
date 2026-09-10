package install

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"aidw/cmd/aidw/internal/util"
)

// GenerateGithubSkills mirrors srcFS (claude/skills/) into destDir
// (.github/skills/) verbatim — unlike agents, skills carry no MCP-specific
// sections to strip.
//
// prune controls whether files under destDir with no corresponding source
// file are removed first. As with GenerateGithubAgents, this MUST be false
// when destDir is an arbitrary user repository — pruning there would
// delete files a user added to their own .github/skills/ that this
// checkout's claude/skills/ knows nothing about. Pass true only for this
// checkout's own mirror-generation entry points (the generate-github-skills
// CLI command / `make mirrors`), where the result is meant to be exactly
// byte-identical to srcFS, which is what the mirrors drift test asserts.
func GenerateGithubSkills(srcFS fs.FS, destDir string, prune bool) error {
	// Validate the source is readable and non-empty BEFORE doing anything
	// destructive. Without this, `--src /nonexistent --prune` would first
	// delete every file under destDir (removeOrphanTree treats every dest
	// file as an orphan when fs.Stat on srcFS always fails) and only then
	// report the unreadable source. GenerateGithubAgents ReadDirs src up
	// front for the same reason.
	entries, err := fs.ReadDir(srcFS, ".")
	if err != nil {
		return fmt.Errorf("read src dir: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("read src dir: source is empty — refusing to mirror an empty tree over %s", destDir)
	}

	if prune {
		if err := removeOrphanTree(srcFS, destDir); err != nil {
			return fmt.Errorf("clean dest dir: %w", err)
		}
	}
	warnOnSkillOverwrites(srcFS, destDir)

	if err := util.CopyFS(srcFS, destDir); err != nil {
		return fmt.Errorf("copy skills: %w", err)
	}
	return nil
}

// warnOnSkillOverwrites prints the same "customizations will be lost"
// warning GenerateGithubAgents emits, for every dest file that already
// exists with content differing from its source. Best-effort: a read error
// on either side is simply not warned about — this must never fail the
// copy itself.
func warnOnSkillOverwrites(srcFS fs.FS, destDir string) {
	_ = fs.WalkDir(srcFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // best-effort warning only
		}
		destPath := filepath.Join(destDir, filepath.FromSlash(path))
		existing, readErr := os.ReadFile(destPath)
		if readErr != nil {
			return nil
		}
		want, readErr := fs.ReadFile(srcFS, path)
		if readErr != nil || string(existing) == string(want) {
			return nil
		}
		fmt.Fprintf(os.Stderr, "WARNING: Overwriting %s (content differs from generated version).\n", destPath)
		fmt.Fprintln(os.Stderr, "         Any customizations in this file will be lost.")
		return nil
	})
}

// removeOrphanTree deletes any file under destDir whose relative path does
// not exist in srcFS. Directories that become empty as a result are also
// removed. destDir not existing yet is not an error.
func removeOrphanTree(srcFS fs.FS, destDir string) error {
	if _, err := os.Stat(destDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return filepath.Walk(destDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(destDir, path)
		if err != nil {
			return err
		}
		if _, statErr := fs.Stat(srcFS, filepath.ToSlash(rel)); statErr != nil {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove orphan %s: %w", rel, err)
			}
		}
		return nil
	})
}
