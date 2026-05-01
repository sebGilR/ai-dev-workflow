package memory

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const (
	// VectorDimensions matches the default Google text-embedding-004 model.
	VectorDimensions = 768
)

// DB manages the local memory database.
type DB struct {
	conn          *sql.DB
	vectorEnabled bool
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

	registerDriver(home)

	conn, err := sql.Open(driverName, dbPath)
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
	// Standard Facts Table
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
		return fmt.Errorf("init facts: %w", err)
	}

	// Metadata table for indexed documents
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
		return fmt.Errorf("init items: %w", err)
	}

	// Check if vector extension is functional
	var vecVersion string
	err = db.conn.QueryRow("SELECT vec_version()").Scan(&vecVersion)
	if err == nil {
		db.vectorEnabled = true

		// Create virtual tables for vectors
		_, err = db.conn.Exec(fmt.Sprintf(`
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
	}

	return nil
}

func (db *DB) Close() error { return db.conn.Close() }

func (db *DB) VectorEnabled() bool { return db.vectorEnabled }

func (db *DB) Status() map[string]any {
	return map[string]any{
		"vector_extension_loaded": db.vectorEnabled,
		"database_connected":      db.conn != nil,
	}
}

// StoreFact saves a fact and its embedding if provided.
func (db *DB) StoreFact(repoPath, branch, key, value string, embedding []float32) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO facts (repo_path, branch, key, value)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(repo_path, branch, key) DO UPDATE SET value=excluded.value;
	`, repoPath, branch, key, value)
	if err != nil {
		return err
	}

	if db.vectorEnabled && len(embedding) == VectorDimensions {
		id, _ := res.LastInsertId()
		if id == 0 {
			// If ON CONFLICT happened, LastInsertId might be 0 on some sqlite versions/drivers.
			// Let's find the ID.
			_ = tx.QueryRow("SELECT id FROM facts WHERE repo_path=? AND branch=? AND key=?", repoPath, branch, key).Scan(&id)
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

// SearchResult represents a single semantic search result.
type SearchResult struct {
	RepoPath string  `json:"repo_path,omitempty"`
	FilePath string  `json:"file_path"`
	Content  string  `json:"content"`
	Distance float64 `json:"distance"`
}

// Search performs a semantic similarity search.
func (db *DB) Search(repoPath string, queryEmbedding []float32, k int) ([]SearchResult, error) {
	if !db.vectorEnabled {
		return nil, fmt.Errorf("vector search is currently disabled")
	}

	if len(queryEmbedding) != VectorDimensions {
		return nil, fmt.Errorf("invalid embedding dimensions: expected %d, got %d", VectorDimensions, len(queryEmbedding))
	}

	var rows *sql.Rows
	var err error

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

	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.RepoPath, &r.FilePath, &r.Content, &r.Distance); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// IndexItem stores a document and its embedding for search.
func (db *DB) IndexItem(repoPath, filePath, content string, embedding []float32) error {
	if !db.vectorEnabled {
		return nil
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO items (repo_path, file_path, content)
		VALUES (?, ?, ?)
		ON CONFLICT(repo_path, file_path) DO UPDATE SET content=excluded.content;
	`, repoPath, filePath, content)
	if err != nil {
		return err
	}

	id, _ := res.LastInsertId()
	if id == 0 {
		_ = tx.QueryRow("SELECT id FROM items WHERE repo_path=? AND file_path=?", repoPath, filePath).Scan(&id)
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
