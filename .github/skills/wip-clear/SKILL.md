---
name: wip-clear
description: Delete all .wip branch folders except the most recently dated one.
---

When this skill is used:

1. Run a dry run first:

```bash
~/.claude/ai-dev-workflow/bin/aidw clear-wip . --dry-run
```

2. Read the JSON output and show the user what would be **archived** (moved
   into `.wip/.archive/`, not deleted) — the `kept` folder and the `archived`
   list. Ask for confirmation before proceeding.
3. On confirmation, run for real:

```bash
~/.claude/ai-dev-workflow/bin/aidw clear-wip .
```

4. Report a concise summary: which branch folder was kept, which folders
   were archived. Note that archived folders are recoverable under
   `.wip/.archive/` — nothing was deleted.
5. Only if the user explicitly asks to permanently delete (not just archive)
   the other branch folders — including anything already archived — run:

```bash
~/.claude/ai-dev-workflow/bin/aidw clear-wip . --purge
```

   `--purge` is the only command in this skill that deletes bytes; always
   preview (`--dry-run`) and get explicit confirmation before running it.
