# Work Model Design (Cluster F)

Status: **approved (Seb, 2026-09-10)**. This document is the contract for
Clusters G-I (Phase 2) and the gate for Cluster J (Phase 3 — freeform mode).
Per spec `AC-F`, every G-I task below maps to a section here, and every open
choice carries a recorded rationale.

Source spec: `.wip/20260909100540-chore-external-findings-gaps-audit-f656ffd0/spec.md`,
Cluster F (line ~281) through Cluster J (line ~410).

## 1. Problem this replaces

Today, `.wip/<branch>/` is the only unit of durable workflow state, keyed by
git branch name inside the current worktree. This conflates three things that
need to vary independently:

- **Which repo** you're in (branch names collide across repos; worktrees of
  the same repo currently don't share `.wip` state at all — a known gap, see
  §3).
- **Which piece of work** you're doing (two unrelated tasks can legitimately
  share one branch; one task can span multiple worktrees/sessions).
- **Where durable history lives** (today: inside the worktree itself, so
  `git worktree remove` can destroy context that was never meant to be
  disposable).

The work model separates these three concerns into a `work_id`-keyed record
store, a session-binding layer, and a small set of resolver functions that
every consumer (wip, review, memory, hooks) shares instead of reimplementing.

## 2. `work.json` schema v1

```json
{
  "schema_version": 1,
  "work_id": "01J8Z3QK5N7VXY2M4R6T8W0ABC",
  "title": "external findings gaps audit",
  "mode": "delivery",
  "lifecycle": "active",
  "stage": "spec-reviewed",
  "context": {
    "goal": "...",
    "constraints": ["..."],
    "decisions": ["..."],
    "open_questions": ["..."],
    "next_action": "..."
  },
  "attachments": [
    {
      "repo_id": "a1b2c3d4...",
      "worktree_path": "/Users/seb/workspace/ai-dev-workflow",
      "branch": "chore/external-findings-gaps-audit",
      "head": "0316521..."
    }
  ],
  "initiative_id": null,
  "provenance": {
    "schema_version": 1,
    "created_at": "2026-09-09T10:05:40-05:00",
    "updated_at": "2026-09-10T08:25:59-05:00",
    "source_hashes": {},
    "authoring_session": "..."
  }
}
```

**Decision — `work_id` format: ULID.**
*Rationale:* sortable by creation time (`work list --recent` needs no
secondary sort key), 26 characters, URL/filename-safe, no coordination
required across concurrent sessions (unlike an incrementing integer), and
it's the format the spec itself recommended. UUID v4 was considered and
rejected — same collision-safety, but not time-sortable, which every listing
UX in G3 wants. Rejected a short random slug (nanoid-style) for the same
reason plus non-trivial collision-probability tuning at small ID lengths.

**Field notes:**
- `mode`: `delivery | freeform`. Delivery is today's `.wip` workflow
  (spec/plan/review/PR artifacts); freeform is Cluster J's lightweight mode.
  Fixed at creation; `work promote <id> --mode delivery` (J1) is the only
  transition, and it's one-way.
