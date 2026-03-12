package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunCountsFields(t *testing.T) {
	results := Run("")
	if len(results.Checks) == 0 {
		t.Fatal("expected at least one check")
	}
	total := results.Passed + results.Failed + results.Warnings
	if total != len(results.Checks) {
		t.Errorf("passed(%d)+failed(%d)+warnings(%d)=%d != len(checks)=%d",
			results.Passed, results.Failed, results.Warnings, total, len(results.Checks))
	}
	if results.OK != (results.Failed == 0) {
		t.Errorf("OK=%v but Failed=%d", results.OK, results.Failed)
	}
}

func TestCheckItemStatusValues(t *testing.T) {
	results := Run("")
	for _, c := range results.Checks {
		switch c.Status {
		case "pass", "FAIL", "warn":
			// ok
		default:
			t.Errorf("unexpected status %q for check %q", c.Status, c.Name)
		}
	}
}

func TestRunWithWorkspace_InvalidPath(t *testing.T) {
	results := Run("/nonexistent/path/that/does/not/exist")
	if results == nil {
		t.Fatal("Run returned nil")
	}
	found := false
	for _, c := range results.Checks {
		if c.Name == "workspace: path exists" && c.Status == "FAIL" {
			found = true
		}
	}
	if !found {
		t.Error("expected a FAIL check for invalid workspace path")
	}
}

func TestRunWithWorkspace_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	results := Run(dir)
	if results == nil {
		t.Fatal("Run returned nil")
	}
	// Should have workspace: checks
	found := false
	for _, c := range results.Checks {
		if len(c.Name) >= 10 && c.Name[:10] == "workspace:" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one workspace: check")
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	if !fileExists(dir) {
		t.Error("dir should exist")
	}
	if fileExists(filepath.Join(dir, "nope")) {
		t.Error("missing file should return false")
	}
	// Create a real file
	f := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExists(f) {
		t.Error("created file should exist")
	}
}

func TestCommandExists(t *testing.T) {
	if !commandExists("sh") {
		t.Error("sh should exist")
	}
	if commandExists("definitely-not-a-real-command-xyz") {
		t.Error("bogus command should not exist")
	}
}
