package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckRejectsInvalidAndIncompleteResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		status int
		short  bool
		alive  bool
	}{
		{name: "ipv4", body: "203.0.113.7\n", status: http.StatusOK, alive: true},
		{name: "ipv6", body: "2001:db8::7\n", status: http.StatusOK, alive: true},
		{name: "html", body: "<html>blocked</html>", status: http.StatusOK},
		{name: "error status", body: "203.0.113.7", status: http.StatusBadGateway},
		{name: "truncated", body: "203.0.113.7", status: http.StatusOK, short: true},
		{name: "oversized", body: "203.0.113.7\n" + strings.Repeat("x", 4096), status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.short {
					w.Header().Set("Content-Length", fmt.Sprint(len(tc.body)+10))
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c := Candidate{Addr: strings.TrimPrefix(srv.URL, "http://"), Kind: KindHTTP}
			got := Check(context.Background(), c, srv.URL, time.Second)
			if got.Alive != tc.alive || (!tc.alive && got.Err == "") {
				t.Fatalf("unexpected check result: %+v", got)
			}
			if tc.alive && got.ExitIP != strings.TrimSpace(tc.body) {
				t.Errorf("exit IP = %q, want %q", got.ExitIP, strings.TrimSpace(tc.body))
			}
			wantIP := ""
			if tc.alive {
				wantIP = strings.TrimSpace(tc.body)
			}
			if got := RealIP(context.Background(), srv.URL, time.Second); got != wantIP {
				t.Errorf("RealIP = %q, want %q", got, wantIP)
			}
		})
	}
}

func TestProbeCancellationClosesPendingConnect(t *testing.T) {
	started, closed := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("request method = %s, want CONNECT", r.Method)
		}
		close(started)
		select {
		case <-r.Context().Done():
			close(closed)
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := Candidate{Addr: strings.TrimPrefix(srv.URL, "http://"), Kind: KindHTTP}
	done := make(chan ProbeResult, 1)
	go func() { done <- Probe(ctx, c, "https://"+c.Addr, 10*time.Second) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("CONNECT did not start")
	}
	cancel()
	select {
	case got := <-done:
		if got.Err == nil {
			t.Fatal("cancelled probe succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not cancel promptly")
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled probe leaked its pending CONNECT connection")
	}
}

func TestSpeedRejectsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "proxy failed", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := Candidate{Addr: strings.TrimPrefix(srv.URL, "http://"), Kind: KindHTTP}
	if speed, err := Speed(context.Background(), c, srv.URL, time.Second); err == nil || speed != 0 {
		t.Fatalf("Speed = %d, %v; want zero and an error", speed, err)
	}
}

func TestSpeedStopsAfterPayloadLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2000000")
		fmt.Fprint(w, strings.Repeat("x", 1_000_000))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c := Candidate{Addr: strings.TrimPrefix(srv.URL, "http://"), Kind: KindHTTP}
	if speed, err := Speed(ctx, c, srv.URL, time.Second); err != nil || speed <= 0 {
		t.Fatalf("Speed = %d, %v; want successful bounded sample", speed, err)
	}
}
