---
name: wip-sync
description: Save the current conversation context and repository state to the branch-scoped WIP folder.
---

Use this skill when you want to "bookmark" your current progress so it can be resumed perfectly later, or before ending a session.

When this skill is used:

1. **Progress Memo**: Summarize your current progress, any "mental notes" about the code, and the next 1-2 steps you were planning to take.
2. **Update WIP Artifacts**:
   - Append the progress memo to `execution.md` (if in implementing stage) or `research.md`.
   - Ensure `context.md` reflects any major shifts in goals.
3. **Snapshot Git State**: Run the snapshot helper to capture a diff and git status:
   ```bash
   ~/.claude/save-wip-snapshot.sh manual
   ```
4. **Distill Context**: Regenerate the compact context summary for zero-latency resumption:
   ```bash
   ~/.claude/ai-dev-workflow/bin/aidw summarize-context .
   ```
5. **Confirm**: Confirm to the user that the "brain state" is synced. Mention that they can pick up right here by running `/wip-resume` in a new session.
