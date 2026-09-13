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

## Freeform mode (only when explicitly requested)

Do NOT run steps 1-7 above for this mode. Follow this section instead
when the user's request explicitly asks for freeform, lightweight, or
"scratch" tracking with no bootstrap (for example, invoking this
skill as `/wip-start freeform`, or wording like "track this without
the full bootstrap" / "just start a lightweight work record"). Absent
an explicit ask like that, always use the delivery-mode steps above —
this section is opt-in, never the default.

1. Detect the current repo root.
2. Run:

   ```bash
   ~/.claude/ai-dev-workflow/bin/aidw work start --mode freeform --title "<short task title>" .
   ```

   This creates a `work.json` + `context.md` only (`work/<id>/`),
   tagged `mode: "freeform"`. It never invokes the legacy delivery
   bootstrap sequence that steps 1-7 above use — no repo-docs deep
   research pass, no semantic memory embedding/indexing, no
   `.github`/`GEMINI.md` seeding, no gitignore edits happen on this
   path.
3. Read the printed record's `work_id` field and summarize that a
   freeform work record was started.
4. Mention that `aidw work promote <id> --mode delivery` can upgrade
   this record to full delivery mode later (materializing
   spec/plan/review/etc under `work/<id>/attachments/`) if the task
   turns out to need the full workflow.
5. Continue the conversation from the freeform record — do not
   proceed to steps 1-7 above.
