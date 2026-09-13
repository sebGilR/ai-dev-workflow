package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"aidw/cmd/aidw/internal/util"
)

// MergeSettings deep-merges the JSON template bytes into the
// settings file at settingsPath.
//
// Merge rules (same as the Python implementation):
//   - Objects: recurse
//   - Arrays: deduplicate by JSON-serialised key (union)
//   - Scalars: user value wins — incoming template does not overwrite
//
// If settingsPath contains invalid JSON it is backed up and a fresh merge
// (template only) is written.
func MergeSettings(settingsPath string, templateData []byte) error {
	var tmpl map[string]any
	if err := json.Unmarshal(templateData, &tmpl); err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	var existing map[string]any
	if data, err := os.ReadFile(settingsPath); err == nil {
		if jerr := json.Unmarshal(data, &existing); jerr != nil {
			backup := backupPath(settingsPath)
			if rerr := os.Rename(settingsPath, backup); rerr != nil {
				return fmt.Errorf("backup invalid settings: %w", rerr)
			}
			fmt.Fprintf(os.Stderr,
				"WARNING: %s contains invalid JSON (%v). Backed up to %s and starting fresh.\n",
				settingsPath, jerr, backup)
			existing = map[string]any{}
		}
	} else if os.IsNotExist(err) {
		existing = map[string]any{}
	} else {
		return fmt.Errorf("read settings: %w", err)
	}

	merged := mergeDict(existing, tmpl)
	retractStaleAllowRules(merged)
	retractStaleRules(merged, "deny")
	retractStaleRules(merged, "ask")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	out, _ := json.MarshalIndent(merged, "", "  ")
	return util.AtomicWrite(settingsPath, append(out, '\n'), 0o644)
}

// retractedAllowPatterns are `permissions.allow` entries that earlier versions
// of settings.template.json shipped and that must NOT survive an upgrade.
//
// mergeLists is union-only: it adds new entries but never removes old ones, so
// without an explicit retraction step every already-installed user keeps the
// blanket `aidw *` allow rule forever. That rule prefix-matches
// `aidw adversarial-review` / `aidw gemini-review` and therefore shadows the
// `permissions.ask` gate those commands rely on.
//
// Keep this list minimal and exact-match only. It is a migration for specific
// known-bad legacy entries, not a general pattern-removal engine.
var retractedAllowPatterns = []string{
	"Bash(~/.claude/ai-dev-workflow/bin/aidw *)",
}

// retractStaleAllowRules removes retractedAllowPatterns from
// merged["permissions"]["allow"] in place. It runs after the merge so the
// written file never contains a retracted pattern regardless of whether it
// came from the user's existing settings or from the template.
//
// Every shape mismatch (missing keys, wrong types, non-string entries) is a
// no-op: this must never corrupt a settings file it does not understand.
func retractStaleAllowRules(merged map[string]any) {
	perms, ok := merged["permissions"].(map[string]any)
	if !ok {
		return
	}
	allow, ok := perms["allow"].([]any)
	if !ok {
		return
	}
	retract := make(map[string]bool, len(retractedAllowPatterns))
	for _, p := range retractedAllowPatterns {
		retract[p] = true
	}
	// Non-nil empty start: a nil slice marshals to `null`, not `[]`, which
	// would be an invalid permissions block if every entry got retracted.
	kept := []any{}
	for _, item := range allow {
		if s, isStr := item.(string); isStr && retract[s] {
			continue
		}
		kept = append(kept, item)
	}
	perms["allow"] = kept
}

// retractedDenyPatterns are `permissions.deny` entries that earlier versions
// of settings.template.json shipped and that must NOT survive an upgrade.
//
// `Bash(curl *)` and `Bash(wget *)` denied every curl/wget invocation,
// including localhost health checks — deny beats allow, so no allow rule
// could ever unblock them while these remained. They are replaced by
// scoped localhost allow rules in the template.
var retractedDenyPatterns = []string{
	"Bash(curl *)",
	"Bash(wget *)",
}

// retractedAskPatterns are `permissions.ask` entries that earlier versions of
// settings.template.json shipped and that must NOT survive an upgrade.
//
// These `ask` rules fired on routine background-agent operations (commits,
// rebases, dependency installs) and stalled unattended sessions waiting on a
// prompt nobody could answer.
var retractedAskPatterns = []string{
	"Bash(git commit *)",
	"Bash(git rebase *)",
	"Bash(npm install *)",
	"Bash(pnpm install *)",
}

// retractStaleRules removes the retracted patterns for listName ("deny" or
// "ask") from merged["permissions"][listName] in place. It runs after the
// merge so the written file never contains a retracted pattern regardless of
// whether it came from the user's existing settings or from the template —
// mergeLists is union-only and would otherwise re-add these patterns forever
// for every already-installed user, silently undoing the template fix.
//
// Every shape mismatch (missing keys, wrong types, non-string entries) is a
// no-op: this must never corrupt a settings file it does not understand.
func retractStaleRules(merged map[string]any, listName string) {
	var patterns []string
	switch listName {
	case "deny":
		patterns = retractedDenyPatterns
	case "ask":
		patterns = retractedAskPatterns
	default:
		return
	}

	perms, ok := merged["permissions"].(map[string]any)
	if !ok {
		return
	}
	list, ok := perms[listName].([]any)
	if !ok {
		return
	}
	retract := make(map[string]bool, len(patterns))
	for _, p := range patterns {
		retract[p] = true
	}
	// Non-nil empty start: a nil slice marshals to `null`, not `[]`, which
	// would be an invalid permissions block if every entry got retracted.
	kept := []any{}
	for _, item := range list {
		if s, isStr := item.(string); isStr && retract[s] {
			continue
		}
		kept = append(kept, item)
	}
	perms[listName] = kept
}

func mergeDict(existing, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(existing))
	for k, v := range existing {
		out[k] = v
	}
	for k, v := range incoming {
		if cur, ok := out[k]; ok {
			curMap, curIsMap := cur.(map[string]any)
			inMap, inIsMap := v.(map[string]any)
			curSlice, curIsSlice := cur.([]any)
			inSlice, inIsSlice := v.([]any)
			switch {
			case curIsMap && inIsMap:
				out[k] = mergeDict(curMap, inMap)
			case curIsSlice && inIsSlice:
				out[k] = mergeLists(curSlice, inSlice)
			// scalar: user value wins — do not overwrite
			}
		} else {
			out[k] = v
		}
	}
	return out
}

func mergeLists(existing, incoming []any) []any {
	seen := map[string]bool{}
	var result []any
	for _, item := range append(existing, incoming...) {
		key, _ := json.Marshal(item)
		k := string(key)
		if !seen[k] {
			seen[k] = true
			result = append(result, item)
		}
	}
	return result
}

func backupPath(p string) string {
	base := p[:len(p)-len(filepath.Ext(p))]
	bak := base + ".json.bak"
	if _, err := os.Stat(bak); err != nil {
		return bak
	}
	ts := time.Now().UTC().Format("20060102150405")
	return base + ".json." + ts + ".bak"
}
