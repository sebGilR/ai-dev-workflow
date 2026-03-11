---
name: wip-cleanup
description: Strip the current branch's .wip folder down to context.md and pr.md, removing all other files.
---

When this skill is used:

1. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw cleanup-branch .
```

2. Read the JSON output to confirm what was deleted and what was kept.
3. Report a concise summary: which branch folder was cleaned, which files were removed, and which files remain.
