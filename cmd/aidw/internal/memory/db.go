package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"aidw/cmd/aidw/internal/state"
)

const (
	// VectorDimensions matches the default Google text-embedding-004 model.
	VectorDimensions = 768

	// FactsScope is the constant scope value every facts row uses, migrated
	// and newly-written alike (§2c decision 1) — this package owns the
	// schema, so it owns this value; cmd/memory.go references it rather than
	// declaring its own copy, and rebuildToV2's INSERT binds it as a
	// parameter rather than inlining the literal, so there is exactly one
	// occurrence of the string in the codebase. Changing this value without
	// also changing every already-migrated row's stored scope would make
	// every migrated fact silently unreadable — see rebuildToV2's use.
	FactsScope = "repo"

	// memorySchemaVersion is the PRAGMA user_version value of the D1-keyed
	// (repo_id/scope) schema. Version 1 is implicitly "the original
	// repo_path/branch-keyed schema, never explicitly numbered until now" —
	// there is no version-1 constant because no DB has ever reported it;
	// a legacy DB simply reports 0 (SQLite's default for a pragma that was
	// never set), which is exactly how Open() tells "brand-new" apart from
	// "pre-versioning legacy" (see init()).
	memorySchemaVersion = 2
)

// DB manages the local memory database.
type DB struct {
	conn          *sql.DB
	dbPath        string
	vectorEnabled bool
	// migrated reflects PRAGMA user_version == memorySchemaVersion as of
	// this DB's Open() call. It is a schema-level switch, not a per-row
	// fallback (Task B.6): every query-issuing method branches on this one
	// field for the lifetime of the handle. A concurrent `memory migrate`
	// run in another process does not update an already-open handle's
	// value — that is expected (AC-DUALREAD), not a race to fix.
	migrated bool
}

// Open initializes or opens the memory database at ~/.claude/memory.db.
func Open() (*DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return open(filepath.Join(home, ".claude", "memory.db"), home)
}

// OpenAt opens (or initializes) the memory database at an explicit dbPath,
// bypassing Open's hard-coded ~/.claude/memory.db location. Open() itself
// has no path parameter, so tests that need an isolated on-disk database
// (this lane's schema-rebuild tests, which must seed a legacy-schema file
// before opening it) call this instead.
func OpenAt(dbPath string) (*DB, error) {
	// Best-effort only: home is used solely to locate the optional vec
	// extension shared library (driver_sqlite_ext.go's registerDriver) and
	// its absence is never fatal — a build without that build tag ignores
	// it entirely.
	home, _ := os.UserHomeDir()
	return open(dbPath, home)
}

