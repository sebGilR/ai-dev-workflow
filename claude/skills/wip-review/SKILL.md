---
name: wip-review
description: Prepare a review bundle, run local Ollama review passes if available, and consolidate review notes.
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

4. Synthesize the review scaffold (writes Ollama findings + `## Claude Review` placeholder into `review.md`):

```bash
~/.claude/ai-dev-workflow/bin/aidw synthesize-review .
```

5. Use the `wip-reviewer` subagent to fill in the `## Claude Review` section of the already-written `review.md`.

The reviewer should:
- Read the existing `review.md` (which already contains Ollama findings)
- Read the review bundle (`review-bundle.json`) for additional context
- Analyze the git diff directly
- Write a prioritized Claude analysis into the `## Claude Review` section:
  - High priority (blockers)
  - Medium priority (should fix)
  - Low priority (suggestions)
- Note missing tests and regression risks
- Include a final verdict

6. Verify the review.md write succeeded:

```bash
~/.claude/ai-dev-workflow/bin/aidw verify-review .
```

7. If verification passes, update the stage:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . reviewed
```

8. Summarize the review findings, focusing on blockers and high-priority issues.
