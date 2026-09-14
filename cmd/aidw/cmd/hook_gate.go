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
// always exit 0.
//
// Output contract (see hookgate.RenderOutput's doc comment for the full
// rationale): a deny decision prints exactly one line of
// permissionDecision:"deny" JSON; every allow decision — including every
// fail-open/panic-recovery path — prints NOTHING. Empty stdout is Claude
// Code's documented "no opinion, defer to the normal permission flow"
// signal; printing an empty line (or any JSON at all) on an allow path
// would either add a stray blank line or, worse, emit an affirmative
// "allow" that silently overrides the user's own permissions.ask/deny
// rules for Edit/Write/NotebookEdit. This command must never do that: its
// only job is to say "deny" in exactly one case and otherwise stay silent.
var hookGateCmd = &cobra.Command{
	Use:   "hook-gate",
	Short: "PreToolUse hook: gate Edit/Write/NotebookEdit behind an active .wip session",
	Run: func(c *cobra.Command, args []string) {
		defer func() {
			// An internal Evaluate panic is still a fail-open case: stay
			// silent (no output), never print an "allow" decision.
			recover()
		}()
		var body []byte
		if info, err := os.Stdin.Stat(); err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
			body, _ = io.ReadAll(os.Stdin)
		}
		decision := hookgate.Evaluate(body, os.Getenv)
		if out := hookgate.RenderOutput(decision); len(out) > 0 {
			fmt.Println(string(out))
		}
	},
}

func init() {
	Root.AddCommand(hookGateCmd)
}
