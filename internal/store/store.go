// Package store keeps a small on-disk cache of the last scan, and exports
// result files. It uses plain JSON — no external database — to keep the
// binaries tiny and startup instant.
package store

import (
	"encoding/json"
	"fmt"
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
	if err := json.Unmarshal(data, &saved); err != nil || len(saved.Results) == 0 {
		var bare []proxy.Result
		if json.Unmarshal(data, &bare) == nil {
			saved.Results = bare
		}
	}
	for _, r := range saved.Results {
		d.byID[key(r.Kind, r.Addr)] = r
	}
	return d, nil
}

// Save upserts the given results and rewrites the cache file atomically.
func (d *DB) Save(rs []proxy.Result) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range rs {
		d.byID[key(r.Kind, r.Addr)] = r
	}
	return d.flush()
}

func (d *DB) flush() error {
	if dir := filepath.Dir(d.path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	list := make([]proxy.Result, 0, len(d.byID))
	for _, r := range d.byID {
		list = append(list, r)
	}
	data, err := json.Marshal(struct {
		Results []proxy.Result `json:"results"`
	}{list})
	if err != nil {
		return err
	}
	tmp := d.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, d.path) // atomic replace
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
	return nil
}
