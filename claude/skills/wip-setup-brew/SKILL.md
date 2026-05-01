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

3. Run the new bootstrap command to refresh the environment and use the Homebrew binary:
   ```bash
   aidw bootstrap --setup-shell --interactive .
   ```

4. Verify the migration:
   ```bash
   which aidw
   aidw version
   ```
   *The path should point to `/opt/homebrew/bin/aidw` or `/usr/local/bin/aidw`.*

5. Clean up the old manual installation artifacts:
   - If the user had a local clone, they can remove the `bin/` directory within it.
   - If they want to switch to a fully "cloneless" setup, they can delete the local repository entirely *after* confirming the Homebrew version is working (the `bootstrap` command above extracted the skills/agents into `~/.claude`).

6. Confirm success to the user. Explain that the `aidw` binary is now managed by Homebrew (`brew upgrade aidw`).