func open(dbPath, home string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	registerDriver(home)

	conn, err := sql.Open(driverName, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db := &DB{conn: conn, dbPath: dbPath}
	if err := db.init(); err != nil {
		conn.Close()
		return nil, err
	}

	return db, nil
}

// init decides, once per Open(), which schema this DB file is on and never
// rebuilds one into the other (§2c decision 2 / Task B.1) — the rebuild
// (Task B.2) runs only from the explicit `aidw memory migrate` command
// (Migrate, below).
func (db *DB) init() error {
	ver, err := db.userVersion()
	if err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	switch {
	case ver == 0:
		legacyExists, err := db.tableExists("facts")
		if err != nil {
			return err
		}
		if !legacyExists {
			// A brand-new, empty database file must never pass through the
			// old schema first — create the new D1-keyed schema directly
			// and set the version now, in one step.
			if err := db.createSchemaV2(); err != nil {
				return err
			}
			if err := db.setUserVersion(memorySchemaVersion); err != nil {
				return err
			}
			db.migrated = true
		} else {
			// A pre-versioning DB that already has legacy-shaped tables.
			// Open() leaves it exactly as-is: legacy schema, repo_path/
			// branch columns, version 0. Only `aidw memory migrate` (Task
			// B.1b, Migrate below) ever rebuilds it.
			if err := db.createLegacySchema(); err != nil {
				return err
			}
			db.migrated = false
		}
	case ver == memorySchemaVersion:
		// Already on the new schema (a prior fresh-init or a completed
		// `memory migrate` run). CREATE TABLE IF NOT EXISTS is a no-op here
		// in the overwhelmingly common case; it only matters for a
		// hand-crafted or corrupted DB that reports the new version but is
		// missing a table, which is not a state this migration lane is
		// responsible for repairing further than "don't crash".
		if err := db.createSchemaV2(); err != nil {
			return err
		}
		db.migrated = true
	default:
		return fmt.Errorf("memory db: unsupported schema version %d", ver)
	}

	return db.initVectorTables()
}

func (db *DB) userVersion() (int, error) {
	var v int
	if err := db.conn.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

func (db *DB) setUserVersion(v int) error {
	// PRAGMA user_version does not accept a bound parameter for its value;
	// v is always the memorySchemaVersion constant, never user input, so
	// interpolating it directly carries no injection risk.
	_, err := db.conn.Exec(fmt.Sprintf("PRAGMA user_version = %d", v))
	return err
}

func (db *DB) tableExists(name string) (bool, error) {
	var n string
	err := db.conn.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (db *DB) createLegacySchema() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS facts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_path TEXT,
			branch TEXT,
			key TEXT,
			value TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(repo_path, branch, key)
		);
	`)
	if err != nil {
		return fmt.Errorf("init legacy facts: %w", err)
	}

	_, err = db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_path TEXT,
			file_path TEXT,
			content TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(repo_path, file_path)
		);
	`)
	if err != nil {
		return fmt.Errorf("init legacy items: %w", err)
	}
	return nil
}

func (db *DB) createSchemaV2() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS facts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id TEXT,
			scope TEXT,
			key TEXT,
			value TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(repo_id, scope, key)
		);
	`)
	if err != nil {
		return fmt.Errorf("init facts: %w", err)
	}

	_, err = db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id TEXT,
			file_path TEXT,
			content TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(repo_id, file_path)
		);
	`)
	if err != nil {
		return fmt.Errorf("init items: %w", err)
	}
	return nil
}

func (db *DB) initVectorTables() error {
	// Check if vector extension is functional.
	var vecVersion string
	if err := db.conn.QueryRow("SELECT vec_version()").Scan(&vecVersion); err != nil {
		// Not available in this build/environment — normal, not degraded
		// (Task B.8). vec_facts/vec_items simply do not exist as tables.
		return nil
	}
	db.vectorEnabled = true

	_, err := db.conn.Exec(fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vec_facts USING vec0(
			id INTEGER PRIMARY KEY,
			embedding FLOAT[%d]
		);
	`, VectorDimensions))
	if err != nil {
		return fmt.Errorf("init vec_facts: %w", err)
	}

	_, err = db.conn.Exec(fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vec_items USING vec0(
			id INTEGER PRIMARY KEY,
			embedding FLOAT[%d]
		);
	`, VectorDimensions))
	if err != nil {
		return fmt.Errorf("init vec_items: %w", err)
	}
	return nil
}

func (db *DB) Close() error { return db.conn.Close() }

func (db *DB) VectorEnabled() bool { return db.vectorEnabled }

// Migrated reports whether this handle's DB file is on the new repo_id/
// scope schema (PRAGMA user_version == memorySchemaVersion) as of Open().
func (db *DB) Migrated() bool { return db.migrated }

func (db *DB) Status() map[string]any {
	status := map[string]any{
		"vector_extension_loaded": db.vectorEnabled,
		"database_connected":      db.conn != nil,
		"migrated":                db.migrated,
	}
	if !db.migrated {
		status["hint"] = "run 'aidw memory migrate' to move to the new schema"
	}
	return status
}

// MigrationSummary reports the outcome of an `aidw memory migrate` run
// (Task B.1b).
type MigrationSummary struct {
	AlreadyMigrated bool   `json:"already_migrated"`
	FactsMigrated   int    `json:"facts_migrated,omitempty"`
	FactsCollapsed  int    `json:"facts_collapsed,omitempty"`
	ItemsMigrated   int    `json:"items_migrated,omitempty"`
	Quarantined     int    `json:"quarantined_repos,omitempty"`
	BackupPath      string `json:"backup_path,omitempty"`
}

