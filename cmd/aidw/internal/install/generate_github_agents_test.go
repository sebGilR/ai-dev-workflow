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

	if err := GenerateGithubAgents(os.DirFS(srcDir), destDir, false); err != nil {
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

// TestGenerateGithubAgents_PruneFalseNeverDeletesUnrelatedFiles guards
// against the SeedRepo call path (prune=false) ever deleting a file a user
// put in their own .github/agents/ that this checkout doesn't know about —
// the exact regression a bare "clean dest before copy" would reintroduce.
func TestGenerateGithubAgents_PruneFalseNeverDeletesUnrelatedFiles(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "src")
	destDir := filepath.Join(tempDir, "dest")

	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "wip-planner.md"), []byte("## Overview\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A user-owned file that is NOT one of this checkout's agents.
	userFile := filepath.Join(destDir, "my-custom-copilot-agent.md")
	if err := os.WriteFile(userFile, []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateGithubAgents(os.DirFS(srcDir), destDir, false); err != nil {
		t.Fatalf("GenerateGithubAgents: %v", err)
	}

	data, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user's own agent file was deleted by prune=false GenerateGithubAgents: %v", err)
	}
	if string(data) != "user content" {
		t.Errorf("user's own agent file was modified, got %q", data)
	}
}

// TestGenerateGithubSkills_PruneFalseNeverDeletesUnrelatedFiles is the
// GenerateGithubSkills analogue of the above.
func TestGenerateGithubSkills_PruneFalseNeverDeletesUnrelatedFiles(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "src", "wip-plan")
	destDir := filepath.Join(tempDir, "dest")

	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: wip-plan\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	userDir := filepath.Join(destDir, "my-custom-skill")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(userDir, "SKILL.md")
	if err := os.WriteFile(userFile, []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateGithubSkills(os.DirFS(filepath.Join(tempDir, "src")), destDir, false); err != nil {
		t.Fatalf("GenerateGithubSkills: %v", err)
	}

	data, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user's own skill file was deleted by prune=false GenerateGithubSkills: %v", err)
	}
	if string(data) != "user content" {
		t.Errorf("user's own skill file was modified, got %q", data)
	}
}
