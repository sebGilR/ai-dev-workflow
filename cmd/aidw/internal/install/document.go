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

	// 2. Generate placeholders that the Analyst Agent will fill
	if err := generateArchitecture(docsDir, top, coreFiles); err != nil {
		return err
	}

	if err := generatePatterns(docsDir, top, coreFiles); err != nil {
		return err
	}
	
	if err := generateGotchas(docsDir); err != nil {
		return err
	}

	return nil
}

func scanCoreFiles(top string) ([]string, error) {
	var core []string
	patterns := []string{
		"package.json", "go.mod", "requirements.txt", "Gemfile",
		"docker-compose.yml", "Makefile", "README.md", "CLAUDE.md",
	}
	
	// Smarter scan: only look at root and immediate subdirs for entry points
	entries, _ := os.ReadDir(top)
	for _, e := range entries {
		if e.IsDir() {
			// Skip noisy dirs
			name := e.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".go" || name == "bin" {
				continue
			}
			
			// Look for main files in subdirs
			subPath := filepath.Join(top, name)
			subEntries, _ := os.ReadDir(subPath)
			for _, se := range subEntries {
				if se.Name() == "main.go" || se.Name() == "index.ts" || se.Name() == "app.py" {
					rel, _ := filepath.Rel(top, filepath.Join(subPath, se.Name()))
					core = append(core, rel)
				}
			}
		} else {
			for _, p := range patterns {
				if e.Name() == p {
					core = append(core, e.Name())
					break
				}
			}
			if e.Name() == "main.go" || e.Name() == "index.ts" || e.Name() == "app.py" {
				core = append(core, e.Name())
			}
		}
	}
	
	return core, nil
}

func generateArchitecture(docsDir, top string, coreFiles []string) error {
	dest := filepath.Join(docsDir, "architecture.md")
	if _, err := os.Stat(dest); err == nil {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("# Project Architecture\n\n")
	sb.WriteString("<!-- ANALYST_TODO: Replace this skeleton with a high-level architectural overview. -->\n\n")
	sb.WriteString("## System Overview\n(Describe the primary purpose and flow of the system)\n\n")
	sb.WriteString("## Core Components (Detected)\n")
	for _, f := range coreFiles {
		sb.WriteString(fmt.Sprintf("- `%s`\n", f))
	}
	sb.WriteString("\n## Data Flow\n(Describe how data moves through the system)\n")

	return util.AtomicWrite(dest, []byte(sb.String()), 0o644)
}

func generatePatterns(docsDir, top string, coreFiles []string) error {
	dest := filepath.Join(docsDir, "patterns.md")
	if _, err := os.Stat(dest); err == nil {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("# Code Patterns and Conventions\n\n")
	sb.WriteString("<!-- ANALYST_TODO: Analyze the core files and describe the coding patterns used. -->\n\n")
	sb.WriteString("## Error Handling\n(Describe how errors are handled and propagated)\n\n")
	sb.WriteString("## Testing Pattern\n(Describe the testing strategy and tools)\n\n")
	sb.WriteString("## CLI/API Patterns\n(Describe how interfaces are defined)\n")

	return util.AtomicWrite(dest, []byte(sb.String()), 0o644)
}

func generateGotchas(docsDir string) error {
	dest := filepath.Join(docsDir, "gotchas.md")
	if _, err := os.Stat(dest); err == nil {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("# Project Gotchas\n\n")
	sb.WriteString("<!-- ANALYST_TODO: Record any non-obvious quirks or environment-specific requirements. -->\n\n")
	sb.WriteString("## Environment\n- (e.g. Requires specific gcloud auth for embeddings)\n\n")
	sb.WriteString("## Known Issues\n- (List any ongoing architectural debt or tricky segments)\n")

	return util.AtomicWrite(dest, []byte(sb.String()), 0o644)
}