// Migrate rebuilds this DB onto the new repo_id/scope schema (Task B.1b).
// It is the ONLY thing that ever triggers Task B.2's rebuild — Open() never
// does (§2c decision 2). Idempotent: a DB already on the new schema reports
// AlreadyMigrated and takes no lock.
func (db *DB) Migrate() (*MigrationSummary, error) {
	if db.migrated {
		return &MigrationSummary{AlreadyMigrated: true}, nil
	}

	exists, err := db.tableExists("facts")
	if err != nil {
		return nil, fmt.Errorf("memory migrate: check legacy schema: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("memory migrate: no legacy facts table found on an unmigrated database")
	}

	// Serializes concurrent `memory migrate` invocations at the process
	// level (hard rule 6: reuse state.AcquireLock, don't reinvent). Task
	// B.2's BEGIN IMMEDIATE additionally takes SQLite's own write lock, so
	// this is belt-and-suspenders with that, not the only thing preventing
	// two rebuilds from racing.
	release, err := state.AcquireLock(db.dbPath)
	if err != nil {
		return nil, fmt.Errorf("memory migrate: %w", err)
	}
	defer release()

	// Re-check under the lock: another process could have completed a
	// migration between our first check above and acquiring the lock.
	ver, err := db.userVersion()
	if err != nil {
		return nil, fmt.Errorf("memory migrate: re-check user_version: %w", err)
	}
	if ver == memorySchemaVersion {
		db.migrated = true
		return &MigrationSummary{AlreadyMigrated: true}, nil
	}

	// Copy memory.db to memory.db.pre-v2.bak BEFORE opening a transaction —
	// the last-resort recovery path if the transaction is somehow corrupted
	// at the SQLite file level, not just an app-level bug. Retained after a
	// successful migration; never auto-deleted (Task B.1b).
	backupPath := db.dbPath + ".pre-v2.bak"
	if err := copyFilePlain(db.dbPath, backupPath); err != nil {
		return nil, fmt.Errorf("memory migrate: backup: %w", err)
	}

	counts, err := db.rebuildToV2(context.Background())
	if err != nil {
		return nil, fmt.Errorf("memory migrate: rebuild: %w", err)
	}

	db.migrated = true

	return &MigrationSummary{
		FactsMigrated:  counts.factsMigrated,
		FactsCollapsed: counts.factsCollapsed,
		ItemsMigrated:  counts.itemsMigrated,
		Quarantined:    counts.quarantined,
		BackupPath:     backupPath,
	}, nil
}

// copyFilePlain copies a single file byte-for-byte — the pre-migration
// memory.db backup (Task B.1b). util.CopyFS (util/util.go) operates over an
// fs.FS plus a destination directory, which doesn't fit copying one named
// file to another named file, so this is a small single-file analogue of
// the same "plain file I/O, not a work-record write" style.
func copyFilePlain(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// migrationCounts holds row-level statistics from one rebuildToV2 run.
type migrationCounts struct {
	factsMigrated  int
	factsCollapsed int
	itemsMigrated  int
	// quarantined counts distinct legacy repo_path values that failed
	// state.RepoIdentity resolution (a deleted/moved clone), not rows —
	// "quarantined-repo count" per Task B.1b's summary wording.
	quarantined int
}

// testHookAfterDropFacts, when non-nil, is invoked immediately after
// `DROP TABLE facts` but before the rename/version-bump/COMMIT that follow
// it. It exists solely for AC-REBUILD-ATOMIC (db_migrate_test.go) to
// simulate a crash mid-transaction without actually killing the process,
// which SQLite test harnesses can't reliably do — the spec's own suggested
// approach. Always nil in production.
var testHookAfterDropFacts func() error

// rebuildToV2 runs Task B.2's entire rebuild — both tables' CREATE/INSERT/
// DROP/RENAME plus the PRAGMA user_version bump — inside one
// BEGIN IMMEDIATE … COMMIT transaction (AC-REBUILD-ATOMIC). It uses a
// single dedicated *sql.Conn (not db.conn.Begin()'s *sql.Tx) because
// database/sql's Tx has no portable way to request BEGIN IMMEDIATE instead
// of a deferred transaction; issuing "BEGIN IMMEDIATE"/"COMMIT"/"ROLLBACK"
// as literal statements against one held connection gets the same
// guarantee directly. BEGIN IMMEDIATE takes SQLite's write lock up front,
// which doubles as Migrate's serialization guarantee at the SQLite level,
// on top of state.AcquireLock at the process level.
func (db *DB) rebuildToV2(ctx context.Context) (migrationCounts, error) {
	var counts migrationCounts

	conn, err := db.conn.Conn(ctx)
	if err != nil {
		return counts, fmt.Errorf("acquire dedicated conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return counts, fmt.Errorf("begin immediate: %w", err)
	}
	fail := func(cause error) (migrationCounts, error) {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		return migrationCounts{}, cause
	}

	repoIDCache := map[string]string{}
	resolveRepoID := func(repoPath string) string {
		if id, ok := repoIDCache[repoPath]; ok {
			return id
		}
		id, err := state.RepoIdentity(repoPath)
		if err != nil {
			// Deleted/moved clone: quarantine using the same derivation
			// shape as state's derivedRepoID (sha256, first 16 hex chars),
			// computed over the legacy repo_path string exactly as stored
			// — it cannot be canonicalized, the path is gone. Never
			// dropped: every other column is preserved.
			sum := sha256.Sum256([]byte(repoPath))
			id = "unresolved:" + hex.EncodeToString(sum[:])[:16]
			counts.quarantined++
		}
		repoIDCache[repoPath] = id
		return id
	}

	// --- facts ---

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE facts_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id TEXT,
			scope TEXT,
			key TEXT,
			value TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(repo_id, scope, key)
		);
	`); err != nil {
		return fail(fmt.Errorf("create facts_new: %w", err))
	}

	type legacyFact struct {
		id                                       int64
		repoPath, branch, key, value, createdAt string
	}
	rows, err := conn.QueryContext(ctx, "SELECT id, repo_path, branch, key, value, created_at FROM facts")
	if err != nil {
		return fail(fmt.Errorf("read legacy facts: %w", err))
	}
	var legacyFacts []legacyFact
	for rows.Next() {
		var lf legacyFact
		if err := rows.Scan(&lf.id, &lf.repoPath, &lf.branch, &lf.key, &lf.value, &lf.createdAt); err != nil {
			rows.Close()
			return fail(fmt.Errorf("scan legacy fact: %w", err))
		}
		legacyFacts = append(legacyFacts, lf)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fail(fmt.Errorf("iterate legacy facts: %w", err))
	}
	rows.Close()

	for _, lf := range legacyFacts {
		repoID := resolveRepoID(lf.repoPath)
		// id is carried EXPLICITLY, in both the column list and the
		// values, rather than a bare "INSERT ... SELECT ... FROM facts"
		// that would let SQLite assign a fresh AUTOINCREMENT id. This one
		// column is the entire R1 mitigation: vec_facts(id, embedding) is
		// joined to facts by id with NO foreign key (see Search's
		// "JOIN items i ON v.id = i.id" below, and the analogous
		// vec_facts lookups in StoreFact) — a fresh id here would silently
		// re-point an existing embedding at a different fact row: no
		// error, no crash, wrong answers forever. This rebuild never reads
		// or writes vec_facts/vec_items at all; id preservation here is
		// what makes that omission safe.
		//
		// Scope collapses to the constant FactsScope for every row (§2c
		// decision 1) — the legacy branch value is read above but not
		// encoded into scope. Bound as a parameter, not inlined as a bare
		// 'repo' literal, so this is the single source of that value in the
		// codebase (see FactsScope's doc comment) — an inlined literal here
		// could silently drift from cmd/memory.go's copy and make every
		// migrated fact permanently unreadable with no error anywhere. Two
		// legacy rows for the same (repo_id, "repo", key) therefore collide
		// on the new UNIQUE constraint; ON CONFLICT DO UPDATE keeps
		// whichever row has the later created_at (ties broken by higher
		// legacy id) via the WHERE guard, and facts_collapsed (below) is
		// derived from the resulting row-count delta.
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO facts_new (id, repo_id, scope, key, value, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(repo_id, scope, key) DO UPDATE SET
				id = excluded.id,
				value = excluded.value,
				created_at = excluded.created_at
			WHERE excluded.created_at > facts_new.created_at
			   OR (excluded.created_at = facts_new.created_at AND excluded.id > facts_new.id);
		`, lf.id, repoID, FactsScope, lf.key, lf.value, lf.createdAt); err != nil {
			return fail(fmt.Errorf("insert facts_new row (legacy id %d): %w", lf.id, err))
		}
	}

	var factsNewCount int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM facts_new").Scan(&factsNewCount); err != nil {
		return fail(fmt.Errorf("count facts_new: %w", err))
	}
	counts.factsMigrated = factsNewCount
	counts.factsCollapsed = len(legacyFacts) - factsNewCount

	if _, err := conn.ExecContext(ctx, "DROP TABLE facts"); err != nil {
		return fail(fmt.Errorf("drop facts: %w", err))
	}

	if testHookAfterDropFacts != nil {
		if err := testHookAfterDropFacts(); err != nil {
			return fail(err)
		}
	}

	if _, err := conn.ExecContext(ctx, "ALTER TABLE facts_new RENAME TO facts"); err != nil {
		return fail(fmt.Errorf("rename facts_new: %w", err))
	}

	// --- items ---

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE items_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id TEXT,
			file_path TEXT,
			content TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(repo_id, file_path)
		);
	`); err != nil {
		return fail(fmt.Errorf("create items_new: %w", err))
	}

	type legacyItem struct {
		id                                     int64
		repoPath, filePath, content, createdAt string
	}
	irows, err := conn.QueryContext(ctx, "SELECT id, repo_path, file_path, content, created_at FROM items")
	if err != nil {
		return fail(fmt.Errorf("read legacy items: %w", err))
	}
	var legacyItems []legacyItem
	for irows.Next() {
		var li legacyItem
		if err := irows.Scan(&li.id, &li.repoPath, &li.filePath, &li.content, &li.createdAt); err != nil {
			irows.Close()
			return fail(fmt.Errorf("scan legacy item: %w", err))
		}
		legacyItems = append(legacyItems, li)
	}
	if err := irows.Err(); err != nil {
		irows.Close()
		return fail(fmt.Errorf("iterate legacy items: %w", err))
	}
	irows.Close()

	for _, li := range legacyItems {
		repoID := resolveRepoID(li.repoPath)
		// Same id-preservation requirement as facts_new above: vec_items is
		// joined to items by id with no foreign key (Search's
		// "JOIN items i ON v.id = i.id"). Carrying id explicitly here is
		// what keeps that join correct across this rebuild.
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO items_new (id, repo_id, file_path, content, created_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(repo_id, file_path) DO UPDATE SET
				id = excluded.id,
				content = excluded.content,
				created_at = excluded.created_at
			WHERE excluded.created_at > items_new.created_at
			   OR (excluded.created_at = items_new.created_at AND excluded.id > items_new.id);
		`, li.id, repoID, li.filePath, li.content, li.createdAt); err != nil {
			return fail(fmt.Errorf("insert items_new row (legacy id %d): %w", li.id, err))
		}
	}

	var itemsNewCount int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM items_new").Scan(&itemsNewCount); err != nil {
		return fail(fmt.Errorf("count items_new: %w", err))
	}
	counts.itemsMigrated = itemsNewCount

	if _, err := conn.ExecContext(ctx, "DROP TABLE items"); err != nil {
		return fail(fmt.Errorf("drop items: %w", err))
	}
	if _, err := conn.ExecContext(ctx, "ALTER TABLE items_new RENAME TO items"); err != nil {
		return fail(fmt.Errorf("rename items_new: %w", err))
	}

	// The version bump is the LAST statement before COMMIT, in the same
	// transaction as the renames above (AC-REBUILD-ATOMIC) — never a
	// separate statement issued after COMMIT. PRAGMA user_version writes
	// are transactional in SQLite (they roll back with the rest of the
	// transaction), which is exactly what makes the atomicity guarantee
	// hold for the version number too, not just the table contents.
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", memorySchemaVersion)); err != nil {
		return fail(fmt.Errorf("set user_version: %w", err))
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fail(fmt.Errorf("commit: %w", err))
	}

	return counts, nil
}

