---
name: wip-fanout
description: 'Fans out independent, non-file-overlapping work items to background agents for concurrent dispatch, using existing aidw work-model and Claude Code Agent/SendMessage primitives.'
context: fork
agent: coordinator
effort: medium
---

# Workflow: Fan-Out Dispatch

**Goal:** Concurrently dispatch multiple independent, non-file-overlapping work items as
background agents, without building any new coordination infrastructure.

## Model guidance

The Select step's overlap check is deterministic once the resolution rule below is followed, so
`effort: medium` is usually enough — escalate to the frontier tier if candidate specs are large or
an overlap determination is genuinely ambiguous. `aidw model route <tier>` prints the configured
model name (env `AIDW_FRONTIER_MODEL` / `AIDW_EFFICIENT_MODEL`). Respect an explicit user model
choice without re-prompting, and never claim a model switch happened unless the host actually
performed it.

## Non-Goals

This skill formalizes an ad hoc pattern already run manually; it intentionally does NOT add a
dependency graph, a lock file, an epic/integration branch, persisted batch/epic state, merge-
ordering automation, a new CLI subcommand, or a new state file. `work.json`'s `initiative_id`
field is a reserved-but-unused placeholder for a future epic concept
(`docs/design/work-model.md:96`) — not activated here. Prose plus existing primitives only.
Revisit only after an actual file-scope collision occurs between concurrently-dispatched items.

## Portability

`Workflow` (a Claude-Code-exclusive, main-session-only primitive) may accelerate deterministic
fan-out on hosts that support it, but this skill's contract must not depend on it — aidw ships
Gemini/Copilot surfaces too (`templates/global/gemini_managed_block.md`,
`templates/github/copilot-instructions.md`, generated `.github/agents/*.md`). This skill must
degrade to plain `Agent()`-tool dispatch on any host.

## STEP 1: Select

1. Enumerate candidates: run `aidw work list .` from the repo root (path argument is required —
   `cobra.ExactArgs(1)`). This filters to records attached to the current repo and excludes
   archived records unless `--include-archived`. If the command exits non-zero (e.g. this repo is
   not registered in `repos.json`) or prints `[]`, report "no fan-out candidates" and stop — do not
   proceed to Brief/Dispatch with zero or unresolved candidates. **Then filter to `lifecycle ==
   "active"` only** — `work list`'s archived-exclusion is not enough on its own, since it still
   returns `paused`/`done` records. A `done`/`paused` record is not a fan-out candidate even if it
   still has a resolvable `spec.md` (a finished item's spec doesn't disappear); report each
   excluded-by-lifecycle record by id and lifecycle value, don't silently drop it.
