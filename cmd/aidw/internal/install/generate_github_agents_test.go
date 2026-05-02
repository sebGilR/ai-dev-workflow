package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateGithubAgents(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "src")
	destDir := filepath.Join(tempDir, "dest")

	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	input := `## Overview

### 1. Serena MCP
This should be skipped.
Some text.

### 2. Next Step
This should be 1. Next Step.

## Another heading
### 3. Final Step
This should be 2. Final Step.
`

	expected := `## Overview

### 1. Next Step
This should be 1. Next Step.

## Another heading
### 2. Final Step
This should be 2. Final Step.
`

	srcFile := filepath.Join(srcDir, "agent.md")
	if err := os.WriteFile(srcFile, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateGithubAgents(os.DirFS(srcDir), destDir); err != nil {
		t.Fatalf("GenerateGithubAgents failed: %v", err)
	}

	destFile := filepath.Join(destDir, "agent.md")
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	if string(content) != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, string(content))
	}
}
