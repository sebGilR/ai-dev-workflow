package install

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const sqliteVecVersion = "v0.1.9"

// InstallSqliteVec downloads and installs the sqlite-vec loadable extension.
func InstallSqliteVec(w io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	libDir := filepath.Join(home, ".claude", "lib")
	
	ext := "dylib"
	if runtime.GOOS == "linux" {
		ext = "so"
	}
	destFile := filepath.Join(libDir, "vec0."+ext)

	if _, err := os.Stat(destFile); err == nil {
		fmt.Fprintf(w, "  sqlite-vec already installed: %s\n", destFile)
		return nil
	}

	fmt.Fprintf(w, "→ Downloading sqlite-vec %s for semantic search...\n", sqliteVecVersion)
	
	filename, err := getSqliteVecFilename()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://github.com/asg017/sqlite-vec/releases/download/%s/%s", sqliteVecVersion, filename)
	
	if err := downloadAndExtract(url, filename, destFile); err != nil {
		return fmt.Errorf("install sqlite-vec: %w", err)
	}

	fmt.Fprintf(w, "  Installed to %s\n", destFile)
	return nil
}

func getSqliteVecFilename() (string, error) {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	ver := strings.TrimPrefix(sqliteVecVersion, "v")

	switch osName {
	case "darwin":
		if arch == "arm64" {
			return fmt.Sprintf("sqlite-vec-%s-loadable-macos-aarch64.tar.gz", ver), nil
		}
		return fmt.Sprintf("sqlite-vec-%s-loadable-macos-x86_64.tar.gz", ver), nil
	case "linux":
		if arch == "arm64" || arch == "aarch64" {
			return fmt.Sprintf("sqlite-vec-%s-loadable-linux-aarch64.tar.gz", ver), nil
		}
		return fmt.Sprintf("sqlite-vec-%s-loadable-linux-x86_64.tar.gz", ver), nil
	default:
		return "", fmt.Errorf("unsupported platform: %s-%s", osName, arch)
	}
}

func downloadAndExtract(url, filename, destFile string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	if err := os.MkdirAll(filepath.Dir(destFile), 0o755); err != nil {
		return err
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Look for vec0.dylib or vec0.so
		name := filepath.Base(header.Name)
		if strings.HasPrefix(name, "vec0.") && !strings.HasSuffix(name, ".tar.gz") {
			f, err := os.OpenFile(destFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err := io.Copy(f, tr); err != nil {
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("vec0 library not found in archive")
}
