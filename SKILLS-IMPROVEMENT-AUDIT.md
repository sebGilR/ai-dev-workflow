# Skills & Workflows Improvement Audit

**Date:** 2026-06-17
**Scope:** Custom Claude Code skills/workflows used on the Benvel Shopify build — `wip-*` (ai-dev-workflow), `shopify-*` (ai-shopify-workflow), `bmad-*` + the parallel orchestrator, and `code-first-shopify-ui-implement`.
**Method:** Mined the operator's learned-feedback memory (24 files), ~106 MB of session transcripts (grep-only), BMAD retrospectives, QA results, `.wip` execution logs, and the live skill/agent/gate definitions. Every non-trivial claim cites the source.

---

## 1. Executive summary — top 7 highest-leverage changes

1. **Replace `shopify theme check` with `shopify theme push --development` as the authoritative compile gate, everywhere.** Theme Check is a lenient linter that passed 3 non-compiling sections straight to `main` in Epic 22, and missed `include`-in-`render` and `templates/product.json` block-order gaps in Epic 4. This is the single most repeated, most damaging gap. It must be baked into `.bmad/orchestration/gates.yml`, the QA agent, and `code-first-shopify-ui-implement` Step 7. **(Effort: M)**
   - Evidence: `memory/project_theme_check_misses_compile_errors.md`; `retrospective-2026-05-17.md:62,116`; `.bmad/orchestration/gates.yml` (only `theme check` + `prettier --check`); `code-first-shopify-ui-implement/SKILL.md` Step 7 says "Run `shopify theme check`".

2. **Fix the broken prettier gate.** `gates.yml` runs `prettier --check "shopify-theme/**/*.liquid"` but `@shopify/prettier-plugin-liquid` is **not installed** (not in `node_modules`, not in `package.json`). The gate is either silently no-op'ing or failing on every run. Either install the plugin or remove the gate; don't ship a phantom check. **(Effort: S)**
   - Evidence: `.bmad/orchestration/gates.yml:8`; `ls node_modules/@shopify/prettier-plugin-liquid` → absent; 76 transcript mentions of `prettier-plugin-liquid`; PR #154 follow-up list (`memory/project_epic_22_pdp_parity.md`) explicitly says "install `@shopify/prettier-plugin-liquid`".

3. **Build a `theme-verify` helper script** that bundles the recurring manual ritual: `theme push --development --json` → capture preview URL → fetch with `?preview_theme_id=<id>` to bypass Shopify's edge cache → report compile errors + console errors. This ritual is performed constantly by hand (227 `preview_theme_id` mentions, 351 `theme push` mentions across transcripts). **(Effort: M)**

4. **Encode the recurring Shopify gotchas as grep-able lint guards** in a `shopify-liquid-guards` script the gates call: (a) single-line `comment … endcomment` inside `{% liquid %}`, (b) nested `{% stylesheet %}`/`{% javascript %}`, (c) `{% include %}` inside `{% render %}` scope, (d) closed overlays hidden by `transform` alone (reduce-motion stuck-open bug), (e) `document.currentScript` in `<script type="module">`, (f) `money_with_currency | replace: '.00'` COP corruption. Each one shipped a real defect; none is caught by Theme Check. **(Effort: M)**
   - Evidence: `memory/project_theme_check_misses_compile_errors.md` (a,b,d); `retrospective-2026-05-17.md:116` (c); `memory/project_epic_22_pdp_parity.md` D1 (e); transcripts (34 `money_with_currency` hits, all describing the `.00` COP-corruption bug, f).

5. **Promote the operator's hard-won BMAD-orchestrator operational rules from memory into the skill/agent files**, so a fresh context doesn't re-learn them: control-tower-must-be-detached, the `sprint-status.yaml merge=ours` driver, "re-run `scan` after any overlay edit", sprint-status-KEY≠filename `cp` step, and the `BMAD_EXTRA_NON_CONFLICTING_SCOPES=locale` lever. These are durable mechanics currently living only in two memory files. **(Effort: M)**
   - Evidence: `memory/project_orchestrator_merge_mechanics.md`, `memory/project_epic_22_pdp_parity.md` "RUN GOTCHAS".

