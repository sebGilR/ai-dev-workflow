---
name: wip-resume
description: Resume work from the current branch state without redoing the full workflow.
disable-model-invocation: true
---

When this skill is used:

1. Run:

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py status .
```

2. Read the current branch `.wip` files, starting with `status.json`, `context.md`, `plan.md`, and `execution.md`.
3. Summarize the current stage, what is done, and the next recommended action.
4. Continue from the right stage instead of restarting the workflow.
