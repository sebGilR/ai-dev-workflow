# Cluster H execution plan + parallelization matrix

Written 2026-09-12 by a planning session that did **not** execute. Companion
to `handoff-cluster-h.md` (status/lessons) and `work-model.md` (the design
contract). This file exists to let an executing session start without
re-deriving the two decisions that gate parallelism.

Read order for the executing session: `handoff-cluster-h.md` → `work-model.md`
§5–§6 → this file.

---

## 0. Decisions resolved during planning (do not re-litigate)

These were the two open questions blocking a parallel matrix. Both were
resolved against current code, not against the backlog spec's prose. Each
records its tiebreaker so a reviewer can challenge it on evidence.

### D1 — Memory re-key: `repo_id` is load-bearing, `work_id` is a scope, not a replacement

**Resolution:** re-key on **`repo_id` + a single `scope` string**, not on
`work_id` directly.

```
facts:  UNIQUE(repo_id, scope, key)          scope ∈ {"repo", "branch:<slug>", "work:<id>"}
items:  UNIQUE(repo_id, file_path)           (no scope — items has no branch today)
```

**Why `work_id` cannot be the key.** The spec's "switch to work-ID keys"
framing is not implementable as written:

1. `items` (`cmd/aidw/internal/memory/db.go:69`) has **no branch column at
   all** — `UNIQUE(repo_path, file_path)`. There is nothing to map to a
   `work_id`. `repo_id` alone is the whole re-key for doc-index rows.
2. `memory.go:22-29` states as a deliberate invariant that memory must work
   on a repo/branch with **no `.wip` state at all** — that is what
   `/wip-document-project` does on a fresh repo. A `work_id`-keyed facts
   table cannot represent those rows.
3. AC-G's own premise is that **one branch can carry multiple work
   records**, so `branch → work_id` is not even a function, let alone a
   total one.