6. **Fix the implementer/QA agent contract that assumes it can spawn the QA subagent — it can't.** The `Agent` tool is unavailable inside subagent contexts, so every dispatch prompt's "spawn QA synchronously" step silently fails and the implementer returns Template B. Rewrite the contract to emit a structured self-review summary on Agent-unavailable, and move QA spawning to the parent orchestrator. **(Effort: M)**
   - Evidence: `memory/feedback_subagent_cannot_spawn_qa.md` (observed Story 3.6, 2026-05-14); `.claude/agents/bmad-shopify-theme-implementer.md` Section 8.

7. **Add a runtime/visual QA pass that catches what static QA structurally cannot** — and run it WITH `prefers-reduced-motion: reduce` emulated. The `bmad-shopify-theme-qa` agent is path-anchored to the diff and the staff fixture page; it shipped 2/12 PDP primitives invisible because nothing checked the live `templates/product.json`, and Epic 22's visual QA missed the stuck-open drawer for lacking reduce-motion emulation. **(Effort: M)**
   - Evidence: `retrospective-2026-05-17.md:62`; `memory/project_theme_check_misses_compile_errors.md` ("its browser had no reduce-motion emulation").

---

## 2. Cross-cutting themes

### 2a. Prescriptiveness gaps — the operator had to correct the same ambiguity repeatedly
The learned-feedback memory is a direct ledger of where a skill was vague enough to pick the wrong path. Eleven `feedback_*` files each encode a correction the operator had to give and re-give. The pattern: BMAD orchestration skills described an *intended* behavior (spawn QA, stop for approval, open separate windows) that didn't match the operator's *actual* desired behavior (self-review + merge, no per-story gates, in-session background subagents). Every one of these should be folded back into the skill/agent text so it isn't re-learned each session.
- Evidence: `memory/feedback_dispatch_mode_subagents.md`, `feedback_no_approval_gates.md`, `feedback_epic_end_review_only.md`, `feedback_subagent_cannot_spawn_qa.md`, `feedback_orchestrator_story_file_gate.md`, `feedback_bmad_parallel_run_handles_all_steps.md`, `feedback_git_autopilot_epic_branches.md`.

### 2b. Scripting opportunities — recurring multi-step rituals done by hand
The highest-frequency manual rituals in the transcripts are all scriptable:
- **`theme push --development` + `?preview_theme_id` cache-bust + verify** (227 + 351 hits). → `theme-verify` script (§4).
- **Liquid compile/runtime guards** (the 6 gotchas in §1.4). → `shopify-liquid-guards` script (§4).
- **Orchestrator merge ritual**: detach → `merge=ours` setup → `cp` key-named story file → `merge-story --story-branch` → `release` → re-`scan`. Currently a 6-step manual dance recorded only in memory. → `bmad-merge-story-safe` wrapper (§4).
- **Live deploy**: `shopify theme push --live --allow-live --path shopify-theme` — the bare `--live` form fails non-interactively and had to be corrected. → a `theme-deploy-live` alias/script (§4).
  - Evidence: `memory/project_live_theme_push_command.md`.

### 2c. Tooling gaps
- **No authoritative compile gate** (the central finding — §1.1).
- **Prettier Liquid plugin missing** (§1.2).
- **`cross_repo_gate` was specified in two retros but never wired into a CI guard or orchestrator scan command** — so cross-repo BU blockers (Story 1.11 sat 5 days; Epics 10/11 amplified it) stayed invisible. The mechanic exists as a sprint-status comment convention only.
  - Evidence: `epic-1-retro-2026-05-11.md:37`, `retrospective-2026-05-17.md:133`.

