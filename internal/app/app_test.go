package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
)

func TestValidateCancellationStopsQueuedChecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "203.0.113.1")
	}))
	defer srv.Close()
	c := proxy.Candidate{Addr: strings.TrimPrefix(srv.URL, "http://"), Kind: proxy.KindHTTP}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int
	alive := Validate(ctx, []proxy.Candidate{c, c, c}, Options{Workers: 1, IPURL: srv.URL}, func(r proxy.Result) {
		calls++
		cancel()
	})
	if calls != 1 || len(alive) != 1 {
		t.Fatalf("cancelled validation: callbacks=%d, alive=%d; want 1 each", calls, len(alive))
	}
	Validate(ctx, []proxy.Candidate{c}, Options{IPURL: srv.URL}, func(proxy.Result) {
		t.Error("already cancelled validation invoked callback")
	})
}

func TestValidateBoundedConcurrencyAndCounts(t *testing.T) {
	const workers, count = 3, 12
	var active, maximum atomic.Int32
	started := make(chan struct{}, count)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		fmt.Fprint(w, "203.0.113.1")
	}))
	defer srv.Close()
	defer unblock()
	cands := make([]proxy.Candidate, count)
	for i := range cands {
		cands[i] = proxy.Candidate{Addr: strings.TrimPrefix(srv.URL, "http://"), Kind: proxy.KindHTTP, Source: fmt.Sprint(i)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var mu sync.Mutex
	calls := map[string]int{}
	done := make(chan []proxy.Result, 1)
	go func() {
		done <- Validate(ctx, cands, Options{Workers: workers, IPURL: srv.URL}, func(r proxy.Result) {
			mu.Lock()
			defer mu.Unlock()
			calls[r.Source]++
		})
	}()
	for i := 0; i < workers; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("workers did not start concurrently")
		}
	}
	unblock()
	select {
	case alive := <-done:
		if len(alive) != count || maximum.Load() != workers {
			t.Fatalf("alive=%d maximum concurrency=%d; want %d and %d", len(alive), maximum.Load(), count, workers)
		}
	case <-ctx.Done():
		t.Fatal("validation did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, c := range cands {
		if calls[c.Source] != 1 {
			t.Errorf("candidate %s got %d callbacks", c.Source, calls[c.Source])
		}
	}
}

func TestFinaliseCancellationPreservesUncheckedResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	defer cancel()
	rows := []proxy.Result{
		{Addr: strings.TrimPrefix(srv.URL, "http://"), Kind: proxy.KindHTTP, LatencyMs: 1, SpeedKBs: 42, Anonymity: proxy.AnonElite},
		{Addr: strings.TrimPrefix(srv.URL, "http://"), Kind: proxy.KindHTTP, LatencyMs: 2, SpeedKBs: 43, Anonymity: proxy.AnonAnonymous},
	}
	opt := Options{Workers: 1, SpeedTest: true, Anonymity: true, DirectIP: "203.0.113.1", SpeedURL: srv.URL, HeadersURL: srv.URL}
	done := make(chan []proxy.Result, 1)
	go func() { done <- Finalise(ctx, rows, opt) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("speed request did not start")
	}
	cancel()
	select {
	case got := <-done:
		if !reflect.DeepEqual(got, rows) {
			t.Fatalf("cancelled enrichment changed results: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("enrichment did not stop promptly")
	}
	if got := Finalise(ctx, rows, opt); !reflect.DeepEqual(got, rows) {
		t.Fatalf("already cancelled enrichment changed results: %+v", got)
	}
}

func TestFinaliseHonorsProbeCapsAndDoesNotMutateInput(t *testing.T) {
	var speed, anon atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/speed" {
			speed.Add(1)
			fmt.Fprint(w, strings.Repeat("x", 4096))
		} else {
			anon.Add(1)
			fmt.Fprint(w, `{"headers":{"Via":"test-proxy"}}`)
		}
	}))
	defer srv.Close()
	rows := make([]proxy.Result, 6)
	for i := range rows {
		rows[i] = proxy.Result{Addr: strings.TrimPrefix(srv.URL, "http://"), Kind: proxy.KindHTTP, LatencyMs: len(rows) - i}
	}
	original := append([]proxy.Result(nil), rows...)
	got := Finalise(context.Background(), rows, Options{
		Workers: 2, SpeedTest: true, SpeedTop: 2, Anonymity: true, AnonTop: 4,
		DirectIP: "203.0.113.1", SpeedURL: srv.URL + "/speed", HeadersURL: srv.URL + "/headers",
	})
	if speed.Load() != 2 || anon.Load() != 4 {
		t.Fatalf("speed=%d anonymity=%d; want 2 and 4", speed.Load(), anon.Load())
	}
	if !reflect.DeepEqual(rows, original) {
		t.Fatal("Finalise mutated its input")
	}
	for i, r := range got {
		if r.LatencyMs != i+1 || (i < 4) != (r.Anonymity == proxy.AnonAnonymous) {
			t.Errorf("unexpected ranked row %d: %+v", i, r)
		}
	}
}
