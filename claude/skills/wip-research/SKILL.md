---
name: wip-research
description: Gather relevant implementation research and save it into .wip files.
---

When this skill is used:

1. Ensure the repo is initialized for the current branch.
2. Use the `wip-researcher` subagent to gather codebase findings. The subagent uses Serena MCP tools as its primary navigation layer — do not attempt code navigation in the main context before dispatching the subagent. The subagent may expand scope as needed based on the task.
3. Update `research.md` with concrete findings.
4. Update `context.md` with a concise distilled context snapshot for continuation.
5. Verify the write succeeded:

```bash
~/.claude/ai-dev-workflow/bin/aidw verify-research .
```

6. If verification passes, set the stage:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . researched
```

Keep the notes compact and continuation-friendly.

## RTK Usage (Token Compression)

When RTK is installed (`rtk init -g`), the Claude Code Bash hook automatically rewrites common commands (`ls`, `grep`, `git`, etc.) to their RTK equivalents — no explicit invocation needed. If you want to call RTK directly (e.g., to force compression on a command the hook doesn't cover), use:

- `rtk ls <dir>` — compact directory tree
- `rtk grep "<pattern>" <dir>` — grouped search results
- `rtk find "<glob>" <dir>` — condensed file discovery
- `rtk git log --oneline -20` — compact commit history
- `rtk git diff --stat` — condensed diff summary

Note: explicit `rtk <cmd>` calls require RTK to be installed — they are not transparent fallbacks. If RTK is absent, use the raw command equivalents directly.
