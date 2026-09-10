package verify

import "testing"

// TestVersionAtLeast covers the comparison that gates the Claude Code
// frontmatter-support warning. It previously lived inline in
// checkClaudeCodeVersion as a hand-rolled 3-way boolean expression with no
// coverage at all.
func TestVersionAtLeast(t *testing.T) {
	req := [3]int{2, 1, 259}

	cases := []struct {
		name    string
		current [3]int
		want    bool
	}{
		{"exact match", [3]int{2, 1, 259}, true},
		{"one patch below", [3]int{2, 1, 258}, false},
		{"one patch above", [3]int{2, 1, 260}, true},
		{"minor below, patch far above", [3]int{2, 0, 999}, false},
		{"minor above, patch zero", [3]int{2, 2, 0}, true},
		{"major below, everything else above", [3]int{1, 9, 999}, false},
		{"major above, everything else zero", [3]int{3, 0, 0}, true},
		{"all zeroes (unparsed)", [3]int{0, 0, 0}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionAtLeast(tc.current, req); got != tc.want {
				t.Errorf("versionAtLeast(%v, %v) = %v, want %v", tc.current, req, got, tc.want)
			}
		})
	}

	// Sanity: the comparison must be reflexive against the real constant.
	if !versionAtLeast(minFrontmatterEffortVersion, minFrontmatterEffortVersion) {
		t.Error("versionAtLeast must be true for equal versions")
	}
}
