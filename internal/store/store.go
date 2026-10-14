// Package store keeps a small on-disk cache of the last scan, and exports
// result files. It uses plain JSON — no external database — to keep the
// binaries tiny and startup instant.
package store

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
)

// DB is an in-memory result cache backed by a compact JSON file.
type DB struct {
	path string
	mu   sync.Mutex
	byID map[string]proxy.Result
}

func key(kind, addr string) string { return kind + "|" + addr }

// Open loads the cache at path, creating an empty one if it does not exist.
func Open(path string) (*DB, error) {
	d := &DB{path: path, byID: make(map[string]proxy.Result)}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return d, nil
		}
		return nil, err
	}
	var saved struct {
		Results []proxy.Result `json:"results"`
	}
	// Tolerate a bare array as well as the wrapper object.
	if err := json.Unmarshal(data, &saved); err != nil {
		var bare []proxy.Result
		if err := json.Unmarshal(data, &bare); err != nil {
			return nil, fmt.Errorf("decode cache: %w", err)
		}
		saved.Results = bare
	}
	for _, r := range saved.Results {
		upsert(d.byID, r)
	}
	return d, nil
}

// Save upserts the given results and rewrites the cache file atomically.
func (d *DB) Save(rs []proxy.Result) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	updated := maps.Clone(d.byID)
	for _, r := range rs {
		upsert(updated, r)
	}
	if err := d.flush(updated); err != nil {
		return err
	}
	d.byID = updated
	return nil
}

func upsert(rows map[string]proxy.Result, r proxy.Result) {
	id := key(r.Kind, r.Addr)
	if old, ok := rows[id]; ok && old.CheckedAt.After(r.CheckedAt) {
		return
	}
	rows[id] = r
}

func (d *DB) flush(rows map[string]proxy.Result) error {
	if dir := filepath.Dir(d.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	list := make([]proxy.Result, 0, len(rows))
	for _, r := range rows {
		list = append(list, r)
	}
	data, err := json.Marshal(struct {
		Results []proxy.Result `json:"results"`
	}{list})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.path), filepath.Base(d.path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), d.path)
}

// Top returns the n fastest alive proxies.
func (d *DB) Top(n int) ([]proxy.Result, error) {
	d.mu.Lock()
	alive := make([]proxy.Result, 0, len(d.byID))
	for _, r := range d.byID {
		if r.Alive {
			alive = append(alive, r)
		}
	}
	d.mu.Unlock()
	sort.Slice(alive, func(i, j int) bool { return alive[i].LatencyMs < alive[j].LatencyMs })
	if n > 0 && len(alive) > n {
		alive = alive[:n]
	}
	return alive, nil
}

// Count returns alive and total row counts.
func (d *DB) Count() (alive, total int, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range d.byID {
		total++
		if r.Alive {
			alive++
		}
	}
	return alive, total, nil
}

// Close is a no-op; the cache is flushed on every Save.
func (d *DB) Close() error { return nil }

// ExportJSON writes results as pretty JSON.
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
	return f.Close()
}
