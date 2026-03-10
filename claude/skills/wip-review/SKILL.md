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

If the command fails because Ollama is not installed, continue to step 5 without Ollama results. You can verify Ollama availability separately with `aidw ollama-check`. The review can proceed with Claude's own analysis.

4. Analyze review coverage gaps (only if Ollama review completed):

```bash
~/.claude/ai-dev-workflow/bin/aidw review-gaps .
```

This identifies:
- Files with no specific findings
- High-severity findings needing deeper analysis
- Suggested areas for Claude's independent review

If gaps are identified that need focused follow-up, optionally run targeted review:

```bash
~/.claude/ai-dev-workflow/bin/aidw review-targeted . --files <files> --focus <focus>
```

Where `<files>` is a comma-separated list of file paths and `<focus>` is a specific concern (e.g., "error-handling", "performance", "concurrency").

4.5. Optionally run adversarial Gemini review (only if `AIDW_GEMINI_REVIEW=1`):

Before proceeding to synthesis, decide whether this change warrants adversarial review. Run it if ANY of the following apply:
- The diff touches more than 3 files
- Any Ollama finding has severity `HIGH` or `CRITICAL`
- The change modifies public API surface, authentication/authorization logic, data model/schema, or concurrent code

Skip if the change is a small refactor, rename/formatting, docs-only update, or trivially small diff.

If warranted:

```bash
~/.claude/ai-dev-workflow/bin/aidw gemini-review .
```

If the command fails because Gemini CLI is not installed or auth is not configured, continue to step 5 without adversarial results. The `## Adversarial Review` section will be omitted from the synthesized review.

5. Synthesize the review scaffold (writes Ollama findings + gap analysis + targeted review + adversarial review (if present) + `## Claude Review` placeholder into `review.md`):

```bash
~/.claude/ai-dev-workflow/bin/aidw synthesize-review .
```

6. Use the `wip-reviewer` subagent to fill in the `## Claude Review` section of the already-written `review.md`.

The reviewer should:
- Read the existing `review.md` (which already contains Ollama findings, gap analysis, and any targeted review results)
- Read the review bundle (`review-bundle.json`) for additional context
- **Perform an independent analysis of the git diff directly** — do not just summarize Ollama findings
- Use Ollama findings and gap analysis as supplementary information, not the primary source
- Identify issues Ollama may have missed
- Focus on: architecture fit, maintainability, edge cases, API design, cross-file dependencies
- Write a prioritized Claude analysis into the `## Claude Review` section:
  - High priority (blockers)
  - Medium priority (should fix)
  - Low priority (suggestions)
- Note missing tests and regression risks
- Include a final verdict

7. Verify the review.md write succeeded:

```bash
~/.claude/ai-dev-workflow/bin/aidw verify-review .
```

8. If verification passes, update the stage:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . reviewed
```

9. Summarize the review findings, focusing on blockers and high-priority issues.
