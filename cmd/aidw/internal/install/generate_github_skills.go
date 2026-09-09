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
// sections to strip. It removes any file under destDir that no longer
// exists in srcFS first, so the mirror never accumulates orphaned copies
// of deleted/renamed skills, then copies srcFS over it. The result is
// byte-identical to srcFS, which is what the mirrors drift test asserts.
func GenerateGithubSkills(srcFS fs.FS, destDir string) error {
	if err := removeOrphanTree(srcFS, destDir); err != nil {
		return fmt.Errorf("clean dest dir: %w", err)
	}
	if err := util.CopyFS(srcFS, destDir); err != nil {
		return fmt.Errorf("copy skills: %w", err)
	}
	return nil
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
