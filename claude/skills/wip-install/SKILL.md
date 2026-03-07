---
name: wip-install
description: Verify the ai-dev-workflow installation and explain how to bootstrap a repo or workspace.
disable-model-invocation: true
---

When this skill is used:

1. Confirm that `~/.claude/ai-dev-workflow` exists.
2. Confirm that the `wip-*` skills are installed under `~/.claude/skills/`.
3. Confirm that the `wip-*` agents are installed under `~/.claude/agents/`.
4. Confirm that the current repo has `.wip/` and `.claude/repo-docs/`.
5. If the current repo is missing bootstrap files, suggest running:

```bash
~/.claude/ai-dev-workflow/install.sh .
```

Use `python ~/.claude/ai-dev-workflow/scripts/aidw.py ensure-repo .` when you need to seed the current repo only.

For a full verification:

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py verify
```
