---
name: wip-cleanup
description: Keep the current branch's .wip folder intact (all files), and delete all other branch folders in .wip/.
---

When this skill is used:

1. Run (requires `aidw` on `$PATH`):

```bash
aidw clear-others .
```

2. Read the JSON output to confirm what was deleted and what was kept.
3. Report a concise summary: which branch dir was kept, and which other dirs were removed.
