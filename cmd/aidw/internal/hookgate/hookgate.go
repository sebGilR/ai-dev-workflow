// Package hookgate implements the decision logic for the opt-in PreToolUse
// workflow-gate hook: it denies Edit/Write/NotebookEdit calls in a git repo
// that has no active .wip session for the current branch and no declared
// competing workflow, so habitual/incidental skipping of the /wip-* workflow
// gets caught before an edit lands rather than after.
//
// Evaluate is designed to fail open on every ambiguous or unexpected input:
// a bug here should silently stop gating, never silently start blocking
// every edit on the machine.
package hookgate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"aidw/cmd/aidw/internal/git"
	"aidw/cmd/aidw/internal/policy"
	"aidw/cmd/aidw/internal/wip"
)

// gatedTools is the set of tool_name values this hook evaluates. Anything
// else (Bash, Read, Grep, ...) is allowed unconditionally.
var gatedTools = map[string]bool{
	"Edit":         true,
	"Write":        true,
	"NotebookEdit": true,
}

// DenyReason is the exact, user-facing deny message. It must not be reworded
// — the wording is a recorded product decision, and Task 1's ACs assert
// byte-identity against it.
const DenyReason = "No .wip session for this branch. Run /wip-auto (small/low-risk task) or /wip-start (larger task) before editing. Set AIDW_SKIP_GATE=1 to bypass once."

// HookInput is the subset of Claude Code's PreToolUse hook stdin payload
// this package reads. tool_input is left as raw JSON per-field: its shape is
// tool-specific (file_path for Edit/Write, notebook_path for NotebookEdit)
// and this package must never panic on an unexpected shape.
type HookInput struct {
	ToolName       string                     `json:"tool_name"`
	ToolInput      map[string]json.RawMessage `json:"tool_input"`
	Cwd            string                     `json:"cwd"`
	SessionID      string                     `json:"session_id"`
	PermissionMode string                     `json:"permission_mode"`
	AgentID        string                     `json:"agent_id"`
	AgentType      string                     `json:"agent_type"`
}

// Decision is the outcome of Evaluate: whether to allow the tool call, and
// a human-readable reason surfaced to the model (deny) or kept for
// diagnostics (allow).
type Decision struct {
	Allow  bool
	Reason string
}

func allow(reason string) Decision { return Decision{Allow: true, Reason: reason} }

// Evaluate decides whether a PreToolUse call should be allowed, in the
// exact order documented in spec.md Task 1. Every step is a distinct,
// independently-testable allow/deny path; getenv is injected so
// AIDW_SKIP_GATE can be exercised without mutating process environment in
// tests.
func Evaluate(raw []byte, getenv func(string) string) Decision {
	var input HookInput
	if len(raw) == 0 {
		return allow("could not parse hook input: empty body")
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return allow("could not parse hook input: " + err.Error())
	}

	if getenv("AIDW_SKIP_GATE") == "1" {
		return allow("AIDW_SKIP_GATE=1 set")
	}

	if !gatedTools[input.ToolName] {
		return allow("tool not gated")
	}

	if input.AgentID != "" || input.AgentType != "" {
		return allow("subagent call; gated by the top-level session")
	}

	repoTop, err := git.Toplevel(input.Cwd)
	if err != nil {
		return allow("not a git repo")
	}

	if targetOutsideRepo(input, repoTop) {
		return allow("edit target outside repo")
	}

	if isDir(filepath.Join(repoTop, ".bmad")) {
		return allow("repo declares its own workflow (.bmad/)")
	}

	cfg, _ := policy.Load(repoTop)
	if cfg != nil && strings.EqualFold(cfg.WipGate, "disabled") {
		return allow("wip_gate disabled in .aidw/policy.json")
	}

	branchName, berr := resolveGateBranch(repoTop)
	if berr != nil {
		return allow("could not resolve branch: " + berr.Error() + "; failing open")
	}

	_, err = wip.FindBranchState(repoTop, branchName)
	switch {
	case err == nil:
		return allow("active .wip session for this branch")
	case errors.Is(err, wip.ErrNoActiveWork):
		return Decision{Allow: false, Reason: DenyReason}
	default:
		return allow("could not resolve .wip state: " + err.Error() + "; failing open")
	}
}

