package install

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// mirrorsRepoRoot locates the checkout root by walking up from this test
// file's directory to the first ancestor containing go.mod, so the drift
// test works regardless of the directory `go test` was invoked from.
func mirrorsRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (no go.mod found)")
		}
		dir = parent
	}
}

// TestMirrorSkillsDriftFromClaudeSkills is a byte-equality (never text-diff
// — see the rtk false-identical hazard) check that .github/skills/ is
// exactly `make mirrors`'s output for claude/skills/: every file present in
// one side exists with identical bytes on the other, in both directions
// (so a deleted/renamed skill can't leave an orphaned mirror copy behind).
func TestMirrorSkillsDriftFromClaudeSkills(t *testing.T) {
	root := mirrorsRepoRoot(t)
	src := filepath.Join(root, "claude", "skills")
	dest := filepath.Join(root, ".github", "skills")

	assertTreesByteEqual(t, src, dest, "run `make mirrors` after editing claude/skills/**")
}

// TestMirrorAgentsMatchGeneratedOutput regenerates .github/agents/ into a
// scratch directory from the current claude/agents/ source and asserts it
// is byte-identical to the committed .github/agents/ — i.e. `make mirrors`
// has already been run and its output committed.
func TestMirrorAgentsMatchGeneratedOutput(t *testing.T) {
	root := mirrorsRepoRoot(t)
	src := filepath.Join(root, "claude", "agents")
	committed := filepath.Join(root, ".github", "agents")

	scratch := t.TempDir()
	srcFS := os.DirFS(src)
	if err := GenerateGithubAgents(srcFS, scratch); err != nil {
		t.Fatalf("GenerateGithubAgents: %v", err)
	}

	assertTreesByteEqual(t, scratch, committed, "run `make mirrors` after editing claude/agents/**")
}

// assertTreesByteEqual fails if any file differs (by sha bytes, not text
// diff) between a and b, or if either side has a file the other lacks.
func assertTreesByteEqual(t *testing.T, a, b, hint string) {
	t.Helper()

	filesUnder := func(root string) map[string][]byte {
		out := map[string][]byte{}
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if info.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			out[filepath.ToSlash(rel)] = data
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
		return out
	}

	filesA := filesUnder(a)
	filesB := filesUnder(b)

	for rel, dataA := range filesA {
		dataB, ok := filesB[rel]
		if !ok {
			t.Errorf("%s exists under %s but not %s — %s", rel, a, b, hint)
			continue
		}
		if !bytes.Equal(dataA, dataB) {
			t.Errorf("%s differs between %s and %s — %s", rel, a, b, hint)
		}
	}
	for rel := range filesB {
		if _, ok := filesA[rel]; !ok {
			t.Errorf("%s exists under %s but not %s (orphan) — %s", rel, b, a, hint)
		}
	}
}
