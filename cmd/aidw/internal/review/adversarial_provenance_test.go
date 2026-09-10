package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aidw/cmd/aidw/internal/util"
	"aidw/cmd/aidw/internal/wip"
)

// --- Cluster D regression tests (spec E3 items 6-7) ---

func writeAdvFile(t *testing.T, wipDir, fingerprint string) {
	t.Helper()
	header := "<!-- aidw:adversarial provider=gemini model=test-model " +
		"generated_at=2026-01-01T00:00:00Z diff_sha256=" + fingerprint + " -->\n\n"
	if err := os.WriteFile(filepath.Join(wipDir, "adversarial-review.md"),
		[]byte(header+"Critical bug found.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeBundleWithFingerprint(t *testing.T, wipDir, fingerprint string) {
	t.Helper()
	bundle := &BundleResult{
		Repo:            "test-repo",
		Branch:          "test-branch",
		GeneratedAt:     "2026-01-01T00:00:00Z",
		ChangedFiles:    []string{"foo.go"},
		DiffFingerprint: fingerprint,
	}
	if err := util.WriteJSON(filepath.Join(wipDir, "review-bundle.json"), bundle); err != nil {
		t.Fatal(err)
	}
}

// seedWip runs SynthesizeReview once to create the branch WIP dir and returns it.
func seedWip(t *testing.T, dir string) string {
	t.Helper()
	result, err := SynthesizeReview(dir)
	if err != nil {
		t.Fatalf("SynthesizeReview (seed): %v", err)
	}
	return filepath.Dir(result.ReviewPath)
}

// TestAdversarialStaleImport covers AC-D3: an adversarial-review.md whose
// provenance fingerprint does not match the current review bundle is imported
// under the SAME `## Adversarial Review` heading but labelled stale; a matching
// fingerprint imports with an ordinary provenance line.
func TestAdversarialStaleImport(t *testing.T) {
	t.Run("mismatch labels stale", func(t *testing.T) {
		dir := t.TempDir()
		initGitRepo(t, dir)
		wipDir := seedWip(t, dir)

		writeAdvFile(t, wipDir, "sha256:oldoldold")
		// The bundle's stored fingerprint is deliberately a third value: the
		// comparison must use the fingerprint recomputed from git, not this one.
		writeBundleWithFingerprint(t, wipDir, "sha256:bundlebundle")
		current := diffFingerprint(dir)

		result, err := SynthesizeReview(dir)
		if err != nil {
			t.Fatalf("SynthesizeReview: %v", err)
		}
		data, _ := os.ReadFile(result.ReviewPath)
		content := string(data)

		if !strings.Contains(content, "## Adversarial Review") {
			t.Error("stale import must keep the '## Adversarial Review' heading")
		}
		if !reAdversarial.MatchString(content) {
			t.Error("reAdversarial must still match the heading of a stale import")
		}
		if !strings.Contains(content, "_Stale:") {
			t.Errorf("expected a stale provenance line; got:\n%s", content)
		}
		if !strings.Contains(content, "sha256:oldoldold") || !strings.Contains(content, current) {
			t.Errorf("stale line should name both the recorded and the current fingerprint; got:\n%s", content)
		}
		if strings.Contains(content, "sha256:bundlebundle") {
			t.Error("staleness must be judged against git, not review-bundle.json's stored fingerprint")
		}
		if !strings.Contains(content, "Critical bug found.") {
			t.Error("adversarial body should still be imported")
		}
		if strings.Contains(content, "aidw:adversarial") {
			t.Error("the raw provenance HTML comment should not be copied into review.md")
		}
	})

	t.Run("match imports normally", func(t *testing.T) {
		dir := t.TempDir()
		initGitRepo(t, dir)
		wipDir := seedWip(t, dir)

		writeBundleWithFingerprint(t, wipDir, "sha256:bundlebundle")
		writeAdvFile(t, wipDir, diffFingerprint(dir))

		result, err := SynthesizeReview(dir)
		if err != nil {
			t.Fatalf("SynthesizeReview: %v", err)
		}
		data, _ := os.ReadFile(result.ReviewPath)
		content := string(data)

		if !strings.Contains(content, "## Adversarial Review") {
			t.Error("missing '## Adversarial Review' heading")
		}
		if strings.Contains(content, "_Stale:") {
			t.Errorf("matching fingerprint must not be labelled stale; got:\n%s", content)
		}
		if !strings.Contains(content, "fingerprint matches the current") {
			t.Errorf("expected a matching provenance line; got:\n%s", content)
		}
		if !strings.Contains(content, "Critical bug found.") {
			t.Error("adversarial body should be imported")
		}
	})

	t.Run("missing header is flagged", func(t *testing.T) {
		dir := t.TempDir()
		initGitRepo(t, dir)
		wipDir := seedWip(t, dir)

		if err := os.WriteFile(filepath.Join(wipDir, "adversarial-review.md"),
			[]byte("Legacy findings, no header.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeBundleWithFingerprint(t, wipDir, "sha256:whatever")

		result, err := SynthesizeReview(dir)
		if err != nil {
			t.Fatalf("SynthesizeReview: %v", err)
		}
		data, _ := os.ReadFile(result.ReviewPath)
		content := string(data)
		if !strings.Contains(content, "Provenance unknown") {
			t.Errorf("headerless adversarial file should be flagged; got:\n%s", content)
		}
		if !strings.Contains(content, "Legacy findings, no header.") {
			t.Error("headerless body should still be imported")
		}
	})
}

func TestParseAdversarialHeader(t *testing.T) {
	meta, body := parseAdversarialHeader(
		"<!-- aidw:adversarial provider=gemini model=m1 generated_at=2026-01-01T00:00:00Z diff_sha256=sha256:abc -->\n\nfindings\n")
	if !meta.Present {
		t.Fatal("expected header to be detected")
	}
	if meta.Provider != "gemini" || meta.Model != "m1" ||
		meta.GeneratedAt != "2026-01-01T00:00:00Z" || meta.DiffSHA256 != "sha256:abc" {
		t.Errorf("unexpected meta: %+v", meta)
	}
	if strings.TrimSpace(body) != "findings" {
		t.Errorf("body = %q, want %q", strings.TrimSpace(body), "findings")
	}

	meta2, body2 := parseAdversarialHeader("no header here")
	if meta2.Present {
		t.Error("expected no header")
	}
	if body2 != "no header here" {
		t.Errorf("body2 = %q", body2)
	}
}

// TestAdversarialWritesProvenanceHeader asserts the generation half of D6: the
// bundle's own DiffFingerprint is stamped verbatim into the file header.
// It also demonstrates AC-D2 at the library level — no enablement env var is
// set, and the (stubbed) provider still runs.
func TestAdversarialWritesProvenanceHeader(t *testing.T) {
	fake := t.TempDir()
	script := filepath.Join(fake, "gemini")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\necho 'FAKE FINDINGS'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Prepend (not replace) so git remains reachable for the repo fixture.
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AIDW_ADVERSARIAL_REVIEW", "0")

	dir := t.TempDir()
	initGitRepo(t, dir)
	state, err := wip.EnsureBranchState(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	bundle := &BundleResult{
		Repo:            "test-repo",
		Branch:          "test-branch",
		GeneratedAt:     "2026-01-01T00:00:00Z",
		BranchDiff:      "diff --git a/foo.go b/foo.go\n+added line",
		ChangedFiles:    []string{"foo.go"},
		DiffFingerprint: "sha256:deadbeef",
	}
	if err := util.WriteJSON(filepath.Join(state.WipDir, "review-bundle.json"), bundle); err != nil {
		t.Fatal(err)
	}

	result, err := AdversarialReview(dir, "gemini", "test-model", 30)
	if err != nil {
		t.Fatalf("AdversarialReview: %v", err)
	}
	if result.Status != "ok" {
		t.Fatalf("status = %q, want ok", result.Status)
	}

	data, err := os.ReadFile(filepath.Join(state.WipDir, "adversarial-review.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "diff_sha256=sha256:deadbeef") {
		t.Errorf("header must carry the bundle fingerprint verbatim; got:\n%s", content)
	}
	if !strings.Contains(content, "provider=gemini") || !strings.Contains(content, "model=test-model") {
		t.Errorf("header missing provider/model; got:\n%s", content)
	}
	if !strings.Contains(content, "FAKE FINDINGS") {
		t.Errorf("provider output missing; got:\n%s", content)
	}
}

// TestAdversarialStalenessOnExplicitInvocationPath exercises the real
// explicit-invocation sequence that Cluster D made canonical:
//
//	aidw review-bundle .  ->  aidw adversarial-review .  ->  (edit files)  ->
//	aidw synthesize-review .
//
// review-bundle.json is NOT rebuilt before synthesis, so a fingerprint read
// back from it would always compare equal to the one the adversarial pass
// stamped. Synthesis must recompute from git instead.
func TestAdversarialStalenessOnExplicitInvocationPath(t *testing.T) {
	setupRepoWithProvider := func(t *testing.T) string {
		t.Helper()
		fake := t.TempDir()
		script := filepath.Join(fake, "gemini")
		if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\necho 'FAKE FINDINGS'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))

		dir := t.TempDir()
		initGitRepo(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "foo.txt"), []byte("original\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"git", "-C", dir, "add", "foo.txt"},
			{"git", "-C", dir, "commit", "-m", "add foo"},
		} {
			if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
				t.Fatalf("git setup %v: %v\n%s", args, err, out)
			}
		}
		// A working-tree change so the adversarial pass has something to review.
		if err := os.WriteFile(filepath.Join(dir, "foo.txt"), []byte("changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReviewBundle(dir); err != nil {
			t.Fatalf("ReviewBundle: %v", err)
		}
		if _, err := AdversarialReview(dir, "gemini", "test-model", 30); err != nil {
			t.Fatalf("AdversarialReview: %v", err)
		}
		return dir
	}

	t.Run("working tree mutated after the pass is labelled stale", func(t *testing.T) {
		dir := setupRepoWithProvider(t)

		// Mutate the working tree WITHOUT re-running ReviewBundle.
		if err := os.WriteFile(filepath.Join(dir, "foo.txt"), []byte("changed again\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		result, err := SynthesizeReview(dir)
		if err != nil {
			t.Fatalf("SynthesizeReview: %v", err)
		}
		data, _ := os.ReadFile(result.ReviewPath)
		content := string(data)
		if !strings.Contains(content, "_Stale:") {
			t.Errorf("a diff change after the adversarial pass must be flagged stale; got:\n%s", content)
		}
		if !strings.Contains(content, "FAKE FINDINGS") {
			t.Error("stale findings should still be imported")
		}
	})

	t.Run("unchanged working tree is not labelled stale", func(t *testing.T) {
		dir := setupRepoWithProvider(t)

		result, err := SynthesizeReview(dir)
		if err != nil {
			t.Fatalf("SynthesizeReview: %v", err)
		}
		data, _ := os.ReadFile(result.ReviewPath)
		content := string(data)
		if strings.Contains(content, "_Stale:") {
			t.Errorf("an unchanged working tree must not be labelled stale; got:\n%s", content)
		}
		if !strings.Contains(content, "fingerprint matches the current") {
			t.Errorf("expected a matching provenance line; got:\n%s", content)
		}
	})
}
