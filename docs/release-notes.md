# Release notes

## Unreleased — Phase 1 findings-gap fixes (Clusters A–E)

This release changes behavior that only reaches an existing installation
through a re-bootstrap: **run `aidw upgrade` (or re-run `install.sh`) after
updating the binary.** Everything below lives in `~/.claude/` (skills,
agents, hook scripts, `settings.json`) or `~/.claude/aidw.env.sh`, which
are extracted/merged from the embedded templates only on install/upgrade —
an old binary keeps running old hook scripts and old skill copy until you
do this.

### Adversarial review is now off by default, and gated by a permission prompt instead of an env var

- Fresh installs now write `AIDW_ADVERSARIAL_REVIEW="0"` (previously `"1"`
  on some install paths, inconsistently — Go template said `gemini`,
  `install.sh` said `codex`/`gemini-2.5-ultra`).
- **`install.sh`'s upgrade path now actively rewrites an existing
  `AIDW_ADVERSARIAL_REVIEW="1"` in `~/.claude/aidw.env.sh` to `"0"`** —
  this is a real behavior change if you relied on the old default-on
  offer inside `/wip-review`. `aidw adversarial-review .` still works;
  it's just no longer offered automatically.
- Known gap: **`aidw upgrade` alone does not perform this flip.**
  `writeEnvFile`'s existing-file branch is a no-op when
  `~/.claude/aidw.env.sh` already exists — only re-running `install.sh`
  rewrites an existing `="1"` to `="0"`. If you only ever run
  `aidw upgrade`, a legacy `AIDW_ADVERSARIAL_REVIEW="1"` will linger
  (inert for `/wip-review`'s removed offer, but still read by the
  deprecated `gemini-review` command). Tracked as a follow-up; the fix is
  to re-run `install.sh` if you need the flip today.
- The `AdversarialReview` env gate is gone from `aidw adversarial-review`
  itself — direct invocation is now the opt-in, backstopped by a new
  `permissions.ask` entry in `settings.template.json` for
  `aidw adversarial-review*` / `aidw gemini-review*` (only reaches your
  `~/.claude/settings.json` via the settings merge that `aidw upgrade`
  performs).

### Hooks and skills that only take effect after a re-bootstrap

- `~/.claude/save-wip-snapshot.sh` (Stop/PreCompact/SessionEnd) is
  rewritten: it is now a true no-op outside a git repo or with no active
  `.wip` work for the branch (no more spontaneous
  `.wip/<date>-detached-head` dirs), resolves the branch dir via a new
  `aidw resolve-wip` lookup with a corrected, ERE-escaped shell fallback,
  and triggers a best-effort `aidw summarize-context` refresh on
  precompact/stop/sessionend.
- A new `~/.claude/session-start-context.sh` hook (SessionStart, matchers
  `compact`/`resume`) is registered in `settings.template.json`.
- `wip-resume`, `wip-review`, `wip-cleanup`, `wip-clear`, `wip-plan`,
  `wip-implement`, `wip-auto` skill copy changed — an old `~/.claude/skills/`
  tree still shows the previous (pre-fix) instructions until the skills
  are re-extracted.

### `cleanup-branch` / `clear-wip` / `clear-others` now archive, not delete

- These commands move condemned files into `archive/<timestamp>/` (or
  `.wip/.archive/<name>/`) instead of deleting them. Existing automation
  that assumed immediate deletion should switch to the new `--purge` flag
  for that behavior — `--purge` previews first and deletes both current
  non-kept entries and anything already archived.

### `--source-path` (dev-mode) installs

If you installed with `--source-path` (symlinked skills/agents from a
checkout instead of the embedded copy), the managed hook scripts
(`save-wip-snapshot.sh`, `session-start-context.sh`) are **not** currently
symlinked by that path — only skills/agents are. You'll need a normal
`aidw upgrade`/`install.sh` run (or a manual copy) to pick up the hook
script changes in this release.
