package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var managedGitignoreLines = []string{
	".wip/",
	".claude/repo-docs/",
	".claude/settings.local.json",
	"CLAUDE.local.md",
}

// UpdateGlobalGitignore adds the managed lines (plus any extra) to the global
// gitignore file. If core.excludesfile is not configured it is set to
// ~/.gitignore_global. Idempotent — no-op if all lines are already present.
func UpdateGlobalGitignore(extra ...string) error {
	giPath, err := globalGitignorePath()
	if err != nil {
		return err
	}
	entries := append(managedGitignoreLines, extra...)
	return updateGitignoreToPath(giPath, entries)
}

// updateGitignoreToPath is the path-injectable core used by tests.
func updateGitignoreToPath(giPath string, entries []string) error {
	var lines []string
	if data, err := os.ReadFile(giPath); err == nil {
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", giPath, err)
	}

	existing := make(map[string]bool, len(lines))
	for _, l := range lines {
		existing[l] = true
	}

	changed := false
	for _, line := range entries {
		if !existing[line] {
			lines = append(lines, line)
			changed = true
		}
	}

	if !changed {
		return nil
	}

	content := strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
	if err := os.MkdirAll(filepath.Dir(giPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(giPath, []byte(content), 0o644)
}

// globalGitignorePath returns the path of the global gitignore file,
// configuring core.excludesfile to ~/.gitignore_global if unset.
func globalGitignorePath() (string, error) {
	out, err := exec.Command("git", "config", "--global", "core.excludesfile").Output()
	if err == nil {
		path := strings.TrimSpace(string(out))
		if path != "" {
			return expandHome(path), nil
		}
	}

	home, herr := os.UserHomeDir()
	if herr != nil {
		return "", fmt.Errorf("home dir: %w", herr)
	}
	path := filepath.Join(home, ".gitignore_global")
	if cerr := exec.Command("git", "config", "--global", "core.excludesfile", path).Run(); cerr != nil {
		return "", fmt.Errorf("set core.excludesfile: %w", cerr)
	}
	return path, nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
