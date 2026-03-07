---
name: wip-review
description: Prepare a review bundle, run local Ollama review passes if available, and consolidate review notes.
disable-model-invocation: true
---

When this skill is used:

1. Ensure the repo is initialized.

2. Build the review bundle:

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py review-bundle .
```

3. Check if Ollama is available locally:

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py ollama-check
```

4. If Ollama is available, run all review passes:

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py review-all .
```

This runs `bug-risk`, `missing-tests`, `regression-risk`, and `docs-needed` passes and saves individual JSON results into `.wip/<branch>/`.

If Ollama is not available, skip to step 5. The review can proceed with Claude's own analysis.

5. Use the `wip-reviewer` subagent to consolidate all findings into `review.md`.

The reviewer should:
- Read the review bundle (`review-bundle.json`)
- Read any Ollama review results (`ollama-review-*.json`)
- Analyze the git diff directly
- Produce a prioritized review with:
  - High priority (blockers)
  - Medium priority (should fix)
  - Low priority (suggestions)
- Note missing tests and regression risks

6. Synthesize the final review:

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py synthesize-review .
```

7. Update the stage:

```bash
python ~/.claude/ai-dev-workflow/scripts/aidw.py set-stage . reviewed
```

8. Summarize the review findings, focusing on blockers and high-priority issues.
