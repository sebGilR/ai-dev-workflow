package slug

import "testing"

func TestSafeSlug(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// No transformation needed — slug == input
		{"main", "main"},
		{"20260312-refactor-effort", "20260312-refactor-effort"},
		{"ok-branch_v1.2", "ok-branch_v1.2"},

		// Slash replaced, suffix appended
		{"feature/my-branch", "feature-my-branch-93a15044"},
		{"fix/issue-42", "fix-issue-42-68b69336"},
		{"feature/add_thing.v2", "feature-add_thing.v2-ccf1a989"},

		// Slash + special chars
		{"feat/scope: add ultrathink", "feat-scope--add-ultrathink-306f233b"},
		{"user/name@host", "user-name-host-c0c6e48c"},

		// Degenerate inputs that collapse to unknown-branch
		{"", "unknown-branch-e3b0c442"},
		{"---", "unknown-branch-cb3f91d5"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := SafeSlug(tc.input)
			if got != tc.want {
				t.Errorf("SafeSlug(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}
