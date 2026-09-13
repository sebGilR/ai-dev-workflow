---
name: wip-clear
description: Archive all .wip branch folders except the most recently dated one.
---

When this skill is used:

1. Probe for an existing migrated work record with
   `~/.claude/ai-dev-workflow/bin/aidw work list .`. This command always
   exits 0 and prints `[]` when nothing is found — never `null` — so a bare
   exit-code check is not enough; read the JSON array itself.

2. **If `work list .` returns a non-empty array** (this repo has already
   been migrated into the work-model store via `aidw migrate-state`):

   a. The primary action for "I'm done with this branch" is now the
      lifecycle command, not the legacy archive pass:

      ```bash
      ~/.claude/ai-dev-workflow/bin/aidw work archive <id>
      ```

      Ask for confirmation before running it, and tell the user which
      `<id>` you're archiving (from the probe's output) and its title.

   b. After that, offer — informationally, not auto-run — to also check
      what a legacy pass would additionally do: "There is also unmigrated
      legacy `.wip` state still on disk for other branches on this repo;
      here's what a legacy cleanup pass would additionally do." Only if the
      user explicitly asks to proceed, continue with steps 3-7 below exactly
      as the no-migrated-record path does — a migrated `work` record does
      not disable or narrow the legacy path in any way, it only changes
      which action this skill recommends first.

3. **Global-archive disclosure guard** (runs regardless of whether step 1's
   probe found a migrated record — `.wip/.archive/` content is orthogonal to
   whether the *current* branch has been migrated): before offering the
   legacy `--purge --dry-run` preview in step 6 below, check whether
   `.wip/.archive/` is non-empty. If it is, tell the user, **before** showing
   that preview:

   > `.wip/.archive/` contains legacy archived content that has not yet been
   > migrated into the work-model store. Running `--purge` now will
   > permanently delete it with only the legacy archive-root-non-empty
   > precondition — none of `work purge`'s per-record protections apply to
   > this content because it isn't a `work` record yet. Consider running
   > `aidw migrate-state --include-global-archive .` first to preserve it as
   > `archived` `work` records (still deletable later via `work purge <id>`,
   > just not lost outright).

4. **If `work list .` returns `[]`** (no migrated record for this repo — a
   pre-`migrate-state` repo, or a fresh clone), fall back to exactly today's
   behavior, unchanged:

   Run a dry run first:

   ```bash
   ~/.claude/ai-dev-workflow/bin/aidw clear-wip . --dry-run
   ```

5. Read the JSON output and show the user what would be **archived** (moved
   into `.wip/.archive/`, not deleted) — the `kept` folder and the `archived`
   list. Ask for confirmation before proceeding.

   On confirmation, run for real:

   ```bash
   ~/.claude/ai-dev-workflow/bin/aidw clear-wip .
   ```

   Report a concise summary: which branch folder was kept, which folders
   were archived. Note that archived folders are recoverable under
   `.wip/.archive/` — nothing was deleted.

6. Only if the user explicitly asks to permanently delete (not just archive)
   the other branch folders — including anything already archived — run
   step 3's disclosure guard, then preview the purge with
   `--purge --dry-run`. **`--dry-run` without `--purge` previews a strictly
   smaller set than a purge deletes**, so never use it as the preview for a
   purge:

```bash
~/.claude/ai-dev-workflow/bin/aidw clear-wip . --purge --dry-run
```

   This preview is read-only and always succeeds, even on a repo where
   nothing has been archived yet.

7. Show the user the **entire** `deleted` list from that preview. It includes
   everything already under `.wip/.archive/` from every prior run, not just
   the folders about to be archived — and each `.archive/...` entry is a whole
   archived branch tree, so the listed name stands for every file inside it.
   Get explicit confirmation that all of it may be destroyed.
8. Only then run the real purge:

```bash
~/.claude/ai-dev-workflow/bin/aidw clear-wip . --purge
```

   `--purge` is the only command in this skill that deletes bytes, and the
   only step here that can be refused. The refusal fires when
   `.wip/.archive/` is completely empty — if it refuses, run the archive pass
   in step 5 first, then re-run this step.

   Note the guard's real scope: it only checks that `.wip/.archive/` is
   non-empty overall. It does **not** guarantee that each folder being purged
   was itself previously archived — a branch folder created after the last
   archive pass is deleted outright. Treat the step 7 confirmation, not the
   guard, as the safety mechanism.

   This legacy chain (steps 4-8), including its real (non-dry-run)
   `--purge`, stays fully reachable on explicit user ask even after step 2's
   `work archive` runs — a migrated `work` record does not disable or narrow
   this path.
