package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/hookgate"
)

// hookGateCmd implements the PreToolUse workflow-gate hook. wip-gate.sh execs
// straight into this command and passes its stdout through verbatim, so this
// command MUST NEVER call Die/os.Exit(1)/PrintJSON's error path and must
// always exit 0 with a single line of valid permissionDecision JSON — a
// non-zero exit or malformed stdout here has no fallback downstream.
var hookGateCmd = &cobra.Command{
	Use:   "hook-gate",
	Short: "PreToolUse hook: gate Edit/Write/NotebookEdit behind an active .wip session",
	Run: func(c *cobra.Command, args []string) {
		defer func() {
			if r := recover(); r != nil {
				// A distinct decision/reason from AllowOutput()'s
				// binary-unavailable literal (that one is pinned
				// byte-for-byte to wip-gate.sh's own fallback and means a
				// different thing) — this is an internal Evaluate panic,
				// not a missing binary, but still an unconditional allow.
				fmt.Println(string(hookgate.RenderOutput(hookgate.Decision{
					Allow:  true,
					Reason: "wip-gate: internal error; failing open",
				})))
			}
		}()
		var body []byte
		if info, err := os.Stdin.Stat(); err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
			body, _ = io.ReadAll(os.Stdin)
		}
		decision := hookgate.Evaluate(body, os.Getenv)
		fmt.Println(string(hookgate.RenderOutput(decision)))
	},
}

func init() {
	Root.AddCommand(hookGateCmd)
}
