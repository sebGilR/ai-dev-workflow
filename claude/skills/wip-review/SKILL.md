---
name: wip-review
description: Prepare a review bundle, run local Ollama review passes if available, and consolidate review notes.
disable-model-invocation: true
---

When this skill is used:

1. Ensure the repo is initialized.

2. Build the review bundle:

```bash
~/.claude/ai-dev-workflow/bin/aidw review-bundle .
```

3. Run all Ollama review passes. The server is started automatically if it is not already running and stopped when the passes complete:

```bash
~/.claude/ai-dev-workflow/bin/aidw review-all .
```

This runs `bug-risk`, `missing-tests`, `regression-risk`, and `docs-needed` passes and saves individual JSON results into `.wip/<branch>/`.

If the command fails because Ollama is not installed, continue to step 4 without Ollama results. You can verify Ollama availability separately with `aidw ollama-check`. The review can proceed with Claude's own analysis.

4. Use the `wip-reviewer` subagent to consolidate all findings into `review.md`.

The reviewer should:
- Read the review bundle (`review-bundle.json`)
- Read any Ollama review results (`ollama-review-*.json`)
- Analyze the git diff directly
- Produce a prioritized review with:
  - High priority (blockers)
  - Medium priority (should fix)
  - Low priority (suggestions)
- Note missing tests and regression risks

5. Synthesize the final review:

```bash
~/.claude/ai-dev-workflow/bin/aidw synthesize-review .
```

6. Update the stage:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . reviewed
```

7. Summarize the review findings, focusing on blockers and high-priority issues.
