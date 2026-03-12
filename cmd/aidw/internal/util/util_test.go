package util

import (
	"testing"
	"unicode/utf8"
)

func TestTruncateDiff(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		limit     int
		wantTrunc bool
	}{
		{"under limit", "hello", 10, false},
		{"exact limit", "hello", 5, false},
		{"over limit ascii", "hello world", 5, true},
		{"valid utf8 preserved", "héllo", 4, true}, // é is 2 bytes; truncate preserves valid UTF-8
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated := TruncateDiff(tc.input, tc.limit)
			if truncated != tc.wantTrunc {
				t.Errorf("truncated=%v, want %v", truncated, tc.wantTrunc)
			}
			if len(got) > tc.limit {
				t.Errorf("len(got)=%d > limit=%d", len(got), tc.limit)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result is not valid UTF-8: %q", got)
			}
		})
	}
}
