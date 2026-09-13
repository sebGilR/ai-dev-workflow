package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	embedfs "aidw"
)

// blanketRule is the legacy `permissions.allow` entry that shadowed the
// adversarial-review `ask` gate for every already-installed user.
const blanketRule = "Bash(~/.claude/ai-dev-workflow/bin/aidw *)"

func readAllow(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var parsed struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse settings: %v\n%s", err, data)
	}
	return parsed.Permissions.Allow
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestMergeSettings_RetractsBlanketAidwAllowRule covers the upgrade path:
// mergeLists is union-only, so without an explicit retraction step an existing
// settings.json keeps the blanket `aidw *` allow rule forever and the
// adversarial-review permission gate stays shadowed.
func TestMergeSettings_RetractsBlanketAidwAllowRule(t *testing.T) {
	tmpl := []byte(`{"permissions":{"allow":[
		"Bash(~/.claude/ai-dev-workflow/bin/aidw status*)",
		"Bash(~/.claude/ai-dev-workflow/bin/aidw next*)"
	]}}`)

	t.Run("blanket rule is removed and scoped entries land", func(t *testing.T) {
		dir := t.TempDir()
		settings := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(settings, []byte(`{"permissions":{"allow":[
			"Bash(git status)",
			"`+blanketRule+`"
		]}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := MergeSettings(settings, tmpl); err != nil {
			t.Fatalf("MergeSettings: %v", err)
		}
		allow := readAllow(t, settings)
		if contains(allow, blanketRule) {
			t.Errorf("blanket rule survived the merge: %v", allow)
		}
		for _, want := range []string{
			"Bash(git status)",
			"Bash(~/.claude/ai-dev-workflow/bin/aidw status*)",
			"Bash(~/.claude/ai-dev-workflow/bin/aidw next*)",
		} {
			if !contains(allow, want) {
				t.Errorf("missing %q in %v", want, allow)
			}
		}
	})

	t.Run("settings without the blanket rule are unaffected", func(t *testing.T) {
		dir := t.TempDir()
		settings := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(settings, []byte(`{"permissions":{"allow":[
			"Bash(git status)",
			"Bash(rg *)"
		]}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := MergeSettings(settings, tmpl); err != nil {
			t.Fatalf("MergeSettings: %v", err)
		}
		allow := readAllow(t, settings)
		for _, want := range []string{"Bash(git status)", "Bash(rg *)"} {
			if !contains(allow, want) {
				t.Errorf("user entry %q was dropped: %v", want, allow)
			}
		}
		if contains(allow, blanketRule) {
			t.Errorf("blanket rule appeared out of nowhere: %v", allow)
		}
	})

	t.Run("allow list holding only the blanket rule stays a JSON array", func(t *testing.T) {
		dir := t.TempDir()
		settings := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(settings, []byte(`{"permissions":{"allow":["`+blanketRule+`"]}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		// Empty template so nothing is added back.
		if err := MergeSettings(settings, []byte(`{}`)); err != nil {
			t.Fatalf("MergeSettings: %v", err)
		}
		data, _ := os.ReadFile(settings)
		if strings.Contains(string(data), `"allow": null`) {
			t.Errorf("emptied allow list marshalled as null, not []:\n%s", data)
		}
		if got := readAllow(t, settings); len(got) != 0 {
			t.Errorf("expected empty allow list, got %v", got)
		}
	})

	t.Run("merging twice is stable", func(t *testing.T) {
		dir := t.TempDir()
		settings := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(settings, []byte(`{"permissions":{"allow":["`+blanketRule+`"]}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := MergeSettings(settings, tmpl); err != nil {
			t.Fatalf("MergeSettings (1): %v", err)
		}
		first, _ := os.ReadFile(settings)
		if err := MergeSettings(settings, tmpl); err != nil {
			t.Fatalf("MergeSettings (2): %v", err)
		}
		second, _ := os.ReadFile(settings)
		if string(first) != string(second) {
			t.Errorf("merge is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", first, second)
		}
	})

	t.Run("malformed permissions blocks are left alone", func(t *testing.T) {
		for name, body := range map[string]string{
			"permissions not an object": `{"permissions":"nope"}`,
			"allow not a list":          `{"permissions":{"allow":"nope"}}`,
			"no permissions key":        `{"model":"opus"}`,
		} {
			dir := t.TempDir()
			settings := filepath.Join(dir, "settings.json")
			if err := os.WriteFile(settings, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := MergeSettings(settings, []byte(`{}`)); err != nil {
				t.Errorf("%s: MergeSettings: %v", name, err)
			}
		}
	})
}

func readPermList(t *testing.T, path, listName string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse settings: %v\n%s", err, data)
	}
	perms, _ := parsed["permissions"].(map[string]any)
	rawList, _ := perms[listName].([]any)
	out := make([]string, 0, len(rawList))
	for _, item := range rawList {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// TestMergeSettings_RetractsStaleDenyAndAskRules covers the upgrade path for
// the curl/wget deny rules and the commit/rebase/install ask rules a prior
// audit fixed by hand in an already-installed settings.json:
// mergeLists is union-only, so without an explicit retraction step the next
// `aidw upgrade` would silently re-add these stale rules from the template,
// recreating the overnight-stall pattern the audit measured.
func TestMergeSettings_RetractsStaleDenyAndAskRules(t *testing.T) {
	tmpl := []byte(`{"permissions":{
		"deny":["Bash(curl *)","Bash(wget *)"],
		"ask":["Bash(git commit *)","Bash(git rebase *)","Bash(npm install *)","Bash(pnpm install *)"]
	}}`)

	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	// Simulate an already-installed user whose settings.json still has the
	// stale rules from an older template (the exact scenario `aidw upgrade`
	// must fix, not just avoid re-adding).
	if err := os.WriteFile(settings, []byte(`{"permissions":{
		"deny":["Read(./.env)","Bash(curl *)","Bash(wget *)"],
		"ask":["Bash(git push*)","Bash(git commit *)","Bash(git rebase *)","Bash(npm install *)","Bash(pnpm install *)"]
	}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MergeSettings(settings, tmpl); err != nil {
		t.Fatalf("MergeSettings: %v", err)
	}

	deny := readPermList(t, settings, "deny")
	for _, stale := range []string{"Bash(curl *)", "Bash(wget *)"} {
		if contains(deny, stale) {
			t.Errorf("stale deny rule %q survived the merge: %v", stale, deny)
		}
	}
	if !contains(deny, "Read(./.env)") {
		t.Errorf("unrelated deny entry was dropped: %v", deny)
	}

	ask := readPermList(t, settings, "ask")
	for _, stale := range []string{
		"Bash(git commit *)", "Bash(git rebase *)",
		"Bash(npm install *)", "Bash(pnpm install *)",
	} {
		if contains(ask, stale) {
			t.Errorf("stale ask rule %q survived the merge: %v", stale, ask)
		}
	}
	if !contains(ask, "Bash(git push*)") {
		t.Errorf("unrelated ask entry was dropped: %v", ask)
	}
}

// dangerousInvocations are commands that must never be covered by a
// `permissions.allow` entry in the shipped template.
var dangerousInvocations = []string{
	"aidw adversarial-review .",
	"aidw gemini-review .",
	"aidw cleanup-branch . --purge",
	"aidw clear-wip . --purge",
	"aidw clear-others . --purge",
	"aidw cleanup-branch --purge .",
	"aidw clear-wip --purge .",
	"aidw clear-others --purge .",
}

// invocationPrefixes are the ways `aidw` gets spelled on a command line.
var invocationPrefixes = []string{
	"aidw",
	"~/.claude/ai-dev-workflow/bin/aidw",
	"/opt/homebrew/bin/aidw",
	"/usr/local/bin/aidw",
}

// allowPatternCovers reports whether a `permissions.allow` entry would
// auto-approve cmd. Claude Code Bash rules are either an exact string match or
// a prefix match when the pattern ends in `*`.
func allowPatternCovers(entry, cmd string) bool {
	if !strings.HasPrefix(entry, "Bash(") || !strings.HasSuffix(entry, ")") {
		return false
	}
	pat := entry[len("Bash(") : len(entry)-1]
	if strings.HasSuffix(pat, "*") {
		return strings.HasPrefix(cmd, strings.TrimSuffix(pat, "*"))
	}
	return pat == cmd
}

// TestSettingsTemplateNeverAllowsGatedCommands is a regression guard: it fails
// CI if anyone re-introduces a blanket `aidw *` allow rule (or any other
// pattern broad enough to swallow the adversarial-review gate or the
// byte-deleting `--purge` forms) into the shipped template.
func TestSettingsTemplateNeverAllowsGatedCommands(t *testing.T) {
	data, err := embedfs.FS.ReadFile("templates/global/settings.template.json")
	if err != nil {
		t.Fatalf("read embedded template: %v", err)
	}
	var parsed struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Ask   []string `json:"ask"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("template is not valid JSON: %v", err)
	}
	if len(parsed.Permissions.Allow) == 0 {
		t.Fatal("template has no permissions.allow entries — matcher would vacuously pass")
	}

	for _, prefix := range invocationPrefixes {
		for _, danger := range dangerousInvocations {
			cmd := prefix + strings.TrimPrefix(danger, "aidw")
			for _, entry := range parsed.Permissions.Allow {
				if allowPatternCovers(entry, cmd) {
					t.Errorf("allow entry %q auto-approves gated command %q", entry, cmd)
				}
			}
		}
	}

	// Sanity check on the matcher itself: a blanket rule must be detected.
	if !allowPatternCovers(blanketRule, "~/.claude/ai-dev-workflow/bin/aidw adversarial-review .") {
		t.Fatal("allowPatternCovers failed to flag the known-bad blanket rule")
	}

	// And the routine commands the allow list exists for must still be covered,
	// on every invocation path prefix.
	for _, prefix := range invocationPrefixes[1:] {
		cmd := prefix + " status ."
		covered := false
		for _, entry := range parsed.Permissions.Allow {
			if allowPatternCovers(entry, cmd) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("routine command %q is not covered by any allow entry", cmd)
		}
	}
}
