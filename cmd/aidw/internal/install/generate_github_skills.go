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
	if prune {
		if err := removeOrphanTree(srcFS, destDir); err != nil {
			return fmt.Errorf("clean dest dir: %w", err)
		}
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
