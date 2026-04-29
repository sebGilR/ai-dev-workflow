---
name: wip-document-project
description: Perform a deep research pass on the repo to generate high-fidelity documentation in .claude/repo-docs/.
agent: analyst
---

When this skill is used:

1. **Skeleton Pass**: Ensure documentation skeletons exist. Run:
   ```bash
   ~/.claude/ai-dev-workflow/bin/aidw document-project .
   ```

2. **Intelligence Pass**: Use the `wip-analyst` subagent to:
   - Identify the primary entry points and architectural pillars.
   - Describe the data flow and core components.
   - Extract and document error handling, testing, and API/CLI patterns.
   - Identify any "gotchas" or environment requirements.

3. **Finalize**: Update `architecture.md`, `patterns.md`, and `gotchas.md` in `.claude/repo-docs/` with the discovered insights.

4. **Index**: Refresh the semantic memory index:
   ```bash
   ~/.claude/ai-dev-workflow/bin/aidw memory index . .claude/repo-docs/
   ```

5. Summarize the documentation created and invite the user to review.
