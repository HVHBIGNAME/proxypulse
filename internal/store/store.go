// Package store persists scan results to a local SQLite database and exports files.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	_ "modernc.org/sqlite"
)

// DB wraps the results database.
type DB struct{ sql *sql.DB }

// Open opens (creating if needed) the database at the given path.
func Open(path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	d := &DB{sql: db}
	if err := d.migrate(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *DB) migrate() error {
	_, err := d.sql.Exec(`
CREATE TABLE IF NOT EXISTS results (
  addr       TEXT NOT NULL,
  kind       TEXT NOT NULL,
  source     TEXT,
  alive      INTEGER,
  latency_ms INTEGER,
  speed_kbs  INTEGER,
  exit_ip    TEXT,
  country    TEXT,
  anonymity  TEXT,
  err        TEXT,
  checked_at TEXT,
  PRIMARY KEY (kind, addr)
);
CREATE INDEX IF NOT EXISTS idx_alive_latency ON results(alive, latency_ms);`)
	return err
}

// Save upserts results (keeps only the most recent row per endpoint).
func (d *DB) Save(rs []proxy.Result) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO results
      (addr,kind,source,alive,latency_ms,speed_kbs,exit_ip,country,anonymity,err,checked_at)
      VALUES (?,?,?,?,?,?,?,?,?,?,?)
      ON CONFLICT(kind,addr) DO UPDATE SET
        source=excluded.source, alive=excluded.alive, latency_ms=excluded.latency_ms,
        speed_kbs=excluded.speed_kbs, exit_ip=excluded.exit_ip, country=excluded.country,
        anonymity=excluded.anonymity, err=excluded.err, checked_at=excluded.checked_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rs {
		if _, err := stmt.Exec(r.Addr, r.Kind, r.Source, boolInt(r.Alive), r.LatencyMs,
			r.SpeedKBs, r.ExitIP, r.Country, r.Anonymity, r.Err, r.CheckedAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Top returns the n fastest alive proxies previously stored.
func (d *DB) Top(n int) ([]proxy.Result, error) {
	rows, err := d.sql.Query(`SELECT addr,kind,source,latency_ms,speed_kbs,exit_ip,country,anonymity,checked_at
      FROM results WHERE alive=1 ORDER BY latency_ms ASC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []proxy.Result
	for rows.Next() {
		var r proxy.Result
		var ts string
		if err := rows.Scan(&r.Addr, &r.Kind, &r.Source, &r.LatencyMs, &r.SpeedKBs,
			&r.ExitIP, &r.Country, &r.Anonymity, &ts); err != nil {
			return nil, err
		}
		r.Alive = true
		r.CheckedAt, _ = time.Parse(time.RFC3339, ts)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Count returns alive/total counts.
func (d *DB) Count() (alive, total int, err error) {
	err = d.sql.QueryRow(`SELECT
      COALESCE(SUM(alive),0), COUNT(*) FROM results`).Scan(&alive, &total)
	return
}

// Close closes the database.
func (d *DB) Close() error { return d.sql.Close() }

// ExportJSON writes results as JSON.
func ExportJSON(path string, rs []proxy.Result) error {
	data, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ExportText writes one proxy URL per line (fastest first) — the "copy all" file.
func ExportText(path string, rs []proxy.Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, r := range rs {
		if _, err := fmt.Fprintln(f, r.URL()); err != nil {
			return err
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