// StoreFact saves a fact and its embedding if provided. Callers pass both
// the legacy (repoPath, branch) and new (repoID, scope) identifying values;
// which pair is actually used in the WHERE/INSERT clause depends on
// db.migrated (Task B.6) — there is no third code path that references
// both schemas in one call.
func (db *DB) StoreFact(repoPath, branch, repoID, scope, key, value string, embedding []float32) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var res sql.Result
	if db.migrated {
		res, err = tx.Exec(`
			INSERT INTO facts (repo_id, scope, key, value)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(repo_id, scope, key) DO UPDATE SET value=excluded.value;
		`, repoID, scope, key, value)
	} else {
		res, err = tx.Exec(`
			INSERT INTO facts (repo_path, branch, key, value)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(repo_path, branch, key) DO UPDATE SET value=excluded.value;
		`, repoPath, branch, key, value)
	}
	if err != nil {
		return err
	}

	if db.vectorEnabled && len(embedding) == VectorDimensions {
		id, _ := res.LastInsertId()
		if id == 0 {
			// If ON CONFLICT happened, LastInsertId might be 0 on some sqlite versions/drivers.
			// Let's find the ID.
			if db.migrated {
				_ = tx.QueryRow("SELECT id FROM facts WHERE repo_id=? AND scope=? AND key=?", repoID, scope, key).Scan(&id)
			} else {
				_ = tx.QueryRow("SELECT id FROM facts WHERE repo_path=? AND branch=? AND key=?", repoPath, branch, key).Scan(&id)
			}
		}
		if id > 0 {
			_, _ = tx.Exec("DELETE FROM vec_facts WHERE id=?", id)
			_, err = tx.Exec("INSERT INTO vec_facts(id, embedding) VALUES(?, ?)", id, float32ToByteSlice(embedding))
			if err != nil {
				return fmt.Errorf("store vec_fact: %w", err)
			}
		}
	}

	return tx.Commit()
}

