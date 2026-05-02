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
	geminiMDSeed = "# Global Gemini CLI Instructions\n\n"
)

// MergeMD inserts or replaces the managed block in mdPath using the content from
// snippetBytes. If mdPath does not exist it is seeded with a minimal header
// (seed) first. If the sentinels are absent the snippet is appended to the end
// of the file.
func MergeMD(mdPath, seed string, snippetBytes []byte) error {
	snippet := strings.TrimSpace(string(snippetBytes)) + "\n"

	var content string
	if data, err := os.ReadFile(mdPath); err == nil {
		content = string(data)
	} else {
		content = seed
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
	return util.AtomicWrite(mdPath, []byte(merged), 0o644)
}

// MergeCLAUDEMd inserts or replaces the managed block in CLAUDE.md.
func MergeCLAUDEMd(claudeMDPath string, snippetBytes []byte) error {
	return MergeMD(claudeMDPath, claudeMDSeed, snippetBytes)
}

// MergeGEMINIMd inserts or replaces the managed block in GEMINI.md.
func MergeGEMINIMd(geminiMDPath string, snippetBytes []byte) error {
	return MergeMD(geminiMDPath, geminiMDSeed, snippetBytes)
}
