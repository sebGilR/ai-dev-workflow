package slug

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// SafeSlug converts a git branch name to a filesystem-safe directory component.
// Must produce bit-for-bit identical output to the Python safe_slug() and Bash equivalents.
//
// Rules:
//   - Characters outside [A-Za-z0-9_.-] are replaced with '-'
//   - Leading/trailing '-' are stripped
//   - Empty result → "unknown-branch"
//   - If the slug differs from the original value, append "-<8-hex-char sha256>"
func SafeSlug(value string) string {
	var buf strings.Builder
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
			buf.WriteRune(ch)
		} else {
			buf.WriteRune('-')
		}
	}
	s := strings.Trim(buf.String(), "-")
	if s == "" {
		s = "unknown-branch"
	}
	if s != value {
		h := sha256.Sum256([]byte(value))
		s = s + "-" + fmt.Sprintf("%x", h)[:8]
	}
	return s
}