func (db *DB) GetFact(repoPath, branch, repoID, scope, key string) (string, error) {
	var value string
	var err error
	if db.migrated {
		err = db.conn.QueryRow("SELECT value FROM facts WHERE repo_id=? AND scope=? AND key=?", repoID, scope, key).Scan(&value)
	} else {
		err = db.conn.QueryRow("SELECT value FROM facts WHERE repo_path=? AND branch=? AND key=?", repoPath, branch, key).Scan(&value)
	}
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (db *DB) ListFacts(repoPath, branch, repoID, scope string) (map[string]string, error) {
	var rows *sql.Rows
	var err error
	if repoPath == "" {
		// Global listing (`memory list --global`) — no WHERE clause at
		// all, unchanged by the D1 re-key (Task B.5's explicit note: this
		// branch needs no filter and never needed a repo_id/scope
		// parameter). repoPath=="" is the ONLY signal for this branch — a
		// caller wanting a LOCAL (non-global) listing must always pass its
		// real resolved repo path here even on the migrated schema, where
		// the path itself is otherwise unused in the query below; passing
		// "" with a real repoID/scope would silently fall into this global
		// branch instead of filtering by repo_id (memory.go's callers
		// always resolve a real path via git.Toplevel before calling this
		// unless --global was explicitly requested, so this is not
		// reachable in practice today — noted here so it stays that way).
		rows, err = db.conn.Query("SELECT key, value FROM facts")
	} else if db.migrated {
		rows, err = db.conn.Query("SELECT key, value FROM facts WHERE repo_id=? AND scope=?", repoID, scope)
	} else {
		rows, err = db.conn.Query("SELECT key, value FROM facts WHERE repo_path=? AND branch=?", repoPath, branch)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	facts := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		facts[k] = v
	}
	return facts, nil
}

// SearchResult represents a single semantic search result.
type SearchResult struct {
	RepoID   string  `json:"repo_id,omitempty"`
	FilePath string  `json:"file_path"`
	Content  string  `json:"content"`
	Distance float64 `json:"distance"`
}

// Search performs a semantic similarity search.
func (db *DB) Search(repoPath, repoID string, queryEmbedding []float32, k int) ([]SearchResult, error) {
	if !db.vectorEnabled {
		return nil, fmt.Errorf("vector search is currently disabled")
	}

	if len(queryEmbedding) != VectorDimensions {
		return nil, fmt.Errorf("invalid embedding dimensions: expected %d, got %d", VectorDimensions, len(queryEmbedding))
	}

	var rows *sql.Rows
	var err error

	if db.migrated {
		sqlQuery := `
			SELECT
				i.repo_id,
				i.file_path,
				i.content,
				v.distance
			FROM vec_items v
			JOIN items i ON v.id = i.id
			WHERE v.embedding MATCH ?
			AND k = ?
		`
		if repoID != "" {
			sqlQuery += " AND i.repo_id = ?"
			rows, err = db.conn.Query(sqlQuery, float32ToByteSlice(queryEmbedding), k, repoID)
		} else {
			rows, err = db.conn.Query(sqlQuery, float32ToByteSlice(queryEmbedding), k)
		}
	} else {
		sqlQuery := `
			SELECT
				i.repo_path,
				i.file_path,
				i.content,
				v.distance
			FROM vec_items v
			JOIN items i ON v.id = i.id
			WHERE v.embedding MATCH ?
			AND k = ?
		`
		if repoPath != "" {
			sqlQuery += " AND i.repo_path = ?"
			rows, err = db.conn.Query(sqlQuery, float32ToByteSlice(queryEmbedding), k, repoPath)
		} else {
			rows, err = db.conn.Query(sqlQuery, float32ToByteSlice(queryEmbedding), k)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.RepoID, &r.FilePath, &r.Content, &r.Distance); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// IndexItem stores a document and its embedding for search.
func (db *DB) IndexItem(repoPath, repoID, filePath, content string, embedding []float32) error {
	if !db.vectorEnabled {
		return nil
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var res sql.Result
	if db.migrated {
		res, err = tx.Exec(`
			INSERT INTO items (repo_id, file_path, content)
			VALUES (?, ?, ?)
			ON CONFLICT(repo_id, file_path) DO UPDATE SET content=excluded.content;
		`, repoID, filePath, content)
	} else {
		res, err = tx.Exec(`
			INSERT INTO items (repo_path, file_path, content)
			VALUES (?, ?, ?)
			ON CONFLICT(repo_path, file_path) DO UPDATE SET content=excluded.content;
		`, repoPath, filePath, content)
	}
	if err != nil {
		return err
	}

	id, _ := res.LastInsertId()
	if id == 0 {
		if db.migrated {
			_ = tx.QueryRow("SELECT id FROM items WHERE repo_id=? AND file_path=?", repoID, filePath).Scan(&id)
		} else {
			_ = tx.QueryRow("SELECT id FROM items WHERE repo_path=? AND file_path=?", repoPath, filePath).Scan(&id)
		}
	}

	if id > 0 {
		_, _ = tx.Exec("DELETE FROM vec_items WHERE id=?", id)
		_, err = tx.Exec("INSERT INTO vec_items(id, embedding) VALUES(?, ?)", id, float32ToByteSlice(embedding))
		if err != nil {
			return fmt.Errorf("index vec_item: %w", err)
		}
	}

	return tx.Commit()
}

func float32ToByteSlice(f []float32) []byte {
	buf := make([]byte, len(f)*4)
	for i, v := range f {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}