2. For each remaining candidate record, resolve which `.wip/` directory (and thus `spec.md`)
   it corresponds to — no field on `Attachment` points directly at a `.wip/<date>-<slug>/spec.md`
   path, so this link must be derived. A record's `attachments[]` may contain more than one entry
   (e.g. across repos); **select only the attachment whose `repo_id` matches this repo's own
   `repo_id`** (from `aidw status .` run at the repo root, or equivalently `state.RepoIdentity`) —
   never assume `attachments[0]` or "any attachment that happens to match." If more than one
   attachment matches this repo's `repo_id` (should not normally happen, but do not guess), exclude
   the candidate and report "ambiguous: multiple attachments for this repo" rather than picking
   one. **Do not compare `aidw status`'s printed `Branch:` line to `attachments[].branch`** —
   `aidw status` prints the SafeSlug (`status.json`'s `branch` field, e.g.
   `chore-speed-review-batch6-af43d0e7`), not the raw git branch, and a SafeSlug never equals the
   raw branch for any name containing `/` (proven against this repo's own state). Use the raw git
   branch instead:
   a. Take the selected (repo-matched) attachment's `worktree_path` and `branch` — not an
      unqualified `attachments[].worktree_path`/`attachments[].branch`.
   b. Run `git -C <worktree_path> branch --show-current` to get the raw current branch of that
      worktree. If this does not equal the selected attachment's `branch` exactly, exclude the
      candidate and record "branch mismatch — worktree has moved on" in the report.
   c. Run `aidw status <worktree_path>` (explicit path argument required) and take **only** its
      first `WIP directory:` line (first match — `aidw status` also prints a `context.md` preview
      that may itself contain a `- Branch:`-style line; do not match on that).
   d. If `aidw status` exits non-zero, prints no `WIP directory:` value, or `<wip_dir>/spec.md`
      does not exist, exclude this candidate and record "no resolvable spec.md for this work item"
      in the report. Never fall back to scanning `.wip/*-<branch-slug>-*/` as a guess — an explicit
      exclusion is safer than a wrong overlap check.
3. For every pair of successfully-resolved candidates, read each resolved `spec.md`'s `### Task N`
   headers and their **`**File**:`/`**Files**:` header lines only** — not prose or `file:line`
   citations elsewhere in the task body, which would produce false collisions (this spec itself
   cites `templates/briefs/isolated-agent-brief.md` in prose four times while only Task 1 actually
   edits it). Two candidates collide if their `**File**:`/`**Files**:` lines name any file path in
   common.
4. On collision, **do not auto-resolve which item to drop.** Report the collision (both items, the
   shared file path) and stop dispatching that pair — let the user decide which one to run now vs.
   later. (A `created_at`/`updated_at`-based tie-break was considered and rejected: it is both an
   unsound proxy for "further along" and a step toward the merge-ordering-automation this skill
   explicitly excludes as a non-goal.)
5. Cap the batch at 3 concurrent items by default. A higher cap may be requested only as explicit
   argument text in the `/wip-fanout` invocation itself (e.g. `/wip-fanout --max 5`) — there is no
   CLI flag to parse; if a higher cap is requested this way, state the raised concurrency and the
   added risk (more concurrent worktrees to track per completion notification) explicitly in the
   report.

## STEP 2: Brief

1. One dispatch prompt per selected item.
2. Reference — do not inline — `~/.claude/ai-dev-workflow/templates/briefs/isolated-agent-brief.md`
   in each dispatch prompt (e.g. "Follow the ground rules in
   `~/.claude/ai-dev-workflow/templates/briefs/isolated-agent-brief.md`."), to keep dispatch
   prompts short. The brief itself permits either convention ("Paste or reference this brief in
   the dispatch prompt" — `templates/briefs/isolated-agent-brief.md:4`); this skill's contract is
   "reference" (see Open Question 1 in `spec.md` for why this differs from `wip-implement`'s
   current wording). Do not reference `coordinator-stall-recovery.md` in the dispatch prompt —
   that file is coordinator-facing (Task 1), not agent-facing; Step 4 below is where the
   coordinator itself consults it.
3. Also brief each agent on the *other* dispatched agents' branches and worktree paths (not their
   task content) so independently-dispatched agents don't collide on git state even though their
   file scopes already passed the Select-step overlap check.

## STEP 3: Dispatch

1. One named `Agent()` call per selected item, so a later `SendMessage` can address it specifically
   by name.
2. Agents background automatically. Do not poll, sleep, or read a dispatched agent's in-progress
   output.
3. Return a dispatch table (agent name, work item id/title, branch, worktree path) as this step's
   only synchronous output. Everything after this step is driven by completion notifications.

## STEP 4: On Each Completion Notification

1. Run `aidw work checkpoint <worktree_path>` using that item's own worktree path — not a bare `.`
   (`aidw work checkpoint` dies with "path is required without --from-hook" if no path is given,
   and a literal `.` would resolve against the *coordinator's* own cwd/branch, not the completed
   item's). **This only bumps the record's `updated_at` timestamp** (`runCheckpointDirect` is a
   no-op mutator) — it does not persist an outcome, stage, or result. The actual outcome (done /
   stalled / etc.) is recorded in this skill's own per-notification report and in that item's own
   `execution.md`, not in `work.json`. If the command exits non-zero (`ErrAmbiguousWork` — multiple
   candidate records for that path, printed as a list — or `ErrNoActiveWork` — no record at all),
   report the failure for that item and continue chaining the rest of the batch; do not abort the
   whole fan-out on one item's checkpoint failure.
2. Chain that item's own next step immediately (e.g. dispatch its review pass). Do not barrier on
   the rest of the batch.
3. If the notification reports a stall, the coordinator (not the dispatched agent) runs the
   procedure in `templates/briefs/coordinator-stall-recovery.md` (SendMessage-resume by name
   once; escalate to the user on a second stall — never a third silent attempt).

## STEP 5: Close

1. Report per-item outcomes (done / stalled-and-escalated / still running).
2. Never auto-merge any dispatched branch to `main`.

## RTK Usage (Token Compression)

When RTK is installed (`rtk init -g`), Bash commands are automatically compressed:

- `rtk aidw work list .`

If a command's compressed output is insufficient, ask the user before running `rtk proxy <cmd>`.
