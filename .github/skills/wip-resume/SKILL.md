---
name: wip-resume
description: Resume work from the current branch state without redoing the full workflow.
---

When this skill is used, combine evidence rather than trusting a single
source — `context-summary.md` can be stale, and `handoff.md` can be more
recent than the last summary regeneration.

1. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw status .
```

   If this reports no active work, tell the user to run `/wip-start` instead
   of resuming.

2. Check `handoff.md` and the tail of `progress.log` in the branch's `.wip`
   directory — these are the latest MECHANICAL checkpoints, written by the
   Stop/PreCompact/SessionEnd hooks on every stop/compact/session-end
   regardless of whether anyone ran `/wip-sync`:

```bash
tail -10 <wip_dir>/progress.log 2>/dev/null
```

   Read `handoff.md` for the git status/diff/commit snapshot as of the last
   hook run. If `progress.log`'s most recent line carries a `session=<id>`
   value, treat it as a candidate for step-5's conversation-restore offer.

3. Check whether `context-summary.md` is current:

```bash
~/.claude/ai-dev-workflow/bin/aidw context-summary . --json
```

   - If `"stale": false`, read the summary via
     `~/.claude/ai-dev-workflow/bin/aidw context-summary .` — it is a
     trustworthy compact digest of all relevant workflow state.
   - If `"stale": true` (or the command errors because no summary exists),
     note "summary stale, using sources" and fall back to reading
     `status.json`, `context.md`, `plan.md`, and `execution.md` directly.

4. Read the artifacts needed for the recommended next action. Use
   `aidw next .` (or the stage in `status.json`) to decide what those are —
   e.g. `spec.md` plus any unfinished tasks when the stage is `implementing`,
   `review.md` when the stage is `reviewed`, etc.

   If the artifact backing the current stage is an empty header-only
   placeholder, a cleanup pass archived it. Look for the most recent copy
   under `<wip_dir>/archive/<timestamp>/` and say so rather than treating the
   stage as unbacked — `aidw cleanup-branch` downgrades the stage to
   `specified` when it archives, so a mismatch means the artifact predates
   that behaviour.

5. If `progress.log` carried a recent `session=<id>` value (step 2), offer
   `claude --resume <session-id>` as a conversation-restore complement to the
   file-based resume above — useful when the user wants the original
   reasoning trace, not just the artifacts. Do not write this to
   `status.json` or any other WIP file; it is a one-time suggestion.

6. Summarize the current stage, what is done, and the next recommended
   action. Explicitly state whether the resume context is **current**
   (summary fresh, or no summary needed because sources were read directly)
   or **degraded** (summary was stale and the fallback sources may be
   incomplete or out of date) and why.
7. Continue from the right stage instead of restarting the workflow.
