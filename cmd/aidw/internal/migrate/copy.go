package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"aidw/cmd/aidw/internal/util"
)

// sha256File returns the lowercase hex sha256 digest of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashTree computes sha256 digests (lowercase hex) for every regular file
// under root, keyed by its path relative to root, filepath.ToSlash-
// normalized so the map's keys are stable cross-platform (§2a Q3).
func HashTree(root string) (map[string]string, error) {
	hashes := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		digest, err := sha256File(path)
		if err != nil {
			return err
		}
		hashes[filepath.ToSlash(rel)] = digest
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hashes, nil
}

// CopyAttachments copies every file under sourceWipDir (recursively,
// preserving relative paths — so an archive/<timestamp>/... subdirectory
// from Phase 1's archive-cleanup is just more files here, nothing
// schema-aware is done with them) into destDir, via
// util.CopyFS(os.DirFS(sourceWipDir), destDir) — reusing the existing
// recursive-copy helper rather than hand-rolling a walk (hard rule 6). This
// is ordinary file I/O, not a work-record write — see hard rule 5's
// exemption; it must not and does not route through work.Save/UpdateRecord.
//
// Verification happens before success is recorded: sha256 digests are
// computed from the SOURCE before the copy runs, then recomputed from the
// DESTINATION after the copy completes and compared as a set (same files,
// same digests) — a copy that silently truncated or corrupted data is
// caught here, not assumed from a successful io.Copy return. On success,
// returns the source-side digest map for Provenance.SourceHashes.
func CopyAttachments(sourceWipDir, destDir string) (map[string]string, error) {
	srcHashes, err := HashTree(sourceWipDir)
	if err != nil {
		return nil, fmt.Errorf("hash source %s: %w", sourceWipDir, err)
	}

	if err := util.CopyFS(os.DirFS(sourceWipDir), destDir); err != nil {
		return nil, fmt.Errorf("copy attachments from %s to %s: %w", sourceWipDir, destDir, err)
	}

	destHashes, err := HashTree(destDir)
	if err != nil {
		return nil, fmt.Errorf("hash destination %s: %w", destDir, err)
	}

	if len(destHashes) != len(srcHashes) {
		return nil, fmt.Errorf("verify copy %s -> %s: expected %d files, found %d at destination", sourceWipDir, destDir, len(srcHashes), len(destHashes))
	}
	for rel, srcDigest := range srcHashes {
		dstDigest, ok := destHashes[rel]
		if !ok {
			return nil, fmt.Errorf("verify copy %s -> %s: %s missing at destination", sourceWipDir, destDir, rel)
		}
		if dstDigest != srcDigest {
			return nil, fmt.Errorf("verify copy %s -> %s: %s digest mismatch (copy corrupted)", sourceWipDir, destDir, rel)
		}
	}

	return srcHashes, nil
}
