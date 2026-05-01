package util

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CopyFS recursively copies srcFS to destDir.
func CopyFS(srcFS fs.FS, destDir string) error {
	return fs.WalkDir(srcFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		destPath := filepath.Join(destDir, path)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		srcFile, err := srcFS.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, srcFile)
		return err
	})
}
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

// SafeLink creates a symlink from src to dest. 
// If dest already points to src, it does nothing.
// If dest exists and is NOT a symlink, it returns an error.
// If dest is a stale symlink, it updates it.
func SafeLink(src, dest string) error {
	os.MkdirAll(filepath.Dir(dest), 0o755)

	info, err := os.Lstat(dest)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			current, err := os.Readlink(dest)
			if err == nil && current == src {
				return nil
			}
			// Stale or different symlink - remove it to update
			os.Remove(dest)
		} else {
			return fmt.Errorf("%s exists and is not a managed symlink", dest)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return os.Symlink(src, dest)
}
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

// ChunkDiff splits text into a slice of strings, each at most chunkSize bytes,
// preserving valid UTF-8 boundaries.
func ChunkDiff(text string, chunkSize int) []string {
	if chunkSize <= 0 {
		return []string{text}
	}
	b := []byte(text)
	var chunks []string
	for len(b) > 0 {
		if len(b) <= chunkSize {
			chunks = append(chunks, string(b))
			break
		}
		
		limit := chunkSize
		for limit > 0 && b[limit-1]&0xC0 == 0x80 {
			limit--
		}
		if limit == 0 {
			// A single multibyte character exceeds the chunk size. 
			// Advance by 1 byte to prevent infinite loop, though this breaks UTF-8.
			limit = 1
		}
		chunks = append(chunks, string(b[:limit]))
		b = b[limit:]
	}
	return chunks
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
