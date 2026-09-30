package launcher

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // the pure-Go sqlite driver: launcher.db stays the Rust file
)

const day = 86_400

// History is the persistent launch history, (mode, entry) -> uses and
// last use, in the same sqlite file and schema the Rust shell writes
// ($XDG_DATA_HOME/wayle/launcher.db), so an upgrade keeps it.
type History struct {
	db *sql.DB
}

// DataDir resolves wayle's data directory, $XDG_DATA_HOME/wayle or
// ~/.local/share/wayle, creating it (wayle-core ConfigPaths::data_dir).
func DataDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return "", errors.New("launcher: neither XDG_DATA_HOME nor HOME is set")
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, "wayle")
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // the shared data dir, as the Rust shell creates it
		return "", err
	}
	return dir, nil
}

// OpenHistory opens (creating if needed) the launcher history database.
func OpenHistory() (*History, error) {
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}
	return OpenHistoryAt(filepath.Join(dir, "launcher.db"))
}

// OpenHistoryAt opens a history database at an explicit path; tests
// use ":memory:".
func OpenHistoryAt(path string) (*History, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("history database: %w", err)
	}
	// One connection: a :memory: database is per connection, and the
	// launcher's writes are rare enough that serializing them is free.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;
		CREATE TABLE IF NOT EXISTS history (
			mode      TEXT NOT NULL,
			entry     TEXT NOT NULL,
			uses      INTEGER NOT NULL DEFAULT 1,
			last_used INTEGER NOT NULL,
			PRIMARY KEY (mode, entry)
		);`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("history database: %w", err)
	}
	return &History{db: db}, nil
}

// Close releases the database.
func (h *History) Close() error { return h.db.Close() }

// Record counts one launch, pruning the mode's history beyond maxSize.
func (h *History) Record(mode, entry string, maxSize uint32) error {
	return h.RecordAt(mode, entry, time.Now().Unix(), maxSize)
}

// RecordAt is Record with an explicit timestamp.
func (h *History) RecordAt(mode, entry string, now int64, maxSize uint32) error {
	if _, err := h.db.Exec(`INSERT INTO history (mode, entry, uses, last_used) VALUES (?1, ?2, 1, ?3)
		ON CONFLICT (mode, entry) DO UPDATE SET uses = uses + 1, last_used = ?3`, mode, entry, now); err != nil {
		return fmt.Errorf("history database: %w", err)
	}
	if _, err := h.db.Exec(`DELETE FROM history WHERE mode = ?1 AND entry NOT IN (
			SELECT entry FROM history WHERE mode = ?1
			ORDER BY last_used DESC, uses DESC LIMIT ?2
		)`, mode, maxSize); err != nil {
		return fmt.Errorf("history database: %w", err)
	}
	return nil
}

// Remove forgets one entry (rofi shift-delete).
func (h *History) Remove(mode, entry string) error {
	if _, err := h.db.Exec(`DELETE FROM history WHERE mode = ?1 AND entry = ?2`, mode, entry); err != nil {
		return fmt.Errorf("history database: %w", err)
	}
	return nil
}

// Frecency weights a mode's entries by uses x recency bucket; higher
// ranks earlier (drun pre-orders its rows by it).
func (h *History) Frecency(mode string) (map[string]float64, error) {
	return h.FrecencyAt(mode, time.Now().Unix())
}

// FrecencyAt is Frecency with an explicit "now".
func (h *History) FrecencyAt(mode string, now int64) (map[string]float64, error) {
	rows, err := h.db.Query(`SELECT entry, uses, last_used FROM history WHERE mode = ?1`, mode)
	if err != nil {
		return nil, fmt.Errorf("history database: %w", err)
	}
	defer func() { _ = rows.Close() }()
	weights := map[string]float64{}
	for rows.Next() {
		var entry string
		var uses, lastUsed int64
		if err := rows.Scan(&entry, &uses, &lastUsed); err != nil {
			return nil, fmt.Errorf("history database: %w", err)
		}
		weights[entry] = float64(uses) * recencyBucket(now-lastUsed)
	}
	return weights, rows.Err()
}

// Recent lists a mode's entries, most recently used first (rofi run
// ordering).
func (h *History) Recent(mode string) ([]string, error) {
	rows, err := h.db.Query(`SELECT entry FROM history WHERE mode = ?1 ORDER BY last_used DESC`, mode)
	if err != nil {
		return nil, fmt.Errorf("history database: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			return nil, fmt.Errorf("history database: %w", err)
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

// recencyBucket is the Mozilla-style recency weight.
func recencyBucket(age int64) float64 {
	switch {
	case age <= 4*day:
		return 1.0
	case age <= 14*day:
		return 0.7
	case age <= 31*day:
		return 0.5
	case age <= 90*day:
		return 0.3
	default:
		return 0.1
	}
}
