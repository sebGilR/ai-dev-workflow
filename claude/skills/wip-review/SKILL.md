---
name: wip-review
description: Prepare a review bundle and consolidate review notes.
model: claude-opus-4.6
---

When this skill is used:

1. Ensure the repo is initialized.

2. Build the review bundle:

```bash
~/.claude/ai-dev-workflow/bin/aidw review-bundle .
```

3. Synthesize the review scaffold (writes `## Claude Review` placeholder into `review.md`):

```bash
~/.claude/ai-dev-workflow/bin/aidw synthesize-review .
```

4. Use the `wip-reviewer` subagent to fill in the `## Claude Review` section of the already-written `review.md`.

The reviewer should:
- Read the existing `review.md`
- Read the review bundle (`review-bundle.json`) for additional context
- **Perform an independent analysis of the git diff directly**
- **Ultrathink about each finding before flagging it** — only surface issues that genuinely matter
- Focus on: architecture fit, maintainability, edge cases, API design, cross-file dependencies
- Write a prioritized Claude analysis into the `## Claude Review` section:
  - High priority (blockers)
  - Medium priority (should fix)
  - Low priority (suggestions)
- Note missing tests and regression risks
- Include a final verdict

5. Adversarial Gemini review:

Check the `AIDW_GEMINI_REVIEW` environment variable:
- If `AIDW_GEMINI_REVIEW=0`: skip this step entirely (CI opt-out).
- If `AIDW_GEMINI_REVIEW=1`: run without prompting (CI opt-in).
- Otherwise (not set): ask the user: **"Run Gemini adversarial review? [y/N]"** — proceed only if they answer yes.

If running:

```bash
AIDW_GEMINI_REVIEW=1 ~/.claude/ai-dev-workflow/bin/aidw gemini-review .
```

Then re-run synthesize-review to merge adversarial findings into `review.md`:

```bash
~/.claude/ai-dev-workflow/bin/aidw synthesize-review .
```

If the command fails because Gemini CLI is not installed or auth is not configured, skip this step entirely. The `## Adversarial Review` section will be omitted from `review.md`.

6. Verify the review.md write succeeded:

```bash
~/.claude/ai-dev-workflow/bin/aidw verify-review .
```

7. If verification passes, update the stage:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . reviewed
```

8. Summarize the review findings, focusing on blockers and high-priority issues.
