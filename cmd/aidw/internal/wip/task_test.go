package wip

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestNextTask_NoActiveWorkCreatesNothing covers AC-B2 for `aidw task next`.
// Before the fix, NextTask called EnsureBranchState: on a clean repo it seeded
// a .wip directory including an empty spec.md, parsed zero tasks out of it, and
// returned (nil, nil) — which the command reports as "All tasks in spec.md are
// completed." A never-started branch must instead surface ErrNoActiveWork.
func TestNextTask_NoActiveWorkCreatesNothing(t *testing.T) {
	dir := initGitRepo(t)

	task, err := NextTask(dir)

	if !errors.Is(err, ErrNoActiveWork) {
		t.Errorf("expected ErrNoActiveWork, got err=%v task=%+v", err, task)
	}
	if task != nil {
		t.Errorf("expected no task, got %+v", task)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".wip")); !os.IsNotExist(statErr) {
		t.Error("NextTask must not create a .wip directory")
	}
}

// TestNextTask_ReturnsFirstUncompletedTask confirms the happy path still works
// once state exists.
func TestNextTask_ReturnsFirstUncompletedTask(t *testing.T) {
	dir := initGitRepo(t)
	state, err := EnsureBranchState(dir, "")
	if err != nil {
		t.Fatalf("EnsureBranchState: %v", err)
	}

	spec := "# Spec\n\n## Implementation\n\nTask 1: first thing\nTask 2: second thing\n"
	if err := os.WriteFile(filepath.Join(state.WipDir, "spec.md"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	task, err := NextTask(dir)
	if err != nil {
		t.Fatalf("NextTask: %v", err)
	}
	if task == nil || task.ID != 1 || task.Description != "first thing" {
		t.Fatalf("got %+v, want task 1 %q", task, "first thing")
	}

	if err := MarkTaskDone(dir, 1, task.Description); err != nil {
		t.Fatalf("MarkTaskDone: %v", err)
	}

	task, err = NextTask(dir)
	if err != nil {
		t.Fatalf("NextTask after done: %v", err)
	}
	if task == nil || task.ID != 2 {
		t.Fatalf("got %+v, want task 2", task)
	}
}
