---
name: wip-setup-brew
description: Migrate from a manual (install.sh) installation to the Homebrew-managed version of aidw.
---

When this skill is used:

1. Verify that Homebrew is installed:
   ```bash
   command -v brew
   ```

2. Tap the repository and install the tool:
   ```bash
   brew tap sebGilR/homebrew-tap
   brew install aidw
   ```

3. Locate the environment file. It is typically at:
   `~/.claude/ai-dev-workflow/aidw.env.sh`

4. Comment out the local PATH modification to favor the Homebrew binary:
   - **Search for**: `[[ ":$PATH:" != *":$HOME/.claude/ai-dev-workflow/bin:"* ]] && export PATH="$HOME/.claude/ai-dev-workflow/bin:$PATH"`
   - **Replace with**: `# [[ ":$PATH:" != *":$HOME/.claude/ai-dev-workflow/bin:"* ]] && export PATH="$HOME/.claude/ai-dev-workflow/bin:$PATH"`

5. Verify the migration:
   ```bash
   source ~/.claude/ai-dev-workflow/aidw.env.sh
   which aidw
   aidw version
   ```
   *The path should point to `/opt/homebrew/bin/aidw` or `/usr/local/bin/aidw`.*

6. Clean up the local repository's built binary directory:
   ```bash
   # In the root of the ai-dev-workflow repo:
   rm -rf bin/
   ```

7. Confirm success to the user. Explain that the `aidw` binary is now managed by Homebrew (`brew upgrade aidw`), but the local repository must be kept because it contains the source for skills and agents.
