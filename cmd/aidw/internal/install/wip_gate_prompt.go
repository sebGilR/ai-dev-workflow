package install

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	embedfs "aidw"
)

// wipGateFragmentPath is the settings fragment merged in only when the user
// opts in interactively — never read by Bootstrap()'s unconditional
// base-template merge.
const wipGateFragmentPath = "templates/global/settings.wip-gate-fragment.json"

// wipGateMarker is checked against the merged settings.json's raw bytes to
// decide whether the hook is already enabled, so an already-opted-in user is
// never re-prompted on subsequent interactive runs.
const wipGateMarker = "wip-gate.sh"

// WipGateStatus reports what AskEnableWipGate found and, if it prompted,
// what it did.
type WipGateStatus struct {
	AlreadyEnabled bool
	Enabled        bool
	Skipped        bool
	Warning        string
}

// AskEnableWipGate offers to enable the opt-in workflow-gate PreToolUse hook.
// Detection (the "already enabled" check) always runs; the interactive
// prompt only runs when interactive is true, and `aidw upgrade` (no
// --interactive) must never merge this fragment on its own.
func AskEnableWipGate(interactive bool, w io.Writer, settingsPath string) WipGateStatus {
	return askEnableWipGate(interactive, w, os.Stdin, settingsPath)
}

func askEnableWipGate(interactive bool, w io.Writer, stdin io.Reader, settingsPath string) WipGateStatus {
	status := WipGateStatus{}
	if data, err := os.ReadFile(settingsPath); err == nil && bytes.Contains(data, []byte(wipGateMarker)) {
		status.AlreadyEnabled = true
		return status
	}
	if !interactive {
		return status
	}

	fmt.Fprintln(w, "\nThe workflow gate hook denies Edit/Write/NotebookEdit calls in a git")
	fmt.Fprintln(w, "repo when there's no active .wip session for the branch. It is opt-in")
	fmt.Fprintln(w, "because it fires in EVERY repo Claude Code touches on this machine, not")
	fmt.Fprintln(w, "just ai-dev-workflow-managed ones.")
	fmt.Fprint(w, "Enable the workflow gate hook? [y/N]: ")

	reader := bufio.NewReader(stdin)
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	if ans != "y" && ans != "yes" {
		status.Skipped = true
		return status
	}

	fragment, err := embedfs.FS.ReadFile(wipGateFragmentPath)
	if err != nil {
		status.Warning = fmt.Sprintf("wip-gate fragment missing: %v", err)
		return status
	}
	if err := MergeSettings(settingsPath, fragment); err != nil {
		status.Warning = fmt.Sprintf("merge wip-gate hook: %v", err)
		return status
	}
	status.Enabled = true
	fmt.Fprintln(w, "  Workflow gate hook enabled.")
	return status
}
