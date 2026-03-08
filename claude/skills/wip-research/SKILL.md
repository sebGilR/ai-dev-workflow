---
name: wip-research
description: Gather relevant implementation research and save it into .wip files.
disable-model-invocation: true
---

When this skill is used:

1. Ensure the repo is initialized for the current branch.
2. Use the `wip-researcher` subagent to gather relevant codebase findings.
3. Update `research.md` with concrete findings.
4. Update `context.md` with a concise distilled context snapshot for continuation.
5. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . researched
```

Keep the notes compact and continuation-friendly.
