package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aidw/cmd/aidw/internal/git"
	"aidw/cmd/aidw/internal/util"
)

// DocumentProject performs a research pass on the repo and generates/updates
// documentation in .claude/repo-docs/.
func DocumentProject(repoPath string) error {
	top, err := git.Toplevel(repoPath)
	if err != nil {
		return err
	}

	docsDir := filepath.Join(top, ".claude", "repo-docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return err
	}

	// 1. Scan for key files
	coreFiles, err := scanCoreFiles(top)
	if err != nil {
		return err
	}

	// 2. Generate/Update architecture.md
	if err := generateArchitecture(docsDir, top, coreFiles); err != nil {
		return err
	}

	// 3. Generate/Update patterns.md
	if err := generatePatterns(docsDir, top, coreFiles); err != nil {
		return err
	}

	return nil
}

func scanCoreFiles(top string) ([]string, error) {
	var core []string
	patterns := []string{
		"main.go", "index.ts", "app.py", "manage.py",
		"package.json", "go.mod", "requirements.txt", "Gemfile",
		"docker-compose.yml", "Makefile", "README.md",
	}

	err := filepath.Walk(top, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == "node_modules" || info.Name() == ".git" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		for _, p := range patterns {
			if info.Name() == p {
				rel, _ := filepath.Rel(top, path)
				core = append(core, rel)
				break
			}
		}
		return nil
	})
	return core, err
}

func generateArchitecture(docsDir, top string, coreFiles []string) error {
	dest := filepath.Join(docsDir, "architecture.md")
	if _, err := os.Stat(dest); err == nil {
		// Skip if already exists for now, or update if significantly changed.
		return nil
	}

	var sb strings.Builder
	sb.WriteString("# Project Architecture\n\n")
	sb.WriteString("## Overview\nThis project is automatically documented by `aidw`.\n\n")
	sb.WriteString("## Core Components\n")
	for _, f := range coreFiles {
		sb.WriteString(fmt.Sprintf("- `%s`\n", f))
	}
	sb.WriteString("\n## Entry Points\n(To be filled by Analyst agent during research pass)\n")

	return util.AtomicWrite(dest, []byte(sb.String()), 0o644)
}

func generatePatterns(docsDir, top string, coreFiles []string) error {
	dest := filepath.Join(docsDir, "patterns.md")
	if _, err := os.Stat(dest); err == nil {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("# Code Patterns and Conventions\n\n")
	sb.WriteString("## General Style\n- Language: detected via build files\n")
	sb.WriteString("- Conventions: (Follow existing codebase style)\n\n")
	sb.WriteString("## Common Tasks\n- Build: `make build` (if Makefile present)\n")
	sb.WriteString("- Test: `go test ./...` (if go.mod present)\n")

	return util.AtomicWrite(dest, []byte(sb.String()), 0o644)
}
