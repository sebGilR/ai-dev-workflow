---
name: wip-start
description: Initialize the branch-scoped .wip/<branch>/ folder and seed all workflow files.
---

When this skill is used:

1. Detect the current repo root.
2. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw start .
```

3. Read the resulting `status.json` and `context.md`.
4. Summarize what was initialized.
5. Suggest running `/wip-plan` to begin the spec-driven planning sequence (Clarify -> Draft -> Skeptic Review).
6. Continue the conversation from the initialized workflow state.