**Why this still satisfies AC-H clause 2** ("memory lookup from a second
checkout of the same repo/work returns the same facts"). Today's key is
`repo_path = git rev-parse --show-toplevel`, which is *worktree-local* — a
second worktree of the same clone gets a different absolute path and
therefore silently sees different facts. That is the actual bug AC-H is
describing. `state.RepoIdentity` keys off `--git-common-dir`, which all
worktrees of one clone share (`work-model.md` §5), so re-keying on `repo_id`
fixes it exactly. Note the scope boundary per §5: two independent *clones*
are different `repo_id`s unless explicitly linked in `repos.json` — "second
checkout" in AC-H means a worktree, and that is what this satisfies.

**Why this is what unlocks parallelism.** Legacy rows migrate
deterministically — `repo_path → repo_id` mechanically via `RepoIdentity`,
`branch → "branch:<slug>"` textually. Neither derivation consults H1's
old-path→work_id mapping. **H2 therefore has no dependency on H1** and the
two run concurrently. `scope = "work:<id>"` becomes reachable later, written
by the live resolver when a work context is present — it is a forward-looking
capacity, not something the migration backfills.

**Escape hatch / tiebreaker if challenged:** if a reviewer shows AC-H clause 2
genuinely requires `work_id` on facts rows, H2 serializes behind H1's mapping
artifact and the matrix collapses to a chain (Batch 1 becomes single-lane).
Raise it to Opus before switching — do not let a lane re-decide this alone.

**Unresolvable-`repo_path` policy (mandatory, easy to get wrong):**
`RepoIdentity` shells out to git, so it **fails for any `repo_path` that no
longer exists on disk** — a deleted or moved clone. Those rows must **never
be dropped**. Migrate them to a quarantine scope
(`repo_id = "unresolved:<sha256(repo_path)[:16]>"`), keep every column, and
report the count in the migration summary. Silent row loss here is the same
class of failure Phase 1 spent two rounds eliminating from the archive path.

### D2 — `migrate-state` inventory scope: bounded, never a filesystem walk

`work-model.md` §6 says "every `.wip` dir it can find," which is unbounded.
Left undefined, an implementer either walks `$HOME` or silently handles only
the invoked repo. **Neither is acceptable.** The scope is:

| Source | Behavior |
|---|---|
| `aidw migrate-state <path>` (default) | The repo at `<path>`: enumerate `git worktree list`, scan each worktree's `.wip/` for dated branch dirs. |
| `--path <dir>` (repeatable) | Additional explicit roots, same per-repo treatment. |
| `--all-registered` | Iterate `repos.json` `last_known_paths`; paths that no longer resolve are **reported and skipped**, never fatal. |

**No unbounded traversal, ever.** No `$HOME` walk, no `find /`. If a user has
a `.wip` dir somewhere unregistered, `--path` is how they reach it. Write
this into the spec as a stated assumption so the skeptic pass can attack it
rather than discover it.

### D3 — "Read-through" is satisfied by not breaking `wip.go`

§6 item 6 ("legacy `.wip` lookup stays read-through — functional, not just
present") has a cheap correct reading and an expensive wrong one.

**Correct:** `wip.go` already reads `.wip` directly and H1 never deletes
sources, so read-through holds **by construction**. The deliverable is a
test proving legacy commands still resolve a migrated dir — not new code.

**Wrong:** building a compatibility shim/dispatch layer nobody asked for.

Any expansion beyond "don't break `wip.go` + prove it with a test" requires
Opus sign-off. This is the single most likely place for scope to inflate.

---

## 1. Risk register — ranked, with the one that tests structurally cannot catch

### R1 (critical) — `vec_facts`/`vec_items` id re-pointing during table rebuild

SQLite **cannot** alter a `UNIQUE` constraint via `ALTER TABLE`. Re-keying
`facts`/`items` therefore **forces a full table rebuild** (create-new → copy
→ drop → rename). And `vec_facts`/`vec_items` are joined to their base tables
by `id` **by convention only — there is no foreign key**:

- `StoreFact` inserts `vec_facts(id, embedding)` using `facts.id` (`db.go:141-154`)
- `Search` does `JOIN items i ON v.id = i.id` (`db.go:220`)

A rebuild written the natural way —

```sql
INSERT INTO facts_new (repo_id, scope, key, value) SELECT ... FROM facts;
```

— omits `id`, gets fresh AUTOINCREMENT values, and **silently re-points every
embedding at a different fact**. No error, no crash, wrong answers forever.

**Why no ordinary test catches it:** per §6 and Phase 1's notes, this
environment runs with **vector search disabled** (`vec_version()` fails →
`vectorEnabled == false` → `IndexItem` early-returns, `Search` errors out).
Every test that goes through the vector path is inert here.

**Mandatory mitigation, to be written into the spec as an explicit task:**
a test that asserts id preservation **directly on the base tables** —
`SELECT id, key FROM facts` compared as a set before and after the rebuild —
never one that exercises `Search`/`IndexItem`. Plus: the rebuild statement
must carry `id` explicitly, and a code comment must say why.

### R2 (high) — no schema versioning exists in `memory.DB`

`db.init()` (`db.go:50`) is `CREATE TABLE IF NOT EXISTS` only. There is **no**
`PRAGMA user_version`, no migrations table. H2 must **add** versioning as part
of its own work — it cannot assume a mechanism it is about to need. Without
it, the migration is neither idempotent nor detectably-complete, and §6's
"drop the dual-read once confirmed" has no signal to key off.

### R3 (high) — concurrent writes to `work/<id>/work.json`

H1 writes work records while a live session may be running `work checkpoint`.
All H1 writes go through `work.Save` / `work.UpdateRecord` /
`state.AcquireLock` — **never raw `os.WriteFile`**. Per Lesson 3, if any new
coordination is needed, use `flock` (`state/lock.go` already does) rather
than hand-rolled staleness detection.

### R4 (medium) — divergence must surface, never auto-resolve

Re-running `migrate-state` on an already-mapped path recomputes checksums. If
destination ≠ source, that entry is **reported as divergent and skipped** —
no overwrite, no "newest wins". Other entries continue; the summary exits
non-zero. Same no-guessing principle as §4's ambiguity rule.

### R5 (medium) — `mirrors_test.go` drift fails CI

A new `migrate-state` command changes skill/agent surface. `make mirrors`
must run and be committed before the PR, or CI goes red (this already
happened once in Phase 1).

---

## 2. Component inventory (verified against current code)

| Component | Path | Status |
|---|---|---|
| New migration engine | `cmd/aidw/internal/migrate/` | to create |
| New CLI command | `cmd/aidw/cmd/migrate_state.go` | to create |
| Memory re-key | `cmd/aidw/internal/memory/db.go`, `cmd/aidw/cmd/memory.go` | to modify |
| Typed errors | `cmd/aidw/internal/work/errors.go` (new file) | to create |
| Checksum home | `Provenance.SourceHashes` | **verified free** — declared at `record.go:84`, initialized empty at `store.go:50`, written by nothing |
| Old-path→work_id mapping | `$AIDW_STATE_DIR/migrations/wip-paths.json` | to create |
| Purge UX to reuse | `requireArchiveBeforePurge`, `wip.go:68` | reuse verbatim shape |
| Repo identity | `state.RepoIdentity`, `repos.go:113` | reuse |
| Atomic write / lock | `util.AtomicWrite`, `state.AcquireLock` | reuse |

**Naming discipline (Lesson 6):** the source `.wip` directory path and the
resolved `work_id` are **two separately-named values** carried end to end —
`sourceWipDir` and `workID`, never merged into one "key" variable. The
mapping file is the only place they meet. This is the exact conflation that
`RepoIdentity` vs `ExecutionRoot` exists to prevent.

---

## 3. Parallelization matrix

Legend: **[O]** = Opus, main session or Opus reviewer. **[S]** = sonnet
background agent, medium effort. `∥` = concurrent.

| Batch | Lanes | Model | Depends on | Owns (exclusive file scope) |
|---|---|---|---|---|
| **0** Baseline + spec | serial | **[O]** | — | `.wip/**` (spec.md), branch creation |
| **0.5** Typed errors | serial, ~15 min | **[S]** | 0 | `work/errors.go` (new), one localized `store.go` edit |
| **1** Core build | **A ∥ B** | **[S]** ×2 | 0.5 | A: `internal/migrate/**`, `cmd/migrate_state.go` · B: `internal/memory/**`, `cmd/memory.go` |
| **2** Surface | **D ∥ E** | **[S]** ×2 | 1A | D: `--cleanup-sources` in A's files · E: docs, skills, `make mirrors` |
| **3** Adversarial | **R1 ∥ R2 ∥ R3** | **[O]** ×3 | 2 | read-only |
| **4** Fix round | serial | **[O]** dispatching **[S]** | 3 | as findings dictate |
| **5** Re-review + PR | 1–2 passes | **[O]** | 4 | — |

### Why 0.5 is serial and not a third lane

The obvious move is a Lane C running alongside A and B. Don't: `ErrNotFound`
touches `work/store.go`'s `Load`, and Lane A's migration code reads work
records. Landing it first — it is ~20 lines — costs 15 minutes and removes a
shared-file conflict from the fan-out entirely. The handoff already flags
`ErrNotFound` as a residual worth fixing here rather than compounding.

### Batch 1 lane detail

**Lane A — H1 migration engine** *(the larger lane; expect ~2× Lane B)*
1. Inventory per D2 (`git worktree list` + `.wip` dated dirs; no fs walk).
2. Per `.wip` dir → one `work.Record`: `mode=delivery`, `lifecycle=active`,
   `stage`/`branch`/`title` read from that dir's `status.json`.
3. Copy **all** files as opaque blobs → `work/<id>/attachments/` (§6 step 2:
   nothing is dropped, including files the schema has no named field for).
   Phase-1's `.wip/<branch>/archive/<ts>/` subdirs are just more files here.
4. sha256 per file → `Provenance.SourceHashes` (relpath → digest); **verify
   before recording success** (§6 step 3).
5. Mapping write → `migrations/wip-paths.json`, lock-protected + atomic.
6. Idempotent: an already-mapped path re-verifies rather than re-copying;
   divergence → R4 behavior.
7. **Never deletes a source** (§6 step 5). Deletion is Batch 2 Lane D only.

**Lane B — H2 memory re-key** *(fully independent of A, per D1)*
1. Add `PRAGMA user_version` + migration step (R2).
2. Rebuild `facts`/`items` with the D1 keys — **`id` carried explicitly** (R1).
3. Migrate rows: `repo_path → repo_id`, `branch → "branch:<slug>"`;
   unresolvable paths → quarantine scope, never dropped (D1 policy).
4. Dual-read in `memory.go`: new key first, legacy key fallback, until
   `user_version` says migrated — then the fallback drops (§6).
5. Must work with **vector search disabled** — that is this environment's
   normal state, not a degraded mode.
6. **The id-preservation test from R1 is a named deliverable of this lane**,
   not a nice-to-have.

### Batch 2 lane detail

**Lane D — `migrate-state --cleanup-sources`** (serial after A; mechanical —
the pattern is already in-tree). Preview-then-confirm reusing
`requireArchiveBeforePurge`'s shape (`wip.go:68`), including the discipline
that `--dry-run` is a read-only preview and is **never refused**
(`wip.go:65-66`). Refuses when there is nothing to clean up. Only ever
deletes sources that the mapping records as verified-copied.

**Lane E — surface + mirrors.** Docs for `migrate-state`, any skill wiring,
`make mirrors` regen (R5). Independent files from D.

### Batch 3 — three adversarial passes, one job each

Give each pass a *distinct* mandate; three generalists produce three copies
of the same review.

| Pass | Mandate |
|---|---|
| **R1 Mutation testing** | Per Lesson 2 — *actually delete* the checksum gate, the `id`-preserving column list, the `repo_id` match, the divergence check. Each deletion must turn a test red. A green suite after a deletion is a finding. |
| **R2 Live AC-H reproduction** | Run the real binary end to end: `.wip` dir with attachments → `migrate-state` → assert every file present at destination with matching checksum **AND still at source** → assert legacy `wip` commands still resolve it → assert memory lookup from a second worktree returns the same facts. |
| **R3 Concurrency + data-loss** | H1 writing `work.json` while a session checkpoints (R3); partial-failure/interrupted-migration states; every path where a row or file could be silently dropped (D1 quarantine, R4 divergence). |

Budget **at least one fix round** (Batch 4) — every cluster in this project
has needed one, and Lesson 1 says a fix pass on your own fix can introduce a
new bug. The real cycle is implement → review → fix → **re-review**.

---

## 4. Hard rules for every agent in every batch

Carry these verbatim into each subagent prompt; they each cost a real debug
cycle already.

1. **Any test creating a git repo passes `--initial-branch` explicitly.**
   Never rely on `git init`'s default — that broke CI on its first run
   (Lesson 4).
2. **Never touch global git config**, not even to test a hypothesis. Use an
   isolated `$HOME` / `env -i` (Lesson 5).
3. **Never delete a migration source.** Only Lane D deletes, only with
   preview + confirmation, only for verified-copied sources.
4. **`sourceWipDir` and `workID` stay separately named** end to end (Lesson 6).
5. **All work-record writes go through `work.Save`/`UpdateRecord`** — never
   raw file writes (R3).
6. **Reuse, don't reinvent:** `util.AtomicWrite`, `state.AcquireLock`,
   `requireArchiveBeforePurge`'s shape.
7. Gate before handing back: `go build ./... && go vet ./... && go test ./... -count=1`.

## 5. Residuals — decided, so they stop drifting

| Item | Call |
|---|---|
| Typed `ErrNotFound` in `work` | **In** — Batch 0.5 |
| `sessions/*.json` never GC'd | **Defer to Cluster I** — note it in `review.md`, don't build it here |
| `Provenance.AuthoringSession` never populated | **Defer** — H1 may set it opportunistically, not a requirement |
| `work start --branch` vs HEAD coherence | **Out of scope** — spec-level question, not H's |
| Pre-PR#51 installs with Gemini model defaults | **Out of scope** — but H's own migration follows the same principle: explicit user-triggered commands, never silent config rewrites |
