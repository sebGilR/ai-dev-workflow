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

When RTK is installed (`rtk init -g`), common commands are automatically compressed. **In large codebases**, using RTK is essential to avoid hitting context limits during research:

- `rtk ls -R <dir>` — compact recursive directory tree
- `rtk grep "<pattern>"` — grouped search results with snippet deduplication
- `rtk find "<glob>"` — condensed file discovery
- `rtk git log -n 50` — compact history summary

If RTK is absent, use the raw command equivalents directly. If you need full uncompressed output to see complete context, ask the user before running `rtk proxy <cmd>`.
