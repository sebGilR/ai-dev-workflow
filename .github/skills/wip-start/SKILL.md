---
name: wip-start
description: Initialize the branch-scoped .wip/<branch>/ folder and seed all workflow files.
---

When this skill is used:

1. Detect the current repo root.
2. Run:

```bash
~/.claude/ai-dev-workflow/bin/aidw start .
```

3. Read the resulting `status.json` and `context.md`.
4. **Project Intelligence (JIT)**:
   - If `.claude/repo-docs/` is empty or missing, run `/wip-document-project` to perform a deep research pass and generate core documentation.
   - Otherwise, refresh the semantic memory index:
     ```bash
     ~/.claude/ai-dev-workflow/bin/aidw memory index . .claude/repo-docs/
     ```
5. Summarize what was initialized and the project intelligence status.
6. Suggest running `/wip-plan` to begin the spec-driven planning sequence (Clarify -> Draft -> Skeptic Review).
7. Continue the conversation from the initialized workflow state.
