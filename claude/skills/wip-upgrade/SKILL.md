---
name: wip-upgrade
description: Re-apply global ai-dev-workflow configs, refresh embedded skills/agents, and migrate legacy .wip directories to the date-prefixed format.
---

When this skill is used:

1. Run the upgrade command for the current repo:

```bash
~/.claude/ai-dev-workflow/bin/aidw upgrade .
```

2. Read the JSON result and report:
   - Which global configs were updated (CLAUDE.md, settings.json, mcp.json, gitignore)
   - Which skills and agents were refreshed from embedded assets
   - Which `.wip/<slug>` directories were renamed to `YYYYMMDD-<slug>` format
   - Any warnings

3. If `.wip` dirs were migrated, remind the user to run `/wip-resume` to pick up the renamed branch folder.

Use this skill when:
- `.wip` files appear to be missing or in the wrong directory
- After pulling updates to `ai-dev-workflow`
- To repair skill/agent symlinks after a fresh install or OS migration
