---
name: wip-cleanup
description: Keep the current branch's .wip folder intact (all files), and archive all other branch folders in .wip/.
---

When this skill is used:

1. Run a dry run first (requires `aidw` on `$PATH`):

```bash
aidw clear-others . --dry-run
```

2. Read the JSON output and show the user what would be **archived** (moved
   into `.wip/.archive/`, not deleted) — the `kept` dir and the `archived`
   list. Ask for confirmation before proceeding.
3. On confirmation, run for real:

```bash
aidw clear-others .
```

4. Report a concise summary: which branch dir was kept, and which other dirs
   were archived. Note that archived dirs are recoverable under
   `.wip/.archive/` — nothing was deleted.
5. Only if the user explicitly asks to permanently delete (not just archive)
   the other branch dirs — including anything already archived — preview the
   purge with `--purge --dry-run`. **`--dry-run` without `--purge` previews a
   strictly smaller set than a purge deletes**, so never use it as the preview
   for a purge:

```bash
aidw clear-others . --purge --dry-run
```

   This preview is read-only and always succeeds, even on a repo where
   nothing has been archived yet.

6. Show the user the **entire** `deleted` list from that preview. It includes
   everything already under `.wip/.archive/` from every prior run, not just
   the dirs about to be archived — and each `.archive/...` entry is a whole
   archived branch tree, so the listed name stands for every file inside it.
   Get explicit confirmation that all of it may be destroyed.
7. Only then run the real purge:

```bash
aidw clear-others . --purge
```

   `--purge` is the only command in this skill that deletes bytes, and the
   only step here that can be refused. The refusal fires when
   `.wip/.archive/` is completely empty — if it refuses, run the archive pass
   in step 3 first, then re-run this step.

   Note the guard's real scope: it only checks that `.wip/.archive/` is
   non-empty overall. It does **not** guarantee that each dir being purged
   was itself previously archived — a branch dir created after the last
   archive pass is deleted outright. Treat the step 6 confirmation, not the
   guard, as the safety mechanism.
