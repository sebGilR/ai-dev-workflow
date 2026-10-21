---
name: wip-install
description: Verify the ai-dev-workflow installation and explain how to bootstrap a repo or workspace.
---

When this skill is used:

1. Confirm that `~/.claude/ai-dev-workflow` exists.
2. Confirm that the `wip-*` skills are installed under `~/.claude/skills/`.
3. Confirm that the `wip-*` agents are installed under `~/.claude/agents/`.
4. Confirm that the current repo has `.wip/` and `.claude/repo-docs/`.
5. If the current repo is missing bootstrap files, suggest running:

```bash
~/.claude/ai-dev-workflow/bin/aidw bootstrap .
```

For a full verification:

```bash
~/.claude/ai-dev-workflow/bin/aidw verify
```
