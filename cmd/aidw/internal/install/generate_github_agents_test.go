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

// TestGenerateGithubAgents_PruneTrueRemovesOrphans is the positive
// counterpart: with prune=true (the `make mirrors` path) a .md file in
// dest with no corresponding source file MUST be removed, otherwise a
// renamed/deleted agent leaves a stale mirror copy behind forever.
func TestGenerateGithubAgents_PruneTrueRemovesOrphans(t *testing.T) {
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

	orphan := filepath.Join(destDir, "renamed-away.md")
	if err := os.WriteFile(orphan, []byte("stale mirror copy"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateGithubAgents(os.DirFS(srcDir), destDir, true); err != nil {
		t.Fatalf("GenerateGithubAgents: %v", err)
	}

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("prune=true must delete the orphan %s (stat err=%v)", orphan, err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "wip-planner.md")); err != nil {
		t.Errorf("prune=true must keep the file that does have a source: %v", err)
	}
}

// TestGenerateGithubSkills_PruneTrueRemovesOrphans is the skills analogue:
// both an orphan file sitting next to a real skill and a whole orphan
// skill subdirectory must be swept.
func TestGenerateGithubSkills_PruneTrueRemovesOrphans(t *testing.T) {
	tempDir := t.TempDir()
	srcRoot := filepath.Join(tempDir, "src")
	destDir := filepath.Join(tempDir, "dest")

	if err := os.MkdirAll(filepath.Join(srcRoot, "wip-plan"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcRoot, "wip-plan", "SKILL.md"), []byte("---\nname: wip-plan\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// An orphan file inside a directory that DOES still exist in src...
	if err := os.MkdirAll(filepath.Join(destDir, "wip-plan"), 0o755); err != nil {
		t.Fatal(err)
	}
	orphanFile := filepath.Join(destDir, "wip-plan", "OLD-NOTES.md")
	if err := os.WriteFile(orphanFile, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	// ...and a whole orphan skill directory that no longer exists in src.
	orphanDir := filepath.Join(destDir, "wip-removed-skill")
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	orphanDirFile := filepath.Join(orphanDir, "SKILL.md")
	if err := os.WriteFile(orphanDirFile, []byte("stale skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateGithubSkills(os.DirFS(srcRoot), destDir, true); err != nil {
		t.Fatalf("GenerateGithubSkills: %v", err)
	}

	if _, err := os.Stat(orphanFile); !os.IsNotExist(err) {
		t.Errorf("prune=true must delete orphan file %s (stat err=%v)", orphanFile, err)
	}
	if _, err := os.Stat(orphanDirFile); !os.IsNotExist(err) {
		t.Errorf("prune=true must delete orphan skill dir contents %s (stat err=%v)", orphanDirFile, err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "wip-plan", "SKILL.md")); err != nil {
		t.Errorf("prune=true must keep the mirrored skill file: %v", err)
	}
}

// TestGenerateGithubAgents_EmptySrcDoesNotWipeDest is the agents-side twin
// of TestGenerateGithubSkills_EmptySrcDoesNotWipeDest: a src that reads
// fine but is empty must not be treated as "prune everything in dest".
func TestGenerateGithubAgents_EmptySrcDoesNotWipeDest(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "src")
	destDir := filepath.Join(tempDir, "dest")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(destDir, "important.md")
	if err := os.WriteFile(victim, []byte("do not delete me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateGithubAgents(os.DirFS(srcDir), destDir, true); err == nil {
		t.Fatal("expected an error for an empty --src")
	}

	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("dest was wiped for an empty source: %v", err)
	}
	if string(data) != "do not delete me" {
		t.Errorf("dest file was modified, got %q", data)
	}
}

// TestGenerateGithubSkills_UnreadableSrcDoesNotWipeDest guards the exact
// reproduced failure: `--src /nonexistent --dest X --prune` used to delete
// every pre-existing file under X and only then report the bad source.
func TestGenerateGithubSkills_UnreadableSrcDoesNotWipeDest(t *testing.T) {
	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "dest")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(destDir, "important.md")
	if err := os.WriteFile(victim, []byte("do not delete me"), 0o644); err != nil {
		t.Fatal(err)
	}

	missingSrc := os.DirFS(filepath.Join(tempDir, "nonexistent"))
	if err := GenerateGithubSkills(missingSrc, destDir, true); err == nil {
		t.Fatal("expected an error for an unreadable --src")
	}

	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("dest was wiped before the unreadable source was detected: %v", err)
	}
	if string(data) != "do not delete me" {
		t.Errorf("dest file was modified, got %q", data)
	}
}

// TestGenerateGithubSkills_EmptySrcDoesNotWipeDest covers the adjacent
// case: a src that reads fine but is empty must not be treated as "delete
// everything in dest".
func TestGenerateGithubSkills_EmptySrcDoesNotWipeDest(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "src")
	destDir := filepath.Join(tempDir, "dest")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(destDir, "important.md")
	if err := os.WriteFile(victim, []byte("do not delete me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateGithubSkills(os.DirFS(srcDir), destDir, true); err == nil {
		t.Fatal("expected an error for an empty --src")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("dest was wiped for an empty source: %v", err)
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
