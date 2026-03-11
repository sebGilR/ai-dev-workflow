---
name: wip-review
description: Prepare a review bundle and consolidate review notes.
---

When this skill is used:

1. Ensure the repo is initialized.

2. Build the review bundle:

```bash
~/.claude/ai-dev-workflow/bin/aidw review-bundle .
```

3. Optionally run adversarial Gemini review (only if `AIDW_GEMINI_REVIEW=1`):

Before proceeding to synthesis, decide whether this change warrants adversarial review. Run it if ANY of the following apply:
- The diff touches more than 3 files
- The change modifies public API surface, authentication/authorization logic, data model/schema, or concurrent code

Skip if the change is a small refactor, rename/formatting, docs-only update, or trivially small diff.

If warranted:

```bash
~/.claude/ai-dev-workflow/bin/aidw gemini-review .
```

If the command fails because Gemini CLI is not installed or auth is not configured, continue to step 5 without adversarial results. The `## Adversarial Review` section will be omitted from the synthesized review.

4. Synthesize the review scaffold (writes adversarial review (if present) + `## Claude Review` placeholder into `review.md`):

```bash
~/.claude/ai-dev-workflow/bin/aidw synthesize-review .
```

5. Use the `wip-reviewer` subagent to fill in the `## Claude Review` section of the already-written `review.md`.

The reviewer should:
- Read the existing `review.md`
- Read the review bundle (`review-bundle.json`) for additional context
- **Perform an independent analysis of the git diff directly**
- Use any prior review findings as supplementary information, not the primary source
- Identify issues prior passes may have missed
- Focus on: architecture fit, maintainability, edge cases, API design, cross-file dependencies
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