### 2d. Tool-selection — mostly healthy, one stale instruction
- **Serena vs Explore**: clean. 0 `Explore` subagent invocations in transcripts; 60 `mcp__serena` + 64 `codegraph_` calls. The global rule banning Explore is being honored. The 3 "use Serena" mentions are the rule text itself, not corrections.
- **RTK**: heavily used (2021 mentions) and working as intended for large outputs.
- **Stale doc**: `docs/agentic-bmad-parallel-orchestrator.md:404-406` still says "Paste the emitted prompt into a new Claude Code window" — directly contradicted by `feedback_dispatch_mode_subagents.md` (background subagents are the default; windows are fallback-only). The doc's §10 example needs updating. **(Effort: S)**

---

## 3. Per-family findings

### A. `wip-*` family (ai-dev-workflow)

**A1. The `wip-*` family is barely used on this repo and overlaps heavily with BMAD.** 38 `.wip/` branch dirs exist, but the actual product build ran almost entirely through BMAD + the parallel orchestrator and ad-hoc `fix/*` branches. The Skill-tool invocation counts (wip-plan/implement/review = 2 each) are low, and the `status.json` `stage` field is empty across nearly every branch dir — suggesting the `aidw` stage machine wasn't driven to completion in most sessions.
- Evidence: `ls .wip/2026*/` = 38 dirs; `grep '"stage"' */status.json` returns blank for ~all; Skill-tool grep shows wip skills invoked 1-2× each.
- **Proposed change:** Don't expand `wip-*`. Document explicitly (in the global CLAUDE.md managed block) that on BMAD repos, BMAD owns the multi-step flow and `wip-*` is for ad-hoc non-BMAD fixes only — which the project CLAUDE.md *almost* says but the global block presents `wip-*` as "the default workflow for code tasks." That framing causes the two to compete. **(Effort: S)**

**A2. `wip-review`'s model selection is stale.** It hard-codes `claude-opus-4.6` / `claude-sonnet-4.6` and asks "Escalate to Opus 4.6?". The operator moved the whole BMAD system to Opus 4.7 on 2026-05-15 and is now on Opus 4.8. The version-pinned prompt text will keep drifting.
- Evidence: `wip-review/SKILL.md:30-37`; `memory/feedback_model_policy_orchestrator.md`.
- **Proposed change:** Reference models by alias/tier ("the deeper-analysis model"), not pinned version strings. **(Effort: S)**

**A3. `wip-implement` / `wip-auto` have a good `aidw policy check` safety gate but no Shopify-awareness.** For a Shopify theme repo, "self-review against AC" (Step 3) should invoke the §1.1 compile gate, not generic tests. As written, an implementer can mark a Liquid task "done" with zero runtime validation.
- Evidence: `wip-implement/SKILL.md` Step 3; cross-referenced with the theme-check gap.
- **Proposed change:** Add a repo-type hook so on theme repos the self-review pass runs `theme push --development`. **(Effort: M)**

**A4. Solid parts worth keeping:** the `aidw verify-wip-file` artifact-verification gates in `wip-plan`/`wip-research`, the `wip-resume` `context-summary` short-circuit, and the explicit "use `aidw start .` to resolve `wip_dir`, never infer the path" instruction in `wip-research`. These are well-prescribed and should be the model for the looser skills.

### B. `shopify-*` family (ai-shopify-workflow)

**B1. The seven `shopify-*` skills (classify, content, products, scaffold, setup, ux-check, upgrade) show essentially no friction in transcripts** — they're thin, read-only/CRUD-oriented, and weren't a source of corrections. No memory file flags them. Leave them alone.

**B2. `shopify-setup` / the family carry no guidance on the COP / `money_with_currency` trap or the `theme push` deploy forms** — these are store-specific and currently live only in benvel memory. That's the correct boundary (store-specific → theme/BU repo, per the three-repo model), so the fix belongs in the benvel `code-first` skill (§D), not here. Flagging only to confirm the boundary is right. **(No change to shopify-* skills.)**

