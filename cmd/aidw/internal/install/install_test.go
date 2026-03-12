package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- MergeCLAUDEMd ---

func TestMergeCLAUDEMd_SeedsIfMissing(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	snippet := filepath.Join(dir, "snippet.md")
	if err := os.WriteFile(snippet, []byte("## BEGIN AI-DEV-WORKFLOW MANAGED BLOCK\nmy snippet\n## END AI-DEV-WORKFLOW MANAGED BLOCK\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MergeCLAUDEMd(claudeMD, snippet); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(claudeMD)
	content := string(data)
	if !strings.Contains(content, "Global Claude Code Instructions") {
		t.Error("seed header missing")
	}
	if !strings.Contains(content, "my snippet") {
		t.Error("snippet content missing")
	}
}

func TestMergeCLAUDEMd_ReplacesSentinels(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	snippet := filepath.Join(dir, "snippet.md")

	existing := "# Instructions\n\n## BEGIN AI-DEV-WORKFLOW MANAGED BLOCK\nold content\n## END AI-DEV-WORKFLOW MANAGED BLOCK\n\nExtra section.\n"
	if err := os.WriteFile(claudeMD, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snippet, []byte("## BEGIN AI-DEV-WORKFLOW MANAGED BLOCK\nnew content\n## END AI-DEV-WORKFLOW MANAGED BLOCK\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MergeCLAUDEMd(claudeMD, snippet); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(claudeMD)
	content := string(data)
	if strings.Contains(content, "old content") {
		t.Error("old content should have been replaced")
	}
	if !strings.Contains(content, "new content") {
		t.Error("new content missing")
	}
	if !strings.Contains(content, "Extra section.") {
		t.Error("content after sentinels should be preserved")
	}
}

func TestMergeCLAUDEMd_AppendIfNoSentinels(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	snippet := filepath.Join(dir, "snippet.md")

	if err := os.WriteFile(claudeMD, []byte("# My Docs\n\nSome text.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snippet, []byte("Appended content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MergeCLAUDEMd(claudeMD, snippet); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(claudeMD)
	content := string(data)
	if !strings.Contains(content, "Some text.") {
		t.Error("existing content should be preserved")
	}
	if !strings.Contains(content, "Appended content") {
		t.Error("appended content missing")
	}
}

// --- MergeSettings ---

func TestMergeSettings_SeedsIfMissing(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	tmpl := filepath.Join(dir, "template.json")
	if err := os.WriteFile(tmpl, []byte(`{"key":"value","permissions":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MergeSettings(settings, tmpl); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(settings)
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result["key"] != "value" {
		t.Errorf("key = %v, want value", result["key"])
	}
}

func TestMergeSettings_UserScalarWins(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	tmpl := filepath.Join(dir, "template.json")

	if err := os.WriteFile(settings, []byte(`{"theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpl, []byte(`{"theme":"light","newKey":"newVal"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MergeSettings(settings, tmpl); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(settings)
	var result map[string]any
	json.Unmarshal(data, &result)

	if result["theme"] != "dark" {
		t.Errorf("user scalar should win; theme = %v, want dark", result["theme"])
	}
	if result["newKey"] != "newVal" {
		t.Error("template-only key should be added")
	}
}

func TestMergeSettings_InvalidJSONBacksUp(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	tmpl := filepath.Join(dir, "template.json")

	if err := os.WriteFile(settings, []byte("NOT JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpl, []byte(`{"k":"v"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MergeSettings(settings, tmpl); err != nil {
		t.Fatal(err)
	}
	// Backup should exist
	bak := settings[:len(settings)-len(".json")] + ".json.bak"
	if _, err := os.Stat(bak); err != nil {
		t.Error("backup file should exist after invalid JSON")
	}
	// New settings should be valid
	data, _ := os.ReadFile(settings)
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Error("resulting settings.json should be valid JSON")
	}
}

func TestMergeSettings_ListsDeduplicate(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	tmpl := filepath.Join(dir, "template.json")

	if err := os.WriteFile(settings, []byte(`{"items":["a","b"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpl, []byte(`{"items":["b","c"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MergeSettings(settings, tmpl); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(settings)
	var result map[string]any
	json.Unmarshal(data, &result)

	items, _ := result["items"].([]any)
	if len(items) != 3 {
		t.Errorf("expected 3 deduplicated items, got %d: %v", len(items), items)
	}
}

// --- MergeMCPJSON ---

func TestMergeMCPJSON_AddsServers(t *testing.T) {
	// Override home by using a temp dir trick: write mcp.json ourselves
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, ".claude", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(mcpPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcpPath, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Call the internal merge logic directly (bypasses home dir lookup)
	if err := mergeMCPJSONToPath(mcpPath); err != nil {
		t.Fatalf("mergeMCPJSONToPath: %v", err)
	}

	data, _ := os.ReadFile(mcpPath)
	var result map[string]any
	json.Unmarshal(data, &result)

	servers, _ := result["mcpServers"].(map[string]any)
	if _, ok := servers["serena"]; !ok {
		t.Error("serena server should be added")
	}
	if _, ok := servers["context7"]; !ok {
		t.Error("context7 server should be added")
	}
}

func TestMergeMCPJSON_NoopIfAlreadyConfigured(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, ".claude", "mcp.json")
	os.MkdirAll(filepath.Dir(mcpPath), 0o755)

	// Write with canonical serena and context7 already present
	canonical, _ := json.Marshal(map[string]any{"mcpServers": mcpServers})
	os.WriteFile(mcpPath, canonical, 0o644)

	fi1, _ := os.Stat(mcpPath)
	if err := mergeMCPJSONToPath(mcpPath); err != nil {
		t.Fatal(err)
	}
	fi2, _ := os.Stat(mcpPath)

	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Error("file should not be rewritten when already configured")
	}
}

func TestMergeMCPJSON_InvalidJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, ".claude", "mcp.json")
	os.MkdirAll(filepath.Dir(mcpPath), 0o755)
	os.WriteFile(mcpPath, []byte("{not valid json}"), 0o644)

	fi1, _ := os.Stat(mcpPath)
	err := mergeMCPJSONToPath(mcpPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	fi2, _ := os.Stat(mcpPath)
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Error("file should not be modified when JSON is invalid")
	}
}

func TestMergeMCPJSON_PreservesUserFields(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, ".claude", "mcp.json")
	os.MkdirAll(filepath.Dir(mcpPath), 0o755)

	// Write stale serena (old args) with a user-added field
	stale := map[string]any{
		"mcpServers": map[string]any{
			"serena": map[string]any{
				"command": "uvx",
				"args":    []any{"serena@old-version"},
				"env":     map[string]any{"MY_KEY": "my_value"},
			},
		},
	}
	raw, _ := json.Marshal(stale)
	os.WriteFile(mcpPath, raw, 0o644)

	if err := mergeMCPJSONToPath(mcpPath); err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	data, _ := os.ReadFile(mcpPath)
	json.Unmarshal(data, &result)

	servers := result["mcpServers"].(map[string]any)
	serena := servers["serena"].(map[string]any)
	if serena["env"] == nil {
		t.Error("user-added env field should be preserved after stale update")
	}
	args := serena["args"].([]any)
	if len(args) == 0 || args[0] == "serena@old-version" {
		t.Error("args should be updated to current canonical version")
	}
}

// --- UpdateGlobalGitignore ---

func TestUpdateGlobalGitignore_AddsLines(t *testing.T) {
	dir := t.TempDir()
	giPath := filepath.Join(dir, ".gitignore_global")

	if err := os.WriteFile(giPath, []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := updateGitignoreToPath(giPath, managedGitignoreLines); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(giPath)
	content := string(data)
	for _, line := range managedGitignoreLines {
		if !strings.Contains(content, line) {
			t.Errorf("missing managed line %q", line)
		}
	}
	if !strings.Contains(content, "*.log") {
		t.Error("existing content should be preserved")
	}
}

func TestUpdateGlobalGitignore_Idempotent(t *testing.T) {
	dir := t.TempDir()
	giPath := filepath.Join(dir, ".gitignore_global")

	// Pre-populate with all managed lines
	if err := os.WriteFile(giPath, []byte(strings.Join(managedGitignoreLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi1, _ := os.Stat(giPath)

	if err := updateGitignoreToPath(giPath, managedGitignoreLines); err != nil {
		t.Fatal(err)
	}
	fi2, _ := os.Stat(giPath)

	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Error("file should not be rewritten when already up to date")
	}
}
