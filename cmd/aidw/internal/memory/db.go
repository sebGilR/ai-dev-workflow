package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mattn/go-sqlite3"
)

// DB manages the local memory database.
type DB struct {
	conn          *sql.DB
	vectorEnabled bool
}

func init() {
	// Register a custom driver that allows loading extensions
	sql.Register("sqlite3_with_extensions", &sqlite3.SQLiteDriver{
		Extensions: []string{},
	})
}

// Open initializes or opens the memory database at ~/.claude/memory.db
func Open() (*DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(home, ".claude", "memory.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite3_with_extensions", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.init(); err != nil {
		conn.Close()
		return nil, err
	}

	return db, nil
}

func (db *DB) init() error {
	// 1. Load extension
	if err := db.loadVectorExtension(); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: could not load sqlite-vec: %v\n", err)
		fmt.Fprintln(os.Stderr, "         Semantic search will be unavailable.")
	} else {
		db.vectorEnabled = true
	}

	// 2. Simple key-value store for facts
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
		return err
	}

	// 3. Vector table if enabled
	if db.vectorEnabled {
		// vec0 virtual table: id, repo_path, file_path, content, embedding
		// The embedding size depends on the model. Gemini embedding-004 is 768.
		_, err = db.conn.Exec(`
			CREATE VIRTUAL TABLE IF NOT EXISTS vec_items USING vec0(
				repo_path TEXT,
				file_path TEXT,
				content TEXT,
				embedding FLOAT[768]
			);
		`)
		return err
	}

	return nil
}

func (db *DB) loadVectorExtension() error {
	home, _ := os.UserHomeDir()
	libDir := filepath.Join(home, ".claude", "lib")
	extName := "vec0" // default for unix
	if runtime.GOOS == "darwin" {
		extName = "vec0.dylib"
	} else if runtime.GOOS == "linux" {
		extName = "vec0.so"
	}
	extPath := filepath.Join(libDir, extName)

	if _, err := os.Stat(extPath); err != nil {
		return fmt.Errorf("extension not found at %s", extPath)
	}

	// Load extension using raw connection
	return db.conn.QueryRow(fmt.Sprintf("SELECT load_extension('%s')", extPath)).Scan(new(interface{}))
}

func (db *DB) VectorEnabled() bool {
	return db.vectorEnabled
}

// Status returns a summary of the memory layer health.
func (db *DB) Status() map[string]any {
	return map[string]any{
		"vector_extension_loaded": db.vectorEnabled,
		"database_connected":      db.conn != nil,
	}
}

// IndexItem stores a content chunk and its vector into the virtual table.
func (db *DB) IndexItem(repoPath, filePath, content string, embedding []float32) error {
	if !db.vectorEnabled {
		return fmt.Errorf("vector extension not loaded")
	}

	// serialize embedding to JSON for sqlite-vec
	embJSON, err := json.Marshal(embedding)
	if err != nil {
		return err
	}

	_, err = db.conn.Exec(`
		INSERT INTO vec_items(repo_path, file_path, content, embedding)
		VALUES (?, ?, ?, ?)
	`, repoPath, filePath, content, embJSON)
	return err
}

// SearchResult represents a single hit from semantic search.
type SearchResult struct {
	FilePath string  `json:"file_path"`
	Content  string  `json:"content"`
	Distance float64 `json:"distance"`
}

// Search performs a vector search and returns the top K results.
func (db *DB) Search(repoPath string, queryEmbedding []float32, k int) ([]SearchResult, error) {
	if !db.vectorEnabled {
		return nil, fmt.Errorf("vector extension not loaded")
	}

	embJSON, _ := json.Marshal(queryEmbedding)

	// k-NN search using MATCH
	rows, err := db.conn.Query(`
		SELECT file_path, content, distance
		FROM vec_items
		WHERE repo_path = ? AND embedding MATCH ?
		ORDER BY distance
		LIMIT ?
	`, repoPath, embJSON, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.FilePath, &r.Content, &r.Distance); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}


func (db *DB) Close() error {
	return db.conn.Close()
}

// StoreFact saves a persistent fact for a specific branch.
func (db *DB) StoreFact(repoPath, branch, key, value string) error {
	_, err := db.conn.Exec(`
		INSERT INTO facts (repo_path, branch, key, value)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(repo_path, branch, key) DO UPDATE SET value=excluded.value;
	`, repoPath, branch, key, value)
	return err
}

// GetFact retrieves a fact.
func (db *DB) GetFact(repoPath, branch, key string) (string, error) {
	var value string
	err := db.conn.QueryRow("SELECT value FROM facts WHERE repo_path=? AND branch=? AND key=?", repoPath, branch, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// ListFacts returns all facts for a branch.
func (db *DB) ListFacts(repoPath, branch string) (map[string]string, error) {
	rows, err := db.conn.Query("SELECT key, value FROM facts WHERE repo_path=? AND branch=?", repoPath, branch)
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