### C. BMAD + parallel orchestrator

**C1. `gates.yml` is the weakest link (see §1.1 + §1.2).** It runs a lenient linter and a broken prettier check, and nothing else. Add `shopify theme push --development --json` (authoritative compile), fix or drop prettier, and add the `shopify-liquid-guards` grep checks. **(Effort: M)**
- Evidence: `.bmad/orchestration/gates.yml` (full contents: 2 commands, one broken).

**C2. QA agent is path-anchored and misses aggregate/runtime defects.** `bmad-shopify-theme-qa.md` runs Theme Check on the diff and smokes the staff fixture page, but never pulls the live `templates/product.json` to confirm a block actually wired in, never renders snippets in their real consumer scope, and never emulates reduce-motion. Result documented in two retros: 2/12 PDP primitives shipped invisible; `include`-in-`render` runtime break passed QA.
- Evidence: `retrospective-2026-05-17.md:62,116,140`; `.claude/agents/bmad-shopify-theme-qa.md:106` (only `theme check`).
- **Proposed change:** Add to the QA checklist: (1) for any story adding a PDP/template block, grep `templates/<surface>.json` `block_order` for the block post-merge; (2) "controversial-choice smoke" — any `Decisions Consulted` deviation gets a runtime render, not just a lint (this exact action item was written in `retrospective-2026-05-17.md:140` but isn't in the agent file). **(Effort: M)**

**C3. Implementer→QA spawn assumption is false (see §1.6).** **(Effort: M)**

**C4. Story-file shape gate landed but the orchestrator's hardest operational knowledge lives only in memory (see §1.5).** The merge mechanics (`detached control tower`, `merge=ours` driver, `scan`-after-overlay-edit, KEY≠filename `cp`, `BMAD_EXTRA_NON_CONFLICTING_SCOPES`) are durable, repo-stable mechanics that every fresh orchestrator session re-derives painfully. They belong in `docs/agentic-bmad-parallel-orchestrator.md` §9 (Failure handling) and ideally as guardrails inside `scripts/bmad-orchestrator merge-story` itself (auto-install the `merge=ours` driver; auto-detect-and-refuse if the control tower has the epic checked out; auto-`cp` the KEY-named file).
- Evidence: `memory/project_orchestrator_merge_mechanics.md`, `memory/project_epic_22_pdp_parity.md`.
- **Proposed change:** Bake the three deterministic ones (merge=ours install, detached-check, key-file cp) into the `merge-story` command. Document the rest. **(Effort: M)**

**C5. `cross_repo_gate` never got wired (see §2c).** Add a `bmad-orchestrator cross-repo-status` scan that reports stories blocked on a BU PR, so the gap that bit Epics 1/10/11 stops repeating. **(Effort: M)**

**C6. The orchestrator manual contradicts the operator's settled workflow.** `docs/agentic-bmad-parallel-orchestrator.md` §10 still instructs pasting prompts into new windows; §8 / approval-gate sections were updated but the example wasn't. Reconcile with `feedback_dispatch_mode_subagents.md` + `feedback_no_approval_gates.md`. **(Effort: S)**

**C7. Redundancy/dead weight — the Pencil path.** Four `bmad-pencil-design-*` skills + the `OPT-PDG/PDA/PDR/PDC` agent menu entries are explicitly "optional/historical" (project CLAUDE.md, `design/pencil/STATUS.md` empty). They add 4 skills to the menu and recurring "do not run Pencil" anti-pattern guards in `code-first-shopify-ui-implement`. **Recommend: archive the Pencil skills out of the active skill set** (move to a `bmad-pencil/` opt-in plugin or delete), and drop the defensive "refuse Pencil" anti-pattern lines once they're gone. **(Effort: S)**
- Evidence: project CLAUDE.md "Pencil is optional / historical"; `code-first-shopify-ui-implement/SKILL.md` Rules + Anti-patterns both spend lines refusing Pencil.

### D. `code-first-shopify-ui-implement`

**D1. Step 7 validation is wrong (the central finding localized here).** It says "Run `shopify theme check` if available. Open `shopify theme dev` for visual review." Theme Check is not a compile gate, and `theme dev` hot-reload doesn't surface the server-side compile errors that `theme push --development` does. Replace Step 7 with the `theme-verify` ritual: `theme push --development --json` → verify 0 errors → visual review via `?preview_theme_id` (cache-bypassed) at 375×812 and 1280×800, **with reduce-motion emulated**.
- Evidence: `code-first-shopify-ui-implement/SKILL.md` Step 7; `memory/project_theme_check_misses_compile_errors.md`.
- **(Effort: S — text change; depends on §4 script.)**

**D2. The skill's "refuse" rules are good but missing the store-specific traps.** It already encodes PROFECO sale-display, `pm-42` low-stock, photography-never-rounded, cart-drawer-on-mobile. **Add:** (a) garantía legal / Ley 1480 must render on-page (a collapsed accordion is fine; a policy-link-only is a SIC-fineable defect — this is live regulatory exposure currently in the `fix/pdp` branch); (b) COP money must not run `| replace: '.00'`; (c) closed overlays hidden by `visibility`/`pointer-events`, never `transform` alone.
- Evidence: `memory/project_pdp_ley1480_garantia_legal.md` (the `fix/pdp` branch set the SIC block to `display:none` — the precise pattern SIC fines), `memory/project_theme_check_misses_compile_errors.md`.
- **(Effort: S)**

**D3. The skill assumes design contracts may not exist and offers to stop-and-ask or skip.** Given the operator's standing autonomy preference (`feedback_autonomy_defaults_cross_repo.md` — "continue with defaults, don't block on me"), the default-if-no-contract behavior should be "create from `_template.md` and note it," not "stop and ask Sebastian." **(Effort: S)**

---

## 4. Proposed new scripts / helper tooling

| Script | Automates | Replaces manual ritual | Lives in | Effort |
|---|---|---|---|---|
| `theme-verify` | `shopify theme push --development --json` → parse for 0 errors → emit preview URL with `?preview_theme_id=<id>` for cache-bypassed fetch; optional console-error check | The 227×/351× hand-run push+cache-bust loop; the wrong `theme check` gate | `ews-bu-benvel-theme/scripts/` (called by gates + code-first Step 7) | M |
| `shopify-liquid-guards` | greps for the 6 Theme-Check-invisible anti-patterns (single-line comment-in-liquid, nested stylesheet, include-in-render, transform-only overlay hide, currentScript-in-module, money `.00` replace) | Repeated post-hoc debugging of shipped defects | same `scripts/`; add to `gates.yml independent:` | M |
| `theme-deploy-live` | wraps `shopify theme push --live --allow-live --path shopify-theme --store benvel-2.myshopify.com` with a commit-first check + intent confirm | The bare-`--live`-fails-non-interactively correction | `scripts/` | S |
| `bmad-merge-story-safe` (or fold into `bmad-orchestrator merge-story`) | auto-installs `merge=ours` driver for sprint-status.yaml; refuses if epic branch is checked out in control tower; auto-`cp`s KEY-named story file into worktree | The 6-step manual merge dance in `project_orchestrator_merge_mechanics.md` | `scripts/bmad-orchestrator` | M |
| `bmad-orchestrator cross-repo-status` | scans sprint-status for `cross_repo_gate` stories blocked on a BU PR | The invisible-BU-blocker gap (Epics 1/10/11) | `scripts/bmad-orchestrator` | M |
| `prettier-liquid setup` (one-time) | `npm i -D @shopify/prettier-plugin-liquid` + `.prettierrc` wire-up, or removal of the dead gate | The phantom prettier gate | repo + `gates.yml` | S |

---

## 5. Prioritized action plan

### Quick wins (S, do first)
1. **Install `@shopify/prettier-plugin-liquid` or delete the dead gate** in `gates.yml`. (§1.2)
2. **Swap Step 7 of `code-first-shopify-ui-implement`** from `theme check` to the `theme-verify` ritual; add reduce-motion to the visual-review line. (§D1)
3. **Add the 3 store-specific refuse-rules** (Ley 1480 on-page, COP no-`.00`-replace, overlay-hide-via-visibility) to `code-first-shopify-ui-implement`. (§D2)
4. **Update `docs/agentic-bmad-parallel-orchestrator.md` §10** to say background subagents, not new windows; reconcile approval-gate language. (§C6)
5. **De-version `wip-review` model selection.** (§A2)
6. **Archive the 4 Pencil skills** out of the active menu. (§C7)
7. **Clarify wip-vs-BMAD ownership** in the global CLAUDE.md managed block. (§A1)

### Medium reworks (M, do next)
8. **Build `theme-verify` + `shopify-liquid-guards` scripts** and add both to `gates.yml independent:`. This is the highest-impact change — it closes the compile-gap that shipped to `main` twice. (§1.1, §1.3, §1.4)
9. **Rewrite the implementer/QA agent contract** for Agent-tool-unavailable (structured self-review summary; QA spawned by parent). (§1.6, §C3)
10. **Harden the QA agent checklist:** live `templates/product.json` block-order check + controversial-choice runtime smoke (already an unimplemented retro action item). (§C2)
11. **Bake the deterministic merge mechanics into `bmad-orchestrator merge-story`** (auto `merge=ours`, detached-check, key-file cp). (§1.5, §C4)
12. **Wire `cross_repo_gate` into a real scan command.** (§C5)
13. **Add a Shopify-aware self-review hook to `wip-implement`/`wip-auto`** so theme tasks run the compile gate. (§A3)

### Larger / structural (L, schedule)
14. **A runtime/visual-QA standard** that runs at epic-end with reduce-motion emulation and walks the live template (not just the staff fixture). The `bmad-visual-qa-bg` agent exists and worked on Epic 22 — formalize reduce-motion emulation + console/network capture as required, not optional. (§1.7)

---

## Appendix — evidence index

- **Memory (operator corrections):** `~/.claude/projects/-Users-…-benvel-theme/memory/` — `feedback_{dispatch_mode_subagents,no_approval_gates,epic_end_review_only,subagent_cannot_spawn_qa,orchestrator_story_file_gate,bmad_parallel_run_handles_all_steps,git_autopilot_epic_branches,model_policy_orchestrator,autonomy_defaults_cross_repo,locale_default_baseline,rate_limit_handling}.md`; `project_{theme_check_misses_compile_errors,orchestrator_merge_mechanics,epic_22_pdp_parity,epic_22_autonomous_completion,live_theme_push_command,bu_metafield_infra_shipped,reviews_data_gap,pdp_ley1480_garantia_legal}.md`.
- **Retros:** `ews-bu-benvel-theme/_bmad-output/implementation-artifacts/epic-1-retro-2026-05-11.md`, `retrospective-2026-05-17.md`.
- **Gates / agents:** `.bmad/orchestration/gates.yml`, `.claude/agents/bmad-shopify-theme-{implementer,qa}.md`.
- **Skills:** `ai-dev-workflow/claude/skills/wip-*/SKILL.md`, `ews-bu-benvel-theme/.claude/skills/code-first-shopify-ui-implement/SKILL.md`, `docs/agentic-bmad-parallel-orchestrator.md`.
- **Transcript frequency (grep, ~106 MB):** `theme push`=351, `preview_theme_id`=227, `reduced-motion`=525, `prettier-plugin-liquid`=76, `money_with_currency`=34, `Explore` subagent=0, `mcp__serena`=60, `codegraph_`=64, `rtk`=2021.
