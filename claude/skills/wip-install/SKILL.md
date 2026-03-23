---
name: wip-install
description: Verify the ai-dev-workflow installation and explain how to bootstrap a repo or workspace.
---

When this skill is used:

1. Confirm that `~/.claude/ai-dev-workflow` exists.
2. Confirm that the `wip-*` skills are installed under `~/.claude/skills/`.
3. Confirm that the `wip-*` agents are installed under `~/.claude/agents/`.
4. Confirm that the current repo has `.wip/` and `.claude/repo-docs/`.
5. If the current repo is missing bootstrap files, suggest running:

```bash
~/.claude/ai-dev-workflow/install.sh .
```

Use `~/.claude/ai-dev-workflow/bin/aidw ensure-repo .` when you need to seed the current repo only.

For a full verification:

```bash
~/.claude/ai-dev-workflow/bin/aidw verify
```

6. Check whether RTK is installed:

   ```bash
   which rtk
   ```

   - **If already installed**: confirm it's active and move on.
   - **If not installed**: offer to install it with the following prompt to the user:

     > **RTK** is an optional token compression tool that reduces Claude Code's Bash output by 60-90% — making sessions faster and cheaper. It works by intercepting Bash tool calls and stripping noise from build, test, git, lint, and file commands. It only affects Claude Code's internal Bash tool; it does not modify your shell, `.bashrc`, or `.zshrc`. You can uninstall it at any time with `rtk init -g --uninstall`.
     >
     > **Install RTK? [y/N]**

   If the user says yes, install via Homebrew (preferred) or curl:
   ```bash
   # macOS/Linux with Homebrew:
   brew install rtk && rtk init -g

   # Without Homebrew:
   curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/master/install.sh | sh
   rtk init -g
   ```

   Then write the recommended config if it does not already exist. The config path is OS-dependent — use `rtk config` to confirm the location, or write to:
   - **macOS:** `~/Library/Application Support/rtk/config.toml`
   - **Linux:** `~/.config/rtk/config.toml`

   ```toml
   [tee]
   enabled = true
   mode = "failures"
   max_files = 50
   max_file_size = 10485760
   ```

   Confirm success with `rtk --version` and inform the user that RTK is now active for this Claude Code session. To bypass compression for a specific command, ask the user and use `rtk proxy <cmd>` — there is no config-based exclusion supported for the Claude Code hook.
