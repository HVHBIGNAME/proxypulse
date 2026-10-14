package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	"github.com/HVHBIGNAME/proxypulse/internal/store"
)

func TestMailboxCoalescesAndDeliversCompletion(t *testing.T) {
	m := &scanMailbox{}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Go(func() {
			for n := 0; n < 100; n++ {
				m.put(scanUpdate{phase: phaseCheck, done: n})
			}
		})
	}
	workers.Wait()
	m.put(scanUpdate{phase: phaseDone, done: 800, finished: true})
	u := m.take()
	if u == nil || !u.finished || u.done != 800 || m.take() != nil {
		t.Fatalf("completion was lost or duplicated: %+v", u)
	}
}

func TestCancelledRecheckPreservesCachedResult(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := proxy.Result{Addr: "127.0.0.1:1", Kind: proxy.KindHTTP, Alive: true, LatencyMs: 42}
	if err := db.Save([]proxy.Result{r}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := &scanMailbox{}
	runRecheck(ctx, r, scanSettings{}, db, m)
	u := m.take()
	if u == nil || !u.finished || !u.unchanged || u.phase != phaseStopped || u.removed != "" {
		t.Fatalf("cancellation changed the row: %+v", u)
	}
	cached, err := db.Top(0)
	if err != nil || len(cached) != 1 || cached[0] != r {
		t.Fatalf("cancellation invalidated cache: %+v, %v", cached, err)
	}
}

func TestFailedRecheckRemovesOnlySelectedEndpoint(t *testing.T) {
	// Local CONNECT rejection: no public proxy or external test host is used.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := proxy.Result{Addr: strings.TrimPrefix(server.URL, "http://"), Kind: proxy.KindHTTP, Alive: true, LatencyMs: 42}
	other := sampleResults()[0]
	if err := db.Save([]proxy.Result{r, other}); err != nil {
		t.Fatal(err)
	}
	m := &scanMailbox{}
	runRecheck(context.Background(), r, scanSettings{}, db, m)
	u := m.take()
	if u == nil || u.phase != phaseEmpty || !u.recheck || u.removed != r.URL() || !u.finished || u.saveError {
		t.Fatalf("unexpected recheck outcome: %+v", u)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	cached, err := reopened.Top(0)
	if err != nil || len(cached) != 1 || cached[0].URL() != other.URL() {
		t.Fatalf("failed endpoint resurrected or unrelated row removed: %+v, %v", cached, err)
	}
}
