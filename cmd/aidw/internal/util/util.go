package util

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// AtomicWrite writes data to path via a temp file + rename to avoid partial writes.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("atomic write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("atomic write rename: %w", err)
	}
	return nil
}

// ReadJSON reads JSON from path into v.
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// WriteJSON marshals v as indented JSON and atomically writes it to path.
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return AtomicWrite(path, data, 0o644)
}

// NowISO returns the current time as an RFC 3339 string with local timezone offset.
func NowISO() string {
	return time.Now().Format(time.RFC3339)
}

// ParseIntEnv reads an environment variable as an int, returning defaultVal if unset or unparseable.
func ParseIntEnv(key string, defaultVal int) int {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal
	}
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return defaultVal
	}
	return v
}

// TruncateDiff truncates text to at most limit bytes, preserving valid UTF-8.
// Returns the (possibly truncated) string and whether truncation occurred.
func TruncateDiff(text string, limit int) (string, bool) {
	b := []byte(text)
	if len(b) <= limit {
		return text, false
	}
	b = b[:limit]
	// trim trailing UTF-8 continuation bytes (10xxxxxx)
	for len(b) > 0 && b[len(b)-1]&0xC0 == 0x80 {
		b = b[:len(b)-1]
	}
	return string(b), true
}

// ClampInt returns v clamped to [min, max].
func ClampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
