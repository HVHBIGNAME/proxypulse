package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
)

func TestSaveTopCountAndExport(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "t.json"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	rows := []proxy.Result{
		{Addr: "1.1.1.1:80", Kind: "http", Alive: true, LatencyMs: 900, CheckedAt: time.Now()},
		{Addr: "2.2.2.2:80", Kind: "http", Alive: true, LatencyMs: 400, CheckedAt: time.Now()},
		{Addr: "3.3.3.3:80", Kind: "http", Alive: false, Err: "timeout", CheckedAt: time.Now()},
	}
	if err := db.Save(rows); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Re-save the fastest one with a new latency: upsert must keep one row.
	rows[1].LatencyMs = 350
	if err := db.Save(rows[1:2]); err != nil {
		t.Fatalf("resave: %v", err)
	}

	alive, total, err := db.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if alive != 2 || total != 3 {
		t.Fatalf("count = (%d alive, %d total), want (2, 3)", alive, total)
	}

	top, err := db.Top(10)
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	if len(top) != 2 || top[0].Addr != "2.2.2.2:80" || top[0].LatencyMs != 350 {
		t.Fatalf("top unexpected: %+v", top)
	}

	txt := filepath.Join(dir, "out.txt")
	if err := ExportText(txt, top); err != nil {
		t.Fatalf("export text: %v", err)
	}
	data, err := os.ReadFile(txt)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := "http://2.2.2.2:80\nhttp://1.1.1.1:80\n"; string(data) != want {
		t.Errorf("export text = %q, want %q", string(data), want)
	}
}

// TestPersistence checks that a reopened cache still knows its rows.
func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.json")
	db, _ := Open(p)
	_ = db.Save([]proxy.Result{{Addr: "9.9.9.9:80", Kind: "http", Alive: true, LatencyMs: 120}})
	_ = db.Close()

	re, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	top, _ := re.Top(5)
	if len(top) != 1 || top[0].Addr != "9.9.9.9:80" {
		t.Fatalf("reloaded top = %+v", top)
	}
}

func TestOlderResultsCannotOverwriteNewerChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	older := proxy.Result{Addr: "10.0.0.1:80", Kind: proxy.KindHTTP, Alive: true, CheckedAt: time.Now().Add(-time.Minute)}
	newer := older
	newer.Alive = false
	newer.CheckedAt = older.CheckedAt.Add(time.Minute)
	if err := db.Save([]proxy.Result{newer, older}); err != nil {
		t.Fatal(err)
	}
	if err := db.Save([]proxy.Result{older}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, cache := range []*DB{db, reopened} {
		alive, total, err := cache.Count()
		if err != nil || alive != 0 || total != 1 {
			t.Fatalf("stale live result resurrected: alive=%d total=%d err=%v", alive, total, err)
		}
	}
	// Equal timestamps are used when enrichment replaces a reachability result.
	newer.Alive, newer.SpeedKBs = true, 100
	if err := db.Save([]proxy.Result{newer}); err != nil {
		t.Fatal(err)
	}
	top, err := db.Top(1)
	if err != nil || len(top) != 1 || top[0].SpeedKBs != 100 {
		t.Fatalf("same-check enrichment was lost: %+v, %v", top, err)
	}
}

func TestSaveFailureDoesNotChangeMemoryOrDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	row := proxy.Result{Addr: "10.0.0.1:80", Kind: proxy.KindHTTP, Alive: true, CheckedAt: time.Now()}
	if err := db.Save([]proxy.Result{row}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	// Replacing a directory with the cache file must fail on all platforms.
	db.path = blocked
	row.Alive = false
	if err := db.Save([]proxy.Result{row}); err == nil {
		t.Fatal("save to a directory succeeded")
	}
	alive, total, err := db.Count()
	if err != nil || alive != 1 || total != 1 {
		t.Fatalf("failed save changed memory: alive=%d total=%d err=%v", alive, total, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("failed save changed disk: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("failed save left temporary files: %v, %v", matches, err)
	}
}

func TestOpenRejectsCorruptCache(t *testing.T) {
	for _, body := range []string{`{"results":[`, `{"results":[{"addr":123}]}`, `not JSON`} {
		path := filepath.Join(t.TempDir(), "cache.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if db, err := Open(path); err == nil || db != nil {
			t.Fatalf("corrupt cache %q loaded: %v, %v", body, db, err)
		}
	}
}

func TestOpenBareArrayKeepsNewestDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	newer := proxy.Result{Addr: "10.0.0.1:80", Kind: proxy.KindHTTP, Alive: false, CheckedAt: time.Now()}
	older := newer
	older.Alive, older.CheckedAt = true, newer.CheckedAt.Add(-time.Minute)
	data, err := json.Marshal([]proxy.Result{newer, older})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	alive, total, err := db.Count()
	if err != nil || alive != 0 || total != 1 {
		t.Fatalf("old duplicate won: alive=%d total=%d err=%v", alive, total, err)
	}
}
