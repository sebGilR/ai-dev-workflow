package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Rule represents a single policy rule with a regex pattern and a verdict.
type Rule struct {
	Pattern string `json:"pattern"`
	Verdict string `json:"verdict"` // allow, prompt, audit, deny
	Reason  string `json:"reason,omitempty"`
}

// Config represents the policy configuration stored in .aidw/policy.json.
type Config struct {
	Rules []Rule `json:"rules"`
	// WipGate, when case-insensitively "disabled", opts a repo out of the
	// opt-in PreToolUse workflow-gate hook. It is independent of Rules and
	// is consumed by cmd/aidw/internal/hookgate, not by Evaluate. Set it
	// safely with `aidw policy set-wip-gate <path> off` (see SetWipGate),
	// not by hand-writing this field directly.
	WipGate string `json:"wip_gate,omitempty"`
}

// Verdict returned by the policy engine.
type Verdict struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// Load reads the policy from .aidw/policy.json in the repo root.
func Load(repoPath string) (*Config, error) {
	path := filepath.Join(repoPath, ".aidw", "policy.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse policy.json: %w", err)
	}

	// Backfill default rules only when the file doesn't declare its own
	// "rules" key at all (e.g. a hand-written file containing only
	// {"wip_gate": "disabled"}) — never override an explicit "rules": [].
	// This keeps every dynamic Load() call current with DefaultConfig(),
	// rather than freezing a repo to whatever rules existed the moment
	// some file happened to be written without a rules key.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err == nil {
		if _, hasRules := probe["rules"]; !hasRules {
			cfg.Rules = DefaultConfig().Rules
		}
	}
	return &cfg, nil
}

// SetWipGate persists (or clears) a per-repo opt-out for the workflow-gate
// PreToolUse hook. Operates on the raw JSON map, not the typed Config
// struct, so it never writes a "rules" key that wasn't already present —
// a repo with no prior policy.json gets exactly {"wip_gate":"disabled"}
// and nothing else. Load()'s rules-backfill (above) supplies current
// defaults dynamically on every future read instead of this setter
// freezing a point-in-time snapshot to disk.
func SetWipGate(repoPath string, disabled bool) error {
	dir := filepath.Join(repoPath, ".aidw")
	path := filepath.Join(dir, "policy.json")

	raw := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &raw) // best-effort; corrupt file -> start fresh rather than fail the opt-out
	}

	if disabled {
		raw["wip_gate"] = json.RawMessage(`"disabled"`)
	} else {
		delete(raw, "wip_gate")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(raw, "", "  ")
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// DefaultConfig provides a reasonable set of safe and restricted commands.
func DefaultConfig() *Config {
	return &Config{
		Rules: []Rule{
			{Pattern: `^git (status|branch|diff|log|rev-parse|show|remote|symbolic-ref)`, Verdict: "allow", Reason: "Read-only git commands are safe"},
			{Pattern: `^(go|npm|yarn|pnpm|cargo|pip|uv|uvx|make) (test|build|check|clippy|lint|list|install)`, Verdict: "allow", Reason: "Standard build and test tools are safe"},
			{Pattern: `^(ls|cat|pwd|whoami|echo|head|tail|grep|find|find_empty_space_on_canvas)`, Verdict: "allow", Reason: "Standard discovery and read-only utils are safe"},
			{Pattern: `^aidw (start|status|context|review-bundle|verify|memory list|policy check)`, Verdict: "allow", Reason: "Internal workflow read commands are safe"},
			{Pattern: `^(rm|sudo|curl|wget|gcloud|aws|ssh|scp)`, Verdict: "prompt", Reason: "Potentially destructive or network-active commands require user decision"},
		},
	}
}

// Evaluate checks a command against the rules and returns a verdict.
func (cfg *Config) Evaluate(cmd string) Verdict {
	for _, rule := range cfg.Rules {
		match, _ := regexp.MatchString(rule.Pattern, cmd)
		if match {
			return Verdict{Verdict: rule.Verdict, Reason: rule.Reason}
		}
	}
	return Verdict{Verdict: "prompt", Reason: "No matching policy rule found (default to user prompt)"}
}

// AddRule appends a new allow rule for a specific command to the policy file.
func AddRule(repoPath, cmdStr, reason string) error {
	cfg, err := Load(repoPath)
	if err != nil {
		return err
	}

	pattern := "^" + regexp.QuoteMeta(cmdStr)

	// Check if already exists
	for _, r := range cfg.Rules {
		if r.Pattern == pattern {
			return nil // already whitelisted
		}
	}

	// Prepend specific rules so they take precedence over general ones
	newRule := Rule{
		Pattern: pattern,
		Verdict: "allow",
		Reason:  reason,
	}
	cfg.Rules = append([]Rule{newRule}, cfg.Rules...)

	path := filepath.Join(repoPath, ".aidw", "policy.json")
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Init creates a default policy file in the repo.
func Init(repoPath string) error {
	dir := filepath.Join(repoPath, ".aidw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	path := filepath.Join(dir, "policy.json")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("policy file already exists at %s", path)
	}

	cfg := DefaultConfig()
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
