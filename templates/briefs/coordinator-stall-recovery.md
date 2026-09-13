# Coordinator Stall Recovery

Procedure for a coordinating session when a dispatched background agent's completion
notification reports a stall (Claude Code's own 600s no-progress watchdog already detects this
and delivers `status: failed` — there is nothing to build for detection, only for recovery).

- **Resume, don't cold-redispatch.** Use `SendMessage` to resume the *named* agent instance from
  its own transcript. A cold re-dispatch discards the context it had already built up.
- **Inspect the worktree yourself.** Run `git status --short` and `git log <base>..HEAD` in the
  stalled agent's worktree directly from the coordinator session — never delegate this inspection
  to another subagent.
- **Redispatch at most once.** If the resumed/redispatched agent stalls a second time, escalate to
  the user rather than attempting a third silent redispatch.
- **If `SendMessage` cannot address the stalled agent** (it errors, or the instance is no longer
  addressable), one cold re-dispatch is permitted and **counts as** the single retry above — it is
  not a fourth option in addition to it.
