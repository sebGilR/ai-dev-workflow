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
	return &cfg, nil
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
