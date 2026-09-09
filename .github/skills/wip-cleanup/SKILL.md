---
name: wip-cleanup
description: Keep the current branch's .wip folder intact (all files), and delete all other branch folders in .wip/.
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
   the other branch dirs — including anything already archived — run:

```bash
aidw clear-others . --purge
```

   `--purge` is the only command in this skill that deletes bytes; always
   preview (`--dry-run`) and get explicit confirmation before running it.
