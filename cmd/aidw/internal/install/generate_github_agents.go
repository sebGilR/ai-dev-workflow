package install

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"aidw/cmd/aidw/internal/util"
)

// GenerateGithubAgents reads markdown files from srcDir, strips out the
// "### 1. Serena MCP" section, renumbers subsequent numbered sections,
// and writes the result to destDir. It is idempotent.
func GenerateGithubAgents(srcDir, destDir string) error {
	info, err := os.Stat(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Nothing to do
		}
		return fmt.Errorf("stat src dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("src_dir is not a directory: %s", srcDir)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("mkdir dest dir: %w", err)
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read src dir: %w", err)
	}

	headingNumRegex := regexp.MustCompile(`^### \d+\.(.*)`)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		srcPath := filepath.Join(srcDir, e.Name())
		destPath := filepath.Join(destDir, e.Name())

		content, err := processAgentFile(srcPath, headingNumRegex)
		if err != nil {
			return fmt.Errorf("process %s: %w", e.Name(), err)
		}

		// Skip write if content is identical (idempotent).
		if existing, err := os.ReadFile(destPath); err == nil {
			if string(existing) == content {
				continue
			}
			fmt.Fprintf(os.Stderr, "WARNING: Overwriting %s (content differs from generated version).\n", destPath)
			fmt.Fprintln(os.Stderr, "         Any customizations in this file will be lost.")
		}

		if err := util.AtomicWrite(destPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", destPath, err)
		}
	}

	return nil
}

func processAgentFile(srcPath string, headingNumRegex *regexp.Regexp) (string, error) {
	file, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var sb strings.Builder
	scanner := bufio.NewScanner(file)
	skip := false
	n := 1

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "### ") && strings.Contains(line, "Serena MCP") {
			skip = true
			continue
		}

		if skip {
			if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "## ") {
				skip = false
			} else {
				continue
			}
		}

		if match := headingNumRegex.FindStringSubmatch(line); match != nil {
			line = fmt.Sprintf("### %d.%s", n, match[1])
			n++
		}

		sb.WriteString(line)
		sb.WriteString("\n")
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return sb.String(), nil
}
