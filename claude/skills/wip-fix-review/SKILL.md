---
name: wip-fix-review
description: Work through review findings and document what was fixed.
---

When this skill is used:

1. Read `review.md`, `execution.md`, and the current diff.
2. Fix the highest-value review issues first.
3. Update `execution.md` with concise notes about the fixes.
4. Optionally annotate `review.md` with addressed items.
5. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . review-fixed
```
