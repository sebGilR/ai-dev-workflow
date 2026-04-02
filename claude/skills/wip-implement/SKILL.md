---
name: wip-implement
description: Implement the next planned chunk of work and update execution notes.
hooks:
  Stop:
    - type: command
      command: ~/.claude/ai-dev-workflow/bin/aidw summarize-context .
---

When this skill is used:

1. Read the current `plan.md`, `research.md`, `context.md`, and `status.json`.
   When implementing against external libraries or APIs, use Context7 to retrieve
   current documentation before writing integration code.
   If Serena is available, use it (`mcp__serena__find_symbol`, `mcp__serena__get_symbols_overview`,
   `mcp__serena__find_referencing_symbols`) to navigate to relevant symbols and
   understand existing patterns before writing new code.
2. Implement the next chunk of work.
3. Append a concise update to `execution.md` describing what changed and why.
4. Optionally refresh `context.md` if the implementation materially changed the continuation context.
5. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw set-stage . implementing
```

6. Summarize completed work and remaining work.

## RTK Usage (Token Compression)

When RTK is installed (`rtk init -g`), Bash commands are automatically compressed. For implementation work, this is especially valuable on build/test output:

**Happy path — use RTK:**
- `rtk cargo build` / `rtk cargo clippy` — 80% reduction on Rust build output
- `rtk cargo test` — failures only (-90%)
- `rtk lint` / `rtk tsc` — errors grouped by file
- `rtk git diff` — condensed diff

**On failure — ask permission to bypass RTK:**

When compressed output is insufficient to diagnose a failure, ask the user first:

> "I need full output from `<cmd>` to [specific reason]. Run without RTK compression? [y/N]"

If the user confirms, use:
```bash
rtk proxy <failing-command> 2>&1
```
This gives full raw output (passthrough — no compression applied). Do not bypass silently.

**Capturing failure logs to the branch context:**

For persistent debugging context, dump the raw output to the branch `logs/` directory. First look up the actual wip directory from `status.json` (`wip_dir` field), then:
```bash
mkdir -p <wip_dir>/logs
rtk proxy <failing-command> 2>&1 | tee <wip_dir>/logs/$(date -u +"%Y%m%d%H%M%S")-<cmd>.log
```
Replace `<wip_dir>` with the date-prefixed path from `status.json` (e.g., `.wip/20260322-my-feature/`). The `logs/` directory is automatically cleaned up by `/wip-cleanup` — no manual deletion needed.

**Automatic failure capture (RTK config):**

If RTK's config has `[tee] mode = "failures"`, RTK already saves raw output on non-zero exit codes to its own tee directory (location varies by OS — check `rtk config` for the path). Reference that as a fallback when the agent needs the full output of the previous failed command.
