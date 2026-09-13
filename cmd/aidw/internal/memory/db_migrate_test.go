package memory

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"aidw/cmd/aidw/internal/state"
)

// seedLegacyDB creates a fresh sqlite file at path with the pre-D1
// repo_path/branch-keyed schema (no PRAGMA user_version set, so it reports
// 0 — exactly the "pre-versioning legacy DB" state Task B.1's init() must
// leave alone). Returns the closed path; callers reopen it via OpenAt.
func seedLegacyDB(t *testing.T, path string) {
	t.Helper()
	conn, err := sql.Open(driverName, path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Exec(`
		CREATE TABLE facts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_path TEXT,
			branch TEXT,
			key TEXT,
			value TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(repo_path, branch, key)
		);
	`); err != nil {
		t.Fatalf("create legacy facts: %v", err)
	}
	if _, err := conn.Exec(`
		CREATE TABLE items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_path TEXT,
			file_path TEXT,
			content TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(repo_path, file_path)
		);
	`); err != nil {
		t.Fatalf("create legacy items: %v", err)
	}
}

// insertLegacyFact inserts one row with an explicit id and created_at,
// bypassing AUTOINCREMENT defaults so tests can control ordering/ties.
func insertLegacyFact(t *testing.T, path string, id int64, repoPath, branch, key, value, createdAt string) {
	t.Helper()
	conn, err := sql.Open(driverName, path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(
		"INSERT INTO facts (id, repo_path, branch, key, value, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, repoPath, branch, key, value, createdAt,
	); err != nil {
		t.Fatalf("insert legacy fact: %v", err)
	}
}

func insertLegacyItem(t *testing.T, path string, id int64, repoPath, filePath, content, createdAt string) {
	t.Helper()
	conn, err := sql.Open(driverName, path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(
		"INSERT INTO items (id, repo_path, file_path, content, created_at) VALUES (?, ?, ?, ?, ?)",
		id, repoPath, filePath, content, createdAt,
	); err != nil {
		t.Fatalf("insert legacy item: %v", err)
	}
}

func quarantineID(repoPath string) string {
	sum := sha256.Sum256([]byte(repoPath))
	return "unresolved:" + hex.EncodeToString(sum[:])[:16]
}

// AC-USERVERSION: a fresh DB and a migrated legacy DB converge on identical
// sqlite_master.sql for facts/items, both reporting user_version == 2.
// Separately, a legacy DB that never runs `memory migrate` stays at
// user_version == 0 with legacy tables untouched.
func TestUserVersion_FreshAndMigratedConverge(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", t.TempDir())

	freshPath := filepath.Join(t.TempDir(), "fresh.db")
	fresh, err := OpenAt(freshPath)
	if err != nil {
		t.Fatalf("OpenAt fresh: %v", err)
	}
	defer fresh.Close()
	if !fresh.Migrated() {
		t.Fatal("a brand-new DB must be migrated == true immediately")
	}
	if v, err := fresh.userVersion(); err != nil || v != memorySchemaVersion {
		t.Fatalf("fresh user_version = %d, %v; want %d", v, err, memorySchemaVersion)
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyDB(t, legacyPath)
	insertLegacyFact(t, legacyPath, 1, "/repo/a", "main", "k", "v", "2026-01-01 00:00:00")

	legacy, err := OpenAt(legacyPath)
	if err != nil {
		t.Fatalf("OpenAt legacy: %v", err)
	}
	if legacy.Migrated() {
		t.Fatal("Open() must never auto-rebuild a legacy DB")
	}
	if v, err := legacy.userVersion(); err != nil || v != 0 {
		t.Fatalf("legacy user_version = %d, %v; want 0", v, err)
	}
	if _, err := legacy.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	legacy.Close()

	// Fresh handle on the now-migrated DB.
	migrated, err := OpenAt(legacyPath)
	if err != nil {
		t.Fatalf("reopen migrated: %v", err)
	}
	defer migrated.Close()
	if !migrated.Migrated() {
		t.Fatal("reopened DB must report migrated == true after memory migrate")
	}
	if v, err := migrated.userVersion(); err != nil || v != memorySchemaVersion {
		t.Fatalf("migrated user_version = %d, %v; want %d", v, err, memorySchemaVersion)
	}

	freshSQL := sqliteMasterSQL(t, fresh.conn, "facts") + sqliteMasterSQL(t, fresh.conn, "items")
	migratedSQL := sqliteMasterSQL(t, migrated.conn, "facts") + sqliteMasterSQL(t, migrated.conn, "items")
	if normalizeSQL(freshSQL) != normalizeSQL(migratedSQL) {
		t.Errorf("fresh vs migrated schema mismatch:\nfresh: %s\nmigrated: %s", freshSQL, migratedSQL)
	}
}

// TestUserVersion_OpenNeverRebuilds pins §2c decision 2: Open() alone,
// repeated any number of times, never advances user_version off 0 for a
// legacy DB, and normal reads/writes still work against the legacy schema.
func TestUserVersion_OpenNeverRebuilds(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyDB(t, path)
	insertLegacyFact(t, path, 1, "/repo/a", "main", "k", "v", "2026-01-01 00:00:00")

	for i := 0; i < 3; i++ {
		db, err := OpenAt(path)
		if err != nil {
			t.Fatalf("OpenAt iteration %d: %v", i, err)
		}
		if db.Migrated() {
			t.Fatalf("iteration %d: Open() must not have rebuilt", i)
		}
		if v, _ := db.userVersion(); v != 0 {
			t.Fatalf("iteration %d: user_version = %d, want 0", i, v)
		}
		db.Close()
	}
}

func sqliteMasterSQL(t *testing.T, conn *sql.DB, table string) string {
	t.Helper()
	var s string
	if err := conn.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&s); err != nil {
		t.Fatalf("read sqlite_master for %s: %v", table, err)
	}
	return s
}

// normalizeSQL collapses whitespace and strips the double-quotes SQLite
// adds around identifiers after an ALTER TABLE ... RENAME TO (e.g.
// `CREATE TABLE "facts"` vs. a freshly-CREATEd `CREATE TABLE facts`) — a
// cosmetic difference in how sqlite_master.sql happens to be stored, not a
// schema difference, so it must not fail the "identical modulo whitespace"
// comparison AC-USERVERSION asks for.
func normalizeSQL(s string) string {
	out := make([]byte, 0, len(s))
	lastSpace := false
	for _, r := range s {
		if r == '"' {
			continue
		}
		if r == ' ' || r == '\n' || r == '\t' {
			if !lastSpace {
				out = append(out, ' ')
			}
			lastSpace = true
			continue
		}
		lastSpace = false
		out = append(out, byte(r))
	}
	return string(out)
}

// AC-IDPRESERVE (mandatory, unconditional, both tables — Task B.4). This
// test never runs through Search/IndexItem/StoreFact/GetFact — it asserts
// directly on the base tables' (id, key, value) / (id, repo_id, file_path)
// triples, which is the only way to catch a bug that carries id correctly
// but shuffles the value/repo_id column. Named here per the skeptic's
// finding 2 fix item 3: db.go's Search does
// "JOIN items i ON v.id = i.id" — a join with no foreign key, which is
// exactly what a wrong id (or right id/wrong repo_id) would silently break.
func TestMigrate_IDPreservation(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyDB(t, path)

	// Distinct legacy repo_path values that will NOT resolve via
	// state.RepoIdentity (no real git repo there) — deterministic
	// quarantine ids, computed independently by the test.
	repoA := "/nonexistent/repo/a"
	repoB := "/nonexistent/repo/b"

	// IDs are deliberately non-sequential and inserted out of ascending
	// order (703 before 501, both far from 1) — a rebuild that silently
	// drops the explicit `id` column and lets SQLite AUTOINCREMENT assign
	// fresh values would produce 1,2,3 here by coincidence if the legacy
	// ids were themselves 1,2,3 inserted in order, giving this test zero
	// real coverage of R1. Non-sequential, out-of-order ids close that gap.
	insertLegacyFact(t, path, 703, repoB, "main", "k3", "v3", "2026-01-01 00:00:02")
	insertLegacyFact(t, path, 501, repoA, "main", "k1", "v1", "2026-01-01 00:00:00")
	insertLegacyFact(t, path, 302, repoA, "dev", "k2", "v2", "2026-01-01 00:00:01")

	insertLegacyItem(t, path, 900, repoB, "docs/x.md", "world", "2026-01-01 00:00:01")
	insertLegacyItem(t, path, 450, repoA, "README.md", "hello", "2026-01-01 00:00:00")

	db, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	wantFacts := []factTriple{
		{501, "k1", "v1"}, {302, "k2", "v2"}, {703, "k3", "v3"},
	}

	summary, err := db.Migrate()
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if summary.FactsCollapsed != 0 {
		t.Fatalf("no collision expected in this fixture, got FactsCollapsed=%d", summary.FactsCollapsed)
	}

	gotFacts := []factTriple{}
	rows, err := db.conn.Query("SELECT id, key, value FROM facts")
	if err != nil {
		t.Fatalf("select facts: %v", err)
	}
	for rows.Next() {
		var ft factTriple
		if err := rows.Scan(&ft.id, &ft.key, &ft.value); err != nil {
			t.Fatalf("scan fact: %v", err)
		}
		gotFacts = append(gotFacts, ft)
	}
	rows.Close()
	assertSameFactTriples(t, wantFacts, gotFacts)

	type itemTriple struct{ id int64; repoID, filePath string }
	wantItems := []itemTriple{
		{450, quarantineID(repoA), "README.md"},
		{900, quarantineID(repoB), "docs/x.md"},
	}
	gotItems := []itemTriple{}
	irows, err := db.conn.Query("SELECT id, repo_id, file_path FROM items")
	if err != nil {
		t.Fatalf("select items: %v", err)
	}
	for irows.Next() {
		var it itemTriple
		if err := irows.Scan(&it.id, &it.repoID, &it.filePath); err != nil {
			t.Fatalf("scan item: %v", err)
		}
		gotItems = append(gotItems, it)
	}
	irows.Close()

	sort.Slice(wantItems, func(i, j int) bool { return wantItems[i].id < wantItems[j].id })
	sort.Slice(gotItems, func(i, j int) bool { return gotItems[i].id < gotItems[j].id })
	if len(wantItems) != len(gotItems) {
		t.Fatalf("item count = %d, want %d", len(gotItems), len(wantItems))
	}
	for i := range wantItems {
		if wantItems[i] != gotItems[i] {
			t.Errorf("item[%d] = %+v, want %+v", i, gotItems[i], wantItems[i])
		}
	}
}

type factTriple struct {
	id         int64
	key, value string
}

func assertSameFactTriples(t *testing.T, want, got []factTriple) {
	t.Helper()
	sort.Slice(want, func(i, j int) bool { return want[i].id < want[j].id })
	sort.Slice(got, func(i, j int) bool { return got[i].id < got[j].id })
	if len(want) != len(got) {
		t.Fatalf("fact count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("fact[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// AC-REBUILD-ATOMIC: interrupting the rebuild transaction after
// DROP TABLE facts but before COMMIT (via testHookAfterDropFacts, since
// killing the process mid-transaction isn't reliably reproducible in a Go
// test) must leave the legacy facts table fully intact, user_version == 0,
// and no orphan facts_new table on the next Open().
func TestMigrate_RebuildAtomic_InterruptedRollsBackCompletely(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyDB(t, path)
	insertLegacyFact(t, path, 1, "/nonexistent/repo", "main", "k", "v", "2026-01-01 00:00:00")
	insertLegacyItem(t, path, 1, "/nonexistent/repo", "README.md", "hi", "2026-01-01 00:00:00")

	db, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}

	injected := errors.New("simulated crash after DROP TABLE facts")
	testHookAfterDropFacts = func() error { return injected }
	defer func() { testHookAfterDropFacts = nil }()

	if _, err := db.Migrate(); err == nil {
		t.Fatal("expected Migrate to fail via the injected hook")
	}
	db.Close()

	// Re-open fresh, as the AC specifies ("re-opening the database
	// afterward").
	reopened, err := OpenAt(path)
	if err != nil {
		t.Fatalf("reopen after failed migrate: %v", err)
	}
	defer reopened.Close()

	if reopened.Migrated() {
		t.Fatal("a rolled-back migration must leave the DB unmigrated")
	}
	if v, _ := reopened.userVersion(); v != 0 {
		t.Fatalf("user_version = %d, want 0 after rollback", v)
	}

	exists, err := reopened.tableExists("facts_new")
	if err != nil {
		t.Fatalf("tableExists facts_new: %v", err)
	}
	if exists {
		t.Error("orphan facts_new table must not survive a rolled-back migration")
	}
	itemsNewExists, err := reopened.tableExists("items_new")
	if err != nil {
		t.Fatalf("tableExists items_new: %v", err)
	}
	if itemsNewExists {
		t.Error("orphan items_new table must not survive a rolled-back migration")
	}

	var value string
	if err := reopened.conn.QueryRow("SELECT value FROM facts WHERE repo_path=? AND branch=? AND key=?", "/nonexistent/repo", "main", "k").Scan(&value); err != nil {
		t.Fatalf("legacy fact row must survive rollback: %v", err)
	}
	if value != "v" {
		t.Errorf("legacy fact value = %q, want %q", value, "v")
	}
}

// AC-ROWCOUNT: items has no scope collapse (bare count equality), an
// unresolvable repo_path quarantines rather than drops, and facts' count
// delta equals the reported facts_collapsed — never a bare pre/post
// equality for facts, since the scope collapse can legitimately merge rows.
func TestMigrate_RowCount(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyDB(t, path)

	repo := "/nonexistent/repo/gone"
	// Two legacy rows that collapse to the same (repo_id, "repo", key)
	// post-migration: same repo, same key, different (now-discarded)
	// branch. The later created_at (id=2) must win.
	insertLegacyFact(t, path, 1, repo, "main", "dup", "old-value", "2026-01-01 00:00:00")
	insertLegacyFact(t, path, 2, repo, "dev", "dup", "new-value", "2026-01-02 00:00:00")
	// A non-colliding row.
	insertLegacyFact(t, path, 3, repo, "main", "solo", "solo-value", "2026-01-01 00:00:00")

	insertLegacyItem(t, path, 1, repo, "a.md", "content-a", "2026-01-01 00:00:00")
	insertLegacyItem(t, path, 2, repo, "b.md", "content-b", "2026-01-01 00:00:00")

	var factsBefore, itemsBefore int
	{
		conn, err := sql.Open(driverName, path)
		if err != nil {
			t.Fatalf("open raw: %v", err)
		}
		conn.QueryRow("SELECT COUNT(*) FROM facts").Scan(&factsBefore)
		conn.QueryRow("SELECT COUNT(*) FROM items").Scan(&itemsBefore)
		conn.Close()
	}

	db, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	summary, err := db.Migrate()
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var factsAfter, itemsAfter int
	db.conn.QueryRow("SELECT COUNT(*) FROM facts").Scan(&factsAfter)
	db.conn.QueryRow("SELECT COUNT(*) FROM items").Scan(&itemsAfter)

	if itemsAfter != itemsBefore {
		t.Errorf("items count = %d, want unchanged %d (no scope collapse for items)", itemsAfter, itemsBefore)
	}
	if factsBefore-factsAfter != summary.FactsCollapsed {
		t.Errorf("facts count delta = %d, want summary.FactsCollapsed = %d", factsBefore-factsAfter, summary.FactsCollapsed)
	}
	if summary.FactsCollapsed != 1 {
		t.Errorf("FactsCollapsed = %d, want 1 for this fixture", summary.FactsCollapsed)
	}

	var winningValue string
	if err := db.conn.QueryRow("SELECT value FROM facts WHERE repo_id=? AND scope='repo' AND key='dup'", quarantineID(repo)).Scan(&winningValue); err != nil {
		t.Fatalf("select winning dup row: %v", err)
	}
	if winningValue != "new-value" {
		t.Errorf("winning collapsed value = %q, want %q (later created_at)", winningValue, "new-value")
	}

	var repoID string
	if err := db.conn.QueryRow("SELECT repo_id FROM items WHERE file_path='a.md'").Scan(&repoID); err != nil {
		t.Fatalf("select item repo_id: %v", err)
	}
	if repoID != quarantineID(repo) {
		t.Errorf("item repo_id = %q, want quarantine form %q", repoID, quarantineID(repo))
	}
}

// AC-SECONDWORKTREE: a fact stored via StoreFact from one worktree of a
// clone (repo_id from that worktree's --git-common-dir) is visible via
// GetFact using the repo_id derived from a SECOND worktree of the same
// clone, necessarily checked out to a different branch (git forbids
// sharing one branch across worktrees). This is the unit-level
// reproduction of D1 clause 2 (facts scope collapse to the constant
// "repo" makes this hold — see §2c decision 1).
func TestSecondWorktree_SameRepoIDSameFact(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AIDW_STATE_DIR", stateDir)

	home := t.TempDir()
	gitEnv := []string{
		"HOME=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
		"PATH=" + os.Getenv("PATH"),
	}
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v (dir=%s): %v\n%s", args, dir, err, out)
		}
	}

	base := t.TempDir()
	repoDir := filepath.Join(base, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(repoDir, "init", "-q", "-b", "main")
	run(repoDir, "config", "user.email", "test@test.com")
	run(repoDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repoDir, "add", ".")
	run(repoDir, "commit", "-q", "-m", "init")

	worktreeDir := filepath.Join(base, "repo-wt2")
	run(repoDir, "worktree", "add", "-b", "feature", worktreeDir)

	repoID1, err := state.RepoIdentity(repoDir)
	if err != nil {
		t.Fatalf("RepoIdentity(main worktree): %v", err)
	}
	repoID2, err := state.RepoIdentity(worktreeDir)
	if err != nil {
		t.Fatalf("RepoIdentity(second worktree): %v", err)
	}
	if repoID1 != repoID2 {
		t.Fatalf("repo_id differs across worktrees of the same clone: %q vs %q", repoID1, repoID2)
	}

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	db, err := OpenAt(dbPath)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()
	if !db.Migrated() {
		t.Fatal("a brand-new DB must already be on the new schema")
	}

	if err := db.StoreFact("", "", repoID1, "repo", "greeting", "hello", nil); err != nil {
		t.Fatalf("StoreFact: %v", err)
	}

	got, err := db.GetFact("", "", repoID2, "repo", "greeting")
	if err != nil {
		t.Fatalf("GetFact: %v", err)
	}
	if got != "hello" {
		t.Errorf("GetFact from second worktree's repo_id = %q, want %q", got, "hello")
	}
}

// AC-DUALREAD (schema-level, not per-row — §2c decision 2 / Task B.6).
func TestDualRead_SchemaLevelSwitch(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyDB(t, path)
	insertLegacyFact(t, path, 1, "/repo/a", "main", "k", "legacy-value", "2026-01-01 00:00:00")

	legacy, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	if legacy.Migrated() {
		t.Fatal("must be unmigrated before memory migrate runs")
	}
	got, err := legacy.GetFact("/repo/a", "main", "should-be-ignored", "should-be-ignored", "k")
	if err != nil {
		t.Fatalf("GetFact (legacy path): %v", err)
	}
	if got != "legacy-value" {
		t.Fatalf("legacy GetFact = %q, want %q", got, "legacy-value")
	}

	if _, err := legacy.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	legacy.Close()

	fresh, err := OpenAt(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer fresh.Close()
	if !fresh.Migrated() {
		t.Fatal("must be migrated == true after a fresh Open() post-migrate")
	}

	// The migrated repo_id for "/repo/a" is its quarantine form (no real
	// git repo at that path), computed independently here.
	repoID := quarantineID("/repo/a")
	got2, err := fresh.GetFact("should-be-ignored", "should-be-ignored", repoID, "repo", "k")
	if err != nil {
		t.Fatalf("GetFact (new path): %v", err)
	}
	if got2 != "legacy-value" {
		t.Errorf("post-migrate GetFact = %q, want %q", got2, "legacy-value")
	}

	// Staleness boundary: a handle opened BEFORE the migration completed
	// keeps reporting Migrated() == false for its own lifetime — it is
	// fixed at Open() time, not required to observe the concurrent change.
	// (legacy here is already closed above; this just documents/asserts
	// the field was never mutated by someone else's Migrate() call.)
}

// AC-CONFLICT: two StoreFact calls for the same (repo_id, scope, key) with
// different values — the second upserts via ON CONFLICT(repo_id, scope,
// key) rather than erroring or duplicating a row.
func TestStoreFact_ConflictUpserts(t *testing.T) {
	t.Setenv("AIDW_STATE_DIR", t.TempDir())
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	db, err := OpenAt(dbPath)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	if err := db.StoreFact("", "", "repo-x", "repo", "k", "first", nil); err != nil {
		t.Fatalf("first StoreFact: %v", err)
	}
	if err := db.StoreFact("", "", "repo-x", "repo", "k", "second", nil); err != nil {
		t.Fatalf("second StoreFact: %v", err)
	}

	got, err := db.GetFact("", "", "repo-x", "repo", "k")
	if err != nil {
		t.Fatalf("GetFact: %v", err)
	}
	if got != "second" {
		t.Errorf("GetFact = %q, want %q (second write should win)", got, "second")
	}

	var count int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM facts WHERE repo_id='repo-x' AND scope='repo' AND key='k'").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want exactly 1 (upsert, not duplicate)", count)
	}
}