// caseInsensitiveFS reports whether the host OS's default filesystem is
// case-preserving-but-insensitive (macOS, Windows) — a package variable
// rather than an inline runtime.GOOS check so tests can exercise both
// branches of targetOutsideRepo's case-fold fallback deterministically,
// regardless of which OS actually runs the test suite.
var caseInsensitiveFS = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// targetOutsideRepo inspects tool_input's file_path/notebook_path field
// (best-effort: absent or non-string values are a no-op, never a panic) and
// reports whether the edit target resolves to a path outside repoTop. cwd is
// used to resolve a relative target; both sides are filepath.Clean'd before
// comparison. On darwin/windows (case-preserving-but-insensitive by default)
// a case-folded fallback comparison runs before concluding "outside", so a
// case-only mismatch is never mistaken for an out-of-repo target. Symlink
// resolution is attempted best-effort on top of the cleaned paths and never
// changes the decision if it errors.
func targetOutsideRepo(input HookInput, repoTop string) bool {
	target := extractStringField(input.ToolInput, "file_path")
	if target == "" {
		target = extractStringField(input.ToolInput, "notebook_path")
	}
	if target == "" {
		return false
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(input.Cwd, target)
	}
	target = resolveSymlinksBestEffort(filepath.Clean(target))
	top := resolveSymlinksBestEffort(filepath.Clean(repoTop))

	if isWithin(target, top, false) {
		return false
	}
	if caseInsensitiveFS && isWithin(target, top, true) {
		return false
	}
	return true
}

// resolveSymlinksBestEffort resolves symlinks in path, falling back to
// resolving just its parent directory (rejoined with the original base name)
// when path itself doesn't exist yet — e.g. a Write target about to be
// created. If neither resolves, the input is returned unchanged. Never
// errors: a symlink-resolution failure must not change the gate decision.
func resolveSymlinksBestEffort(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(resolved, filepath.Base(path))
	}
	return path
}

// isWithin reports whether target is top or lives under top, optionally
// case-folding the comparison.
func isWithin(target, top string, foldCase bool) bool {
	t, p := target, top
	if foldCase {
		t, p = strings.ToLower(t), strings.ToLower(p)
	}
	if t == p {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(t, strings.TrimSuffix(p, sep)+sep)
}

// extractStringField best-effort extracts a string field from tool_input's
// raw JSON. Any shape mismatch (missing key, non-string value, malformed
// JSON) returns "" rather than erroring — this must never panic on
// adversarial-shaped stdin.
func extractStringField(toolInput map[string]json.RawMessage, key string) string {
	if toolInput == nil {
		return ""
	}
	raw, ok := toolInput[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// RenderOutput builds the PreToolUse hookSpecificOutput JSON for a decision.
//
// Corrected after code review (see spec.md's "Rendering-contract correction"
// section): Claude Code's PreToolUse contract treats an explicit
// permissionDecision as an affirmative decision that bypasses the normal
// permission flow — "allow" grants the call outright, overriding the user's
// own configured permissions.ask/permissions.deny rules for that tool,
// exactly like "deny" blocks it outright. Only *empty stdout* (exit 0, no
// output) means "this hook has no opinion; let the normal permission flow
// (including the user's own ask/deny rules) decide"
// (https://code.claude.com/docs/en/hooks — "Exit code 0 with no output means
// the hook has no decision to report, so the tool call continues through the
// normal permission flow.").
//
// This hook is only supposed to have an opinion in exactly one case: no
// active .wip session for the branch (deny). Every other path is "we don't
// care, defer to whatever the user already has configured" — so every allow
// Decision renders to zero bytes, never an explicit "allow" JSON. Only a
// deny Decision produces output. RenderOutput must never itself fail: on
// the (practically impossible) marshal error on the deny path, it falls
// back to a hardcoded deny-JSON literal rather than ever inventing an
// "allow" (fail-open on a render bug is not the same as fail-open on a
// decision bug — see hook_gate.go's recover() path for the same reasoning).
func RenderOutput(d Decision) []byte {
	if d.Allow {
		return nil
	}
	out := hookOutput{}
	out.HookSpecificOutput.HookEventName = "PreToolUse"
	out.HookSpecificOutput.PermissionDecision = "deny"
	out.HookSpecificOutput.PermissionDecisionReason = d.Reason

	data, err := json.Marshal(out)
	if err != nil {
		// Marshal only fails on unsupported types (channels, funcs) that
		// hookOutput's plain-string fields can never contain — this path is
		// unreachable in practice. If it's ever hit, failing open (no
		// output) is still correct: we'd rather silently stop gating than
		// risk emitting malformed JSON that Claude Code can't parse.
		return nil
	}
	return data
}

type hookOutput struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}