- `lifecycle`: `active | paused | done | archived` — **independent of**
  `stage`. `stage` only has meaning in delivery mode and keeps today's legacy
  values (`started`, `planned`, `specified`, `spec-reviewed`, `researched`,
  `implementing`, `reviewed`, `review-fixed`, `pr-prepped` — see
  `cmd/aidw/internal/wip/wip.go`'s `stages` map) valid as-is, so no stage
  migration is needed, only a re-key (§6). Cluster I's lifecycle operations
  (`pause|done|archive`) only ever touch `lifecycle`, never `stage`.
- `context`: freeform prose fields, deliberately unstructured beyond these
  five keys — this is what `context.md`/`task-context.md` collapse into for
  a record that also needs to be queried/listed, not just read.
- `attachments[]`: zero or more git-repo bindings. Zero for a freeform work
  item with no git attachment at all (J2 — non-git scratch directories, a
  detached HEAD, etc.). More than one for a task that spans repos. Multiple
  work items may share one `(repo_id, branch)` pair (distinct work_ids on one
  branch, J2) — attachments are many-to-many, not a foreign key back to a
  single owning work item.
- `initiative_id`: reserved, unused in Phase 2/3. No initiative concept
  exists yet; this is a placeholder so a future "epic" grouping doesn't
  require a schema migration.
- `provenance.schema_version`: lets `work.json` readers detect and reject (or
  upgrade) a record written by a future incompatible schema — the absence of
  exactly this kind of version field was flagged as a gap in Cluster A's
  provenance header during Phase 1 review; not repeating that mistake here.

## 3. State directory

**Decision — default: XDG, no macOS-specific split.**
`${XDG_STATE_HOME:-$HOME/.local/state}/aidw`, overridable via `AIDW_STATE_DIR`,
on every Unix platform including macOS.

*Rationale:* one predictable path to document, test, and debug, rather than
two. macOS's Application Support convention was considered — more
platform-idiomatic, but it doubles what needs documenting/supporting and
adds a runtime OS-detection branch for zero functional benefit (this is a
CLI tool's state dir, not a GUI app registering with the OS). aidw already
writes to `~/.claude/` on macOS without following Application Support
conventions there either, so XDG-everywhere is also the more consistent
choice given precedent in this codebase.

**Layout:**
```
$AIDW_STATE_DIR/
  work/
    <work_id>/
      work.json
      attachments/          # on-demand artifacts (spec.md, plan.md, etc. for delivery mode)
      context.md             # freeform mode's only extra file, when present
  sessions/
    <session_id>.json        # session -> work_id bindings (§4)
  repos.json                 # repo_id -> {aliases[], last_known_paths[]}
```

`repos.json` is the `repo_id` registry (§5). `sessions/` is written far more
often than `work/`, hence the split — session bindings are cheap, frequent,
small writes; work records are the durable payload.

## 4. Session bindings

**Contract:** explicit ID > session binding > unambiguous worktree
association; on ambiguity, list candidates and refuse to guess; never pick
"newest" as a tiebreaker. Multiple sessions may bind to the same work item
(e.g. two terminal tabs working the same task). Bindings must not overwrite
each other — a second session binding to a work item adds a binding, it does
not evict the first session's binding.

**Resolution order, precisely:**
1. **Explicit ID.** `--work <id>` (or equivalent) on any command always wins,
   no ambiguity possible.
2. **Session binding.** If this session (`session_id` from the Claude Code
   hook JSON, or an equivalent for non-hook invocations — see open question
   below) has a recorded binding in `sessions/<session_id>.json`, use it.
3. **Unambiguous worktree association.** If no binding exists, look up
   `attachments[].worktree_path` (or `repo_id` + `branch` for a moved
   worktree — see §5) across all `active`/`paused` work records. If exactly
   one record matches, bind this session to it and proceed. If zero match,
   the caller gets "no active work" (matching today's `ErrNoActiveWork`
   contract from Phase 1's lookup-vs-create split — G3's `list`/`status`
   inherit that same lookup-only discipline). If more than one matches, list
   every candidate (`work_id`, `title`, `branch`) and exit non-zero — never
   guess, never pick the most-recently-updated one. This is what makes AC-G's
   "two work items on the same branch" scenario safe: each session either has
   its own explicit binding or gets a forced disambiguation prompt, never a
   silent wrong-record write.

**Decision (Seb, 2026-09-10) — outside a Claude Code hook context (e.g. a
bare terminal invocation of `aidw work status` with no hook JSON on stdin),
skip session-level binding entirely and fall straight to step 3 (worktree
association).** No shell-PID/start-time derivation, no `$AIDW_SESSION_ID`
env var, in G1/G2's initial implementation. *Rationale:* simplest correct
behavior, doesn't block G3's command surface, and there's no usage evidence
yet that session-less ambiguity (two work items truly indistinguishable by
worktree alone, with no session context) is common enough to justify the
added complexity. Revisit if real usage says otherwise.

## 5. Resolver contract — three distinct concepts, never conflated

Phase-1 review (Cluster B/C self-review) repeatedly surfaced bugs from
conflating "is there a git repo here" with "is there active work here" with
"where does durable state live." This section exists specifically to prevent
that pattern from recurring in the work-model layer.

| Concept | Function | Backing | Notes |
|---|---|---|---|
| **Execution root** | `ExecutionRoot(path)` | `git rev-parse --show-toplevel` | Worktree-local. Stays exactly as-is — this is what diffs, `git status`, and file paths are relative to. Existing call sites (`git.Toplevel`, used at `wip.go:59,415,552,617,760,830`, `review.go:59,178,396`, `bootstrap.go:176`, `document.go:16`) are UNCHANGED by this design — do not swap them for `RepoIdentity`. |
| **Repo identity** | `RepoIdentity(path)` | realpath of `git rev-parse --path-format=absolute --git-common-dir`, mapped through `repos.json` to a `repo_id` (with aliases) | This is new. Two independent clones of the same remote are DIFFERENT `repo_id`s unless explicitly linked — never key by basename or remote URL (a fork, a mirror, and a stale clone can all share a basename or URL). `--git-common-dir` (not `--git-dir`) is what makes this correct for worktrees: a `git worktree add` checkout has its own `--git-dir` but shares one `--git-common-dir` with its parent, so all worktrees of one clone resolve to the same `repo_id`. This closes the "known worktree-isolation gap" noted throughout Phase 1 review (Cluster B execution.md, "Mapping agent 2") without touching any Phase-1 code path. |
| **State dir** | `StateDir()` | `$AIDW_STATE_DIR` env, else the XDG default (§3) | Global, not per-repo. |

**G2 requirement:** add a `CommonDir()` helper to `cmd/aidw/internal/git/git.go`
(confirmed absent from the current codebase — checked directly) alongside the
existing `Toplevel()`/`CurrentBranch()` functions, following their exact
style (thin wrapper over `run()`, single git invocation, trimmed output).

**Hard invariants (acceptance criteria for all of G-I, not just G):**
- `lookup`/`list`/`status` commands never create state. This generalizes
  Phase 1's `FindBranchState`/`EnsureBranchState` split — the split itself
  proved its worth (it's what caught the Cluster B blocker) and the same
  discipline carries forward: every G3 lookup command must go through a
  lookup-only path with the same shape as `FindBranchState`/`ErrNoActiveWork`,
  never a "seed if missing" path.
- Durable history is never stored under `.git/worktrees/<id>` (or anywhere
  inside a worktree that `git worktree remove` can delete). This is the
  entire reason a separate state dir exists — AC-G's third scenario ("a
  worktree is removed, the work record survives") is not optional, it's the
  design's core value proposition over the status quo.
- Atomic writes + serialized updates to shared metadata (`repos.json`,
  `sessions/*.json`, `work/<id>/work.json`). The codebase already has an
  atomic-write helper (`util.AtomicWrite`, used throughout Phase 1's fixes)
  — reuse it, don't reinvent it. "Serialized" means: concurrent writers to
  the *same* record must not interleave (e.g. a file lock or a
  compare-and-swap on `provenance.updated_at`); concurrent writers to
  *different* records need no coordination.
- Readable files are the source of truth; indexes are rebuildable. Anything
  resembling a cache or index (e.g. a future "list all work_ids" index) must
  be derivable by scanning `work/*/work.json` — never the only place a fact
  is recorded.

## 6. Migration (Cluster H)

**Decision — copy + read-through, no forced cutover.**

`aidw migrate-state`:
1. Inventories registered worktrees (`git worktree list`) and every `.wip`
   dir it can find.
2. Copies records **and all files** (including attachments this design
   doesn't have a named field for — copy them as opaque blobs under
   `work/<id>/attachments/`, don't drop anything) into `work/<id>/`.
3. Verifies success by checksum before recording it as done.
4. Writes an old-path -> `work_id` mapping (so legacy lookups can resolve
   forward).
5. **Never deletes sources.** Removal is only ever a separate, explicit
   `aidw migrate-state --cleanup-sources`, itself required to preview before
   deleting (matching the `--purge`-requires-preview pattern Phase 1 already
   established for `cleanup-branch`/`clear-wip`/`clear-others` — reuse that
   UX, don't invent a new one).
6. Legacy `.wip` lookup stays read-through (i.e. functional, not just
   present-but-ignored) for as long as both copies coexist — indefinitely,
   until the user explicitly runs `--cleanup-sources`. There is no forced
   cutover date or version gate.

*Rationale:* the alternative considered — copy once, then treat `.wip` as
deprecated soon after a successful migration — was rejected. It reduces how
long two resolvers coexist (arguably simpler to reason about), but it
reintroduces exactly the "silent data behind an agent's back" risk that
Phase 1's Cluster C self-review just spent two fix rounds eliminating from
the archive/cleanup path. A migration is inherently higher-stakes than a
cleanup (it's moving the source of truth, not just archiving old artifacts),
so it gets the more conservative of the two options, not the faster one.
Divergent copies (source and destination disagree after what should have
been an idempotent copy) are surfaced for manual resolution — never
auto-picked, same principle as the copy/no-guessing rule in §4.

**Memory re-key (H2):** `cmd/aidw/cmd/memory.go`'s `store`/`index`/`search`/
`list` — just fixed in Phase 1 to resolve repo+branch directly via
`git.Toplevel`/`git.CurrentBranch` rather than routing through `.wip` state
at all (see Phase 1 review, Cluster B fix) — migrate to `work_id`/`repo_id`
keys in the same pass as H1, keeping a compatibility view (dual-read: try
the new key, fall back to the old repo+branch key) until the migration is
confirmed complete, then drop the fallback. This must run with vector search
disabled during the migration itself (verified constraint in this
environment, per Phase 1 execution notes on `sqlite_ext`/`vec0.dylib`) —
facts and doc-index rows still migrate, just without live embedding
generation mid-migration.

## 7. Lifecycle (Cluster I)

`aidw work pause|done|archive <id>` — transitions on `lifecycle` only, never
touches `stage`, never deletes a file. `archived` is excluded from `list`'s
default output (still visible with an explicit flag, not yet named here —
`--all` or `--include-archived`, implementer's choice in G3/I1, not a design
decision that needs sign-off).

`wip-cleanup`/`wip-clear` skills and their backing commands
(`cleanup-branch`/`clear-wip`/`clear-others`) re-route to lifecycle
operations on the work store once G ships — i.e. what Phase 1 just hardened
(archive-not-delete, collision-safe batches, purge-requires-prior-archive) is
not thrown away, it's the model that `aidw work archive` inherits. Phase-1's
`.wip/<branch>/archive/<timestamp>/` subdirectories migrate into the work
store as part of H1's file copy — they're just more files under
`work/<id>/attachments/`.

Permanent deletion is `aidw work purge <id>` only, requiring the same
preview-then-confirm UX Phase 1 built for `--purge` (print the file list,
require explicit confirmation, refuse when there's nothing archived to
purge yet — reuse `requireArchiveBeforePurge`'s shape).

## 8. Freeform mode requirements (for Cluster J, gated on F + G)

Not part of G/H/I's own acceptance criteria, but the schema and resolver
above must not preclude these — recorded here so J's implementer isn't
blocked re-deriving them:

- `mode` field (already in §2's schema) is the only structural difference
  between delivery and freeform at the storage layer.
- Freeform's on-disk footprint is `work.json` + `context.md` only — no
  `spec.md`/`plan.md`/`review.md`/etc. Those get created on demand only if
  `work promote --mode delivery` is later run.
- No bootstrap coupling: freeform `work start` must not trigger a repo-docs
  pass, embedding/indexing, `.github`/`GEMINI.md` seeding, or gitignore
  edits. This is a deliberate un-coupling from what `claude/skills/
  wip-start/SKILL.md:15` and `bootstrap.go:160-217` currently always do
  together — freeform start skips all of it; those capabilities remain
  available but must become separately, lazily invokable rather than
  automatic side effects of starting work.
- Attachment-optional: a freeform work item may have zero `attachments[]`
  entries (a non-git scratch directory), a detached-HEAD attachment, or
  multiple work items sharing one branch — all already representable by
  §2's schema (`attachments[]` is a list, can be empty; nothing in the
  schema requires a clean branch-per-work-item mapping).

## 9. Mapping to spec tasks (AC-F requirement)

| Spec task | Section here |
|---|---|
| G1 (work package, Record struct, atomic save) | §2, §5 (invariants) |
| G2 (shared resolver: ExecutionRoot/RepoIdentity/StateDir) | §5 |
| G3 (work start/list/status/attach/bind-session/checkpoint commands) | §4 (bindings), §5 (lookup-only invariant) |
| G4 (hook integration, checkpoint --from-hook) | §4 (session binding resolution order) |
| H1 (migrate-state) | §6 |
| H2 (memory re-key) | §6 |
| I1 (lifecycle transitions) | §7 |
| I2 (destructive-semantics replacement) | §7 |
| J1 (freeform mode + promote) | §8, §2 (`mode` field) |
| J2 (attachment-optional) | §8, §2 (`attachments[]` as a list) |
| J3 (lightweight startup) | §8 |

## 10. Open choices not yet resolved (explicitly flagged, not blocking sign-off)

These don't block signing off on the schema/resolver/lifecycle contract
above, but need a decision before or during G's implementation:

1. **Exact flag name for showing archived work in `list`** (§7) —
   implementer's choice, not architecturally significant.
2. **`repos.json` alias-merge UX** — what happens when a user manually moves
   or renames a repo directory and `aidw` needs to notice the old path no
   longer resolves. Not needed for G's initial implementation (repos.json
   can start append-only, no alias pruning), but worth flagging so it's not
   forgotten before this ships broadly.

(Session identity outside hook contexts, previously open here, is resolved
in §4.)

---

*Approved 2026-09-10. Cluster G implementation starts on a new branch
stacked on this one.*
