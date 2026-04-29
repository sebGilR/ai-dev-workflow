package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// DB manages the local memory database.
type DB struct {
	conn *sql.DB
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

	conn, err := sql.Open("sqlite3", dbPath)
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
	// Facts Table
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
	return err
}

func (db *DB) Close() error { return db.conn.Close() }

func (db *DB) VectorEnabled() bool { return false }

func (db *DB) Status() map[string]any {
	return map[string]any{
		"vector_extension_loaded": false,
		"database_connected":      db.conn != nil,
	}
}

// StoreFact saves a fact.
func (db *DB) StoreFact(repoPath, branch, key, value string, embedding []float32) error {
	_, err := db.conn.Exec(`
		INSERT INTO facts (repo_path, branch, key, value)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(repo_path, branch, key) DO UPDATE SET value=excluded.value;
	`, repoPath, branch, key, value)
	return err
}

func (db *DB) GetFact(repoPath, branch, key string) (string, error) {
	var value string
	err := db.conn.QueryRow("SELECT value FROM facts WHERE repo_path=? AND branch=? AND key=?", repoPath, branch, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (db *DB) ListFacts(repoPath, branch string) (map[string]string, error) {
	var rows *sql.Rows
	var err error
	if repoPath == "" {
		rows, err = db.conn.Query("SELECT key, value FROM facts")
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

// Search dummy for now (no vector extension)
type SearchResult struct {
	RepoPath string  `json:"repo_path,omitempty"`
	FilePath string  `json:"file_path"`
	Content  string  `json:"content"`
	Distance float64 `json:"distance"`
}

func (db *DB) Search(repoPath string, queryEmbedding []float32, k int) ([]SearchResult, error) {
	return nil, fmt.Errorf("vector search is currently disabled due to build environment restrictions")
}

func (db *DB) IndexItem(repoPath, filePath, content string, embedding []float32) error {
	return nil // Skip indexing if vector search is disabled
}
