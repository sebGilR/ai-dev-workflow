package install

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	embedfs "aidw"
)

func TestAskEnableWipGate_NonInteractive_NeverPrompts(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	status := askEnableWipGate(false, &out, strings.NewReader(""), settingsPath)

	if status.Enabled || status.AlreadyEnabled || status.Skipped {
		t.Fatalf("unexpected status: %+v", status)
	}
	if out.Len() != 0 {
		t.Fatalf("non-interactive run must not write any prompt, got %q", out.String())
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "wip-gate.sh") {
		t.Fatal("settings.json must not be touched when non-interactive")
	}
}

func TestAskEnableWipGate_InteractiveYes_MergesFragment(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	status := askEnableWipGate(true, &out, strings.NewReader("y\n"), settingsPath)

	if !status.Enabled {
		t.Fatalf("expected Enabled, got %+v", status)
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "wip-gate.sh") {
		t.Fatalf("settings.json missing wip-gate.sh entry: %s", data)
	}
}

func TestAskEnableWipGate_InteractiveNo_LeavesUnchanged(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	status := askEnableWipGate(true, &out, strings.NewReader("n\n"), settingsPath)

	if !status.Skipped {
		t.Fatalf("expected Skipped, got %+v", status)
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if len(v) != 0 {
		t.Fatalf("settings.json should remain empty, got %v", v)
	}
}

func TestAskEnableWipGate_AlreadyEnabled_SkipsPromptEntirely(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	existing := `{"hooks":{"PreToolUse":[{"matcher":"Edit|Write|NotebookEdit","hooks":[{"type":"command","command":"~/.claude/wip-gate.sh"}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	// stdin is empty; if this prompted, ReadString would just get "" and
	// treat it as "no" — but the point of this test is that it never
	// prompts (writes to w) in the first place.
	status := askEnableWipGate(true, &out, strings.NewReader(""), settingsPath)

	if !status.AlreadyEnabled {
		t.Fatalf("expected AlreadyEnabled, got %+v", status)
	}
	if out.Len() != 0 {
		t.Fatalf("must not write a prompt when already enabled, got %q", out.String())
	}
}

func TestWipGateFragment_ReadableFromEmbedFS(t *testing.T) {
	data, err := embedfs.FS.ReadFile(wipGateFragmentPath)
	if err != nil {
		t.Fatalf("fragment not embedded: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("fragment not valid JSON: %v", err)
	}
}
