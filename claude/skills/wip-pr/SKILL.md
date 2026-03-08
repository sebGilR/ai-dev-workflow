---
name: wip-pr
description: Prepare PR-ready notes for the current branch.
disable-model-invocation: true
---

When this skill is used:

1. Read `execution.md`, `review.md`, and the current diff.
2. Update `pr.md` with a clean PR summary, test notes, and known risks.
3. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . pr-prep
```

4. Present a concise PR-ready summary.
