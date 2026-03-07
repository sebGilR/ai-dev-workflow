---
name: wip-start
description: Initialize the branch-scoped .wip/<branch>/ folder and seed all workflow files.
disable-model-invocation: true
---

When this skill is used:

1. Detect the current repo root.
2. Run:

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py start .
```

3. Read the resulting `status.json` and `context.md`.
4. Summarize what was initialized.
5. Continue the conversation from the initialized workflow state.
