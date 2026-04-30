package install

import (
	"os"
	"strings"

	"aidw/cmd/aidw/internal/util"
)

const (
	managedStart = "## BEGIN AI-DEV-WORKFLOW MANAGED BLOCK"
	managedEnd   = "## END AI-DEV-WORKFLOW MANAGED BLOCK"
	claudeMDSeed = "# Global Claude Code Instructions\n\n"
)

// MergeCLAUDEMd inserts or replaces the managed block in claudeMDPath using
// the content from snippetBytes. If claudeMDPath does not exist it is seeded
// with a minimal header first. If the sentinels are absent the snippet is
// appended to the end of the file.
func MergeCLAUDEMd(claudeMDPath string, snippetBytes []byte) error {
	snippet := strings.TrimSpace(string(snippetBytes)) + "\n"

	var content string
	if data, err := os.ReadFile(claudeMDPath); err == nil {
		content = string(data)
	} else {
		content = claudeMDSeed
	}

	var merged string
	if strings.Contains(content, managedStart) && strings.Contains(content, managedEnd) {
		pre := strings.TrimRight(strings.SplitN(content, managedStart, 2)[0], " \t")
		post := strings.TrimLeft(strings.SplitN(content, managedEnd, 2)[1], "\n")
		merged = pre + "\n\n" + snippet + "\n" + post
	} else {
		merged = strings.TrimRight(content, "\n") + "\n\n" + snippet
	}

	merged = strings.TrimSpace(merged) + "\n"
	return util.AtomicWrite(claudeMDPath, []byte(merged), 0o644)
}
