package store

import (
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
