package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSetWipGate_NoPriorFile_WritesOnlyWipGate(t *testing.T) {
	dir := t.TempDir()
	if err := SetWipGate(dir, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".aidw", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected exactly one key, got %v", raw)
	}
	if string(raw["wip_gate"]) != `"disabled"` {
		t.Fatalf("wip_gate = %s", raw["wip_gate"])
	}
}

func TestSetWipGate_ThenLoad_RulesBackfilledDynamically(t *testing.T) {
	dir := t.TempDir()
	if err := SetWipGate(dir, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Rules, DefaultConfig().Rules) {
		t.Fatalf("rules not backfilled to current defaults: %+v", cfg.Rules)
	}
	if cfg.WipGate != "disabled" {
		t.Fatalf("WipGate = %q", cfg.WipGate)
	}
}

func TestSetWipGate_PreservesExistingCustomRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".aidw"), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := `{
  "rules": [
    {
      "pattern": "^my-custom-cmd",
      "verdict": "allow",
      "reason": "custom"
    }
  ]
}
`
	path := filepath.Join(dir, ".aidw", "policy.json")
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetWipGate(dir, true); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Pattern != "^my-custom-cmd" {
		t.Fatalf("custom rules not preserved: %+v", cfg.Rules)
	}
	if cfg.WipGate != "disabled" {
		t.Fatalf("WipGate = %q", cfg.WipGate)
	}
}

func TestAddRule_PreservesWipGateField(t *testing.T) {
	dir := t.TempDir()
	if err := SetWipGate(dir, true); err != nil {
		t.Fatal(err)
	}
	if err := AddRule(dir, "npm test", "safe"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WipGate != "disabled" {
		t.Fatalf("AddRule dropped wip_gate: %+v", cfg)
	}
}

// TestLoad_HandWrittenWipGateOnly_BackfillsRulesForEvaluate is the MAJOR 1
// regression test: a hand-written policy.json containing only
// {"wip_gate":"disabled"} (bypassing SetWipGate entirely, exactly the shape
// the README example shows) must still get default rules for Evaluate,
// rather than silently tightening every Bash command to "prompt".
func TestLoad_HandWrittenWipGateOnly_BackfillsRulesForEvaluate(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".aidw"), 0o755); err != nil {
		t.Fatal(err)
	}
	handWritten := `{"wip_gate": "disabled"}`
	if err := os.WriteFile(filepath.Join(dir, ".aidw", "policy.json"), []byte(handWritten), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	verdict := cfg.Evaluate("rm -rf /")
	if verdict.Verdict != "prompt" {
		t.Fatalf("expected prompt verdict from backfilled default rules matching rm, got %+v", verdict)
	}
	// Distinguish from the no-match fallback: a rule actually matched.
	if verdict.Reason == "No matching policy rule found (default to user prompt)" {
		t.Fatalf("expected the rm/sudo default rule to match, but fell through to the no-rules fallback: %+v", verdict)
	}
}

func TestLoad_ExplicitEmptyRules_NotOverridden(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".aidw"), 0o755); err != nil {
		t.Fatal(err)
	}
	explicit := `{"rules": []}`
	if err := os.WriteFile(filepath.Join(dir, ".aidw", "policy.json"), []byte(explicit), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 0 {
		t.Fatalf("expected explicit empty rules to be respected, got %+v", cfg.Rules)
	}
}

func TestSetWipGate_On_ClearsField(t *testing.T) {
	dir := t.TempDir()
	if err := SetWipGate(dir, true); err != nil {
		t.Fatal(err)
	}
	if err := SetWipGate(dir, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WipGate != "" {
		t.Fatalf("expected wip_gate cleared, got %q", cfg.WipGate)
	}
}

// TestSetWipGate_OnWithNoPriorFile_NoOp is a LOW-severity code-review fix:
// re-enabling (disabled=false) on a repo with no pre-existing policy.json
// has nothing to persist (Load() already returns DefaultConfig() when the
// file is absent) — it must not create a stray {"}"} file as clutter.
func TestSetWipGate_OnWithNoPriorFile_NoOp(t *testing.T) {
	dir := t.TempDir()
	if err := SetWipGate(dir, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".aidw", "policy.json")
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected no policy.json to be created, but found one at %s", path)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
