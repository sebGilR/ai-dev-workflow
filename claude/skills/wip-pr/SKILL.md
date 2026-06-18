---
name: wip-pr
description: Prepare PR-ready notes for the current branch.
---

When this skill is used:

1. Read `execution.md`, `review.md`, and the current diff.
2. Update `pr.md` with a clean PR summary, test notes, and known risks.
3. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . pr-prep
```

4. Present a concise PR-ready summary.

## Commit hygiene

Never pass `--no-verify`, `--no-gpg-sign`, or otherwise skip git hooks when committing or pushing, unless the user explicitly asks for it in the current request. Hooks exist to catch problems before they land, and skipping them silently hides failures the user expects to be caught. If a hook fails, surface the failure and fix the underlying issue rather than bypassing it.
