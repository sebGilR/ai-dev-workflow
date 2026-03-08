---
name: wip-implement
description: Implement the next planned chunk of work and update execution notes.
disable-model-invocation: true
---

When this skill is used:

1. Read the current `plan.md`, `research.md`, `context.md`, and `status.json`.
2. Implement the next chunk of work.
3. Append a concise update to `execution.md` describing what changed and why.
4. Optionally refresh `context.md` if the implementation materially changed the continuation context.
5. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . implementing
```

6. Summarize completed work and remaining work.
