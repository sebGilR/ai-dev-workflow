---
name: wip-research
description: Gather relevant implementation research and save it into .wip files.
---

When this skill is used:

1. Ensure the repo is initialized for the current branch.
2. Run `research-scan` to narrow the file list before deep exploration:

```bash
~/.claude/ai-dev-workflow/bin/aidw research-scan --goal "<topic>" .
```

   Replace `<topic>` with the exact research topic from the skill arguments (do not
   paraphrase). If no topic was provided, use the goal sentence from `context.md`.
   The subagent will read `.wip/<branch>/research-scan.json` automatically and use
   its `results` array as its starting file list.

3. Use the `wip-researcher` subagent to gather codebase findings, starting from the
   files identified in `research-scan.json`. Prefer Serena for symbol lookup, call chain
   tracing, and pattern identification within those files. The subagent may expand scope
   if the scan results look unrelated to the actual task.
4. Update `research.md` with concrete findings.
5. Update `context.md` with a concise distilled context snapshot for continuation.
6. Verify the write succeeded:

```bash
~/.claude/ai-dev-workflow/bin/aidw verify-research .
```

7. If verification passes, set the stage:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . researched
```

Keep the notes compact and continuation-friendly.
