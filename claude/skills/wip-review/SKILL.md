---
name: wip-review
description: Prepare a review bundle and consolidate review notes.
effort: high
---

## Model guidance

Review benefits most from the frontier tier — this is the last line of
defense before a PR, and missed findings are expensive. `effort: high`
above requests that tier where the host honors SKILL.md frontmatter
(Claude Code >= 2.1.259); on older hosts it is silently ignored, which is
exactly what step 4 below exists to handle explicitly via
`AIDW_REVIEW_MODEL` / the escalation prompt. `aidw model route <tier>`
prints the configured name for either tier (env `AIDW_FRONTIER_MODEL` /
`AIDW_EFFICIENT_MODEL`) — this is a separate, general-purpose mechanism
from the review-specific `AIDW_REVIEW_MODEL` override in step 4, which
stays as-is. Respect an explicit user model choice without re-prompting,
and never claim a model switch happened unless the host actually
performed it.

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

4. Model selection and Opus escalation:

   a. Run:
   ```bash
   git --no-pager diff HEAD --stat | tail -1
   ```
   Show the output to the user.

   b. Check the `AIDW_REVIEW_MODEL` environment variable:
   - If set to `"opus"` → use the deepest-analysis model tier (CI override, no prompt)
   - If set to `"sonnet"` → use the default model tier (CI override, no prompt)
   - If unset → do NOT ask the user by default. Use the default model tier and proceed
     automatically, **unless** either of these holds, in which case escalate to the
     deeper-analysis tier without prompting:
     - the diff stat from step 4a shows more than ~400 changed lines or more than
       ~15 files touched (a reasonable proxy for "large enough that missed findings
       are expensive"), or
     - the user has already passed an explicit flag/preference for this run (e.g.
       invoked with an escalation flag, or stated a model preference earlier in
       the conversation).
     Mention which path was taken (default tier vs. auto-escalated, and why) in
     the review output instead of pausing to ask.

5. Use the `wip-reviewer` subagent to fill in the `## Claude Review` section of the already-written `review.md`.

The reviewer should:
- Read the existing `review.md`
- Read the review bundle (`review-bundle.json`) for additional context
- **Perform an independent analysis of the git diff directly**
- **Think carefully about each finding before flagging it** — only surface issues that genuinely matter
- Focus on: architecture fit, maintainability, edge cases, API design, cross-file dependencies
- Write a prioritized Claude analysis into the `## Claude Review` section:
  - High priority (blockers)
  - Medium priority (should fix)
  - Low priority (suggestions)
- Note missing tests and regression risks
- Include a final verdict

External adversarial review is never run or offered by this workflow. It runs only when the user explicitly invokes `aidw adversarial-review .`.

6. Verify the review.md write succeeded:

```bash
~/.claude/ai-dev-workflow/bin/aidw verify-review .
```

7. If verification passes, update the stage:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . reviewed
```

8. Summarize the review findings, focusing on blockers and high-priority issues.

## RTK Usage (Token Compression)

The `aidw review-bundle` command already produces compact output. For any supplementary commands run during review, RTK reduces noise significantly:

- `rtk git diff HEAD` — condensed diff for large changesets
- `rtk tsc --noEmit` — TypeScript errors grouped by file
- `rtk lint` / `rtk lint biome` — linter output grouped by rule
- `rtk cargo clippy` — Rust lint output

These are most useful when the reviewer subagent runs supplementary checks outside the review bundle. If a check fails and the compressed output is insufficient to diagnose the cause, ask the user before bypassing:

> "I need full output from `<cmd>` to [reason]. Run without RTK compression? [y/N]"

Only use `rtk proxy <cmd>` after the user confirms.
