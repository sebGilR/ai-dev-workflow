# Handoff: findings-gap audit, Phase 2 continuation (Cluster H next)

Written 2026-09-12 at the end of the session that shipped PR #51. For a
fresh Claude Code session (or a human) picking this up cold — everything
you need to continue without re-deriving context is below.

## Where things stand

**Merged to `main`** (PR #51, merge commit `6ab4dde0`, CI green):
- **Phase 1** (Clusters A–E): `.wip` resolver lookup-vs-create split,
  archive-based cleanup, adversarial-review unwiring, model-hint frontmatter,
  mirror regen + CI.
- **Phase 2 / Cluster F**: `docs/design/work-model.md` — the approved design
  contract for the work-model store. **Read this file first** — everything
  below assumes it.
- **Phase 2 / Cluster G**: `cmd/aidw/internal/state` + `cmd/aidw/internal/work`
  packages, `aidw work start|list|status|attach|bind-session|checkpoint`
  commands, hook integration.

**Source spec** (the original backlog, still authoritative for scope):
`.wip/20260909100540-chore-external-findings-gaps-audit-f656ffd0/spec.md`.
This file is gitignored (`.wip/` is never committed) — it exists only in
this working tree. If you're in a fresh clone or worktree without it,
the design doc (`docs/design/work-model.md`, which IS committed) has
enough detail to proceed; the backlog spec is extra texture, not required.

**Not started:** Clusters H, I (rest of Phase 2), Cluster J (Phase 3).

**Local branches** (all fully merged into `origin/main`, safe to delete but
left alone pending your go-ahead — this doc's author didn't want to delete
branches without asking): `chore/external-findings-gaps-audit`,
`feat/work-model-design`, `feat/work-model-store-resolver`. To clean up:
```
git branch -d chore/external-findings-gaps-audit feat/work-model-design feat/work-model-store-resolver
git push origin --delete chore/external-findings-gaps-audit feat/work-model-design feat/work-model-store-resolver
```

## What's next: Cluster H (migration + memory re-key)

From the backlog spec (line ~344):

- **H1** — `aidw migrate-state`: inventory `git worktree list` + all `.wip`
  dirs; copy records **and all files** (including attachments this design
  has no named field for — copy as opaque blobs) into `work/<id>/`; verify
  by checksum before declaring success; write an old-path→work_id mapping;
  keep legacy `.wip` lookup **read-through** (functional, not just present)
  indefinitely; NEVER delete sources — removal only via a separate explicit
  `aidw migrate-state --cleanup-sources` with a preview-then-confirm step
  (reuse the exact UX Phase 1 built for `--purge` — preview the full
  deletion set, require confirmation, refuse if there's nothing to clean up).
  Divergent copies (source and destination disagree after what should be an
  idempotent copy) are surfaced for manual resolution, never auto-picked.
- **H2** — Memory re-key: migrate `cmd/aidw/cmd/memory.go`'s facts and
  doc-index rows from repo+branch keys to `work_id`/`repo_id` keys in the
  global SQLite store. Keep a compatibility view or dual-read until the
  migration is confirmed, then drop it.

**⚠️ The backlog spec's H2 description is now stale.** It says
"`memory.go:44,146,218` currently ensure branch state — switch to lookup +
work-ID keys," but Phase 1's Cluster B fix already changed `memory.go` to
resolve repo+branch directly via `git.Toplevel`/`git.CurrentBranch` — it no
longer touches `.wip` state or `EnsureBranchState` at all (see
`.wip/20260912135212-.../review.md`'s Cluster-B-adjacent notes, or just read
`cmd/aidw/cmd/memory.go` fresh). The actual line numbers and the "ensure
branch state" framing are both wrong now. Re-derive H2's starting point from
current code, not from the spec's stale citation.

**AC-H** (verbatim from spec): Given a `.wip` dir containing this branch's
evaluation attachments, when `migrate-state` runs, then every file exists in
the new store with matching checksums AND still exists at the source; legacy
commands still resolve it. Given migration completed, when memory lookup
runs from a second checkout of the same repo/work, then it returns the same
facts (same key).

### Recommended sequencing for Cluster H

Design doc (`work-model.md` §6) already resolved the two big open questions
for this cluster (copy+read-through migration stance, no forced cutover), so
you can go straight to `/wip-plan` without another design round:

```
git checkout -b feat/work-model-migration main   # stack directly on main now
aidw start .
# populate context.md pointing at spec.md Cluster H + work-model.md §6
/wip-plan
```

Given H1/H2's blast radius (touches every user's `.wip` dirs and their
memory DB), **follow the same rigor Cluster G got**: skeptic-review the spec
before implementing, then after implementing, dispatch 2-3 parallel
adversarial `wip-reviewer` (Opus) self-review passes with live reproduction
before calling it done. Budget for at least one fix round — every cluster in
this project so far has needed one.

## After H: Cluster I (archive lifecycle)

From spec (line ~367), depends on H (lifecycle ops act on the migrated
store):
- **I1** — `aidw work pause|done|archive <id>` lifecycle transitions (no
  file deletion); `archived` excluded from default `list`.
- **I2** — Re-route `wip-cleanup`/`wip-clear` skills and
  `cleanup-branch`/`clear-wip`/`clear-others` commands onto lifecycle ops;
  migrate Phase-1's `.wip/.../archive/` subdirs into the store; permanent
  removal only via `aidw work purge <id>` with preview + confirmation
  (reuse Phase 1's `requireArchiveBeforePurge` shape again).

## After I: Cluster J / Phase 3 (freeform mode)

Gated on F+G (both done). From spec (line ~383): `--mode freeform` on
`work start`, `work.json` + `context.md` only (no spec/plan/review
artifacts), no bootstrap coupling (no repo-docs pass, no embedding, no
`.github`/`GEMINI.md` seeding on freeform start), attachment-optional work
(non-git scratch dirs, detached HEAD, multiple work_ids on one branch).
Design doc §8 already covers the schema-level requirements. Does not depend
on H or I.

## Load-bearing lessons from this session (read before you implement)

These cost real debugging/fix cycles across Phase 1 and Cluster G — don't
re-learn them:

1. **A self-review pass on your OWN fix can find that the fix introduced a
   new bug.** This happened twice (Phase 1's archive-cleanup fix broke
   `SetStage` validation; Cluster G's permissions/lock fixes both needed a
   second round). Budget for it — "implement → review → fix → re-review" is
   the actual cycle length, not "implement → review → fix."
2. **Mutation testing beats reading the tests.** Cluster G's resolver had a
   green test suite while four separate deletions to its core safety logic
   (repo_id check, phase fallback, phase ordering, dedup) each left it green.
   If you're reviewing correctness-critical matching/locking logic, actually
   try deleting the check and see if a test fails — don't trust "tests
   exist" as a proxy for "tests would catch a regression."
3. **A TOCTOU race in userspace file-locking code is very hard to patch
   incrementally.** The lock primitive went through two designs (mtime+token
   steal, then `flock`) before it was actually correct. If Cluster H needs
   any new locking/concurrency primitive, prefer `flock` (or another
   kernel-arbitrated primitive) from the start rather than hand-rolling
   staleness detection.
4. **CI running for the first time surfaces environment-dependent test bugs
   that pass forever on a single developer's machine.** This PR's own CI
   workflow (`ci.yml`) failed on its first run over a test that hardcoded an
   assumption about `git init`'s default branch name, true only because of
   this machine's global `init.defaultBranch=main` config. Any new test that
   creates a git repo and cares about the branch name must pass it
   explicitly, never rely on `git init`'s default.
5. **Don't touch global git config during debugging**, even to test a
   hypothesis — use an isolated `$HOME`/`env -i` instead. This session did
   it once by accident (recoverable, but avoidable) while chasing lesson #4.
6. **The `state`/`work` package split (execution root / repo identity /
   state dir, kept as three distinct concepts) exists specifically because
   Phase 1 review kept finding bugs from conflating similar-but-different
   concepts** (is-a-git-repo vs. has-active-work vs. where-does-state-live).
   Cluster H's migration code will be tempted to blur "the source `.wip`
   path" and "the resolved work_id" — keep them as explicit, separately-named
   values throughout, the way `RepoIdentity` vs. `ExecutionRoot` do.

## Residual / deferred items still open (not blocking H, but don't lose them)

Consolidated from `.wip/20260909100540-.../review.md` and
`.wip/20260912135212-.../review.md`:

- `work start --branch <override>` vs. HEAD coherence is an actual
  unresolved spec-level question (is `--branch` a pure label, or should it
  re-target HEAD?), not a bug — needs a decision whenever someone next
  touches `work start`.
- Users who installed `aidw` before PR #51 keep Gemini-specific model
  defaults baked into their `~/.claude/aidw.env.sh`; not auto-rewritten
  (would mean silently editing a user's config file). Same logic likely
  applies to whatever config Cluster H's migration touches — prefer
  explicit, user-triggered migration commands over silent rewrites.
- `Provenance.AuthoringSession` (in the `work.json` schema) is declared but
  never populated by anything yet.
- `sessions/*.json` (session-binding files) are never garbage-collected —
  unbounded growth for repos with many unbound hook fires over time. Might
  be worth addressing as part of H or I, since both touch the state-dir
  layout anyway.
- No typed `ErrNotFound` in the `work` package yet — `work attach --work
  <bogus>` surfaces a raw filesystem error. Minor, but Cluster H's
  migration tooling will want clean error types for its own failure modes;
  consider fixing this alongside rather than compounding the pattern.
- A few low-priority test/hygiene gaps (documented in each review.md,
  not re-listed here) — worth a skim but none block Cluster H.

## Key file map

| What | Where |
|---|---|
| Design contract | `docs/design/work-model.md` |
| Original backlog spec (all clusters) | `.wip/20260909100540-chore-external-findings-gaps-audit-f656ffd0/spec.md` (gitignored, this worktree only) |
| Cluster G implementation spec (example of the level of detail expected) | `.wip/20260912135212-feat-work-model-store-resolver-b326ef56/spec.md` |
| Cluster G review record (example of review rigor to replicate) | `.wip/20260912135212-feat-work-model-store-resolver-b326ef56/review.md` |
| State package (execution root, repo identity, locking) | `cmd/aidw/internal/state/` |
| Work package (schema, store, resolver, sessions) | `cmd/aidw/internal/work/` |
| CLI commands | `cmd/aidw/cmd/work.go` |
| Hook integration | `templates/global/scripts/save-wip-snapshot.sh` |
| Legacy `.wip` resolver (what H1 migrates FROM) | `cmd/aidw/internal/wip/wip.go` |
| Legacy memory store (what H2 re-keys) | `cmd/aidw/cmd/memory.go` |

## How to verify you're starting from a clean, working baseline

```
git checkout main && git pull --ff-only
go build ./... && go vet ./... && go test ./... -count=1
make mirrors   # should produce zero diff
```
All of the above should be green before you start Cluster H. If anything
here fails, something regressed post-merge — investigate before building on
top of it.
