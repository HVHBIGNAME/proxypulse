package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	"github.com/HVHBIGNAME/proxypulse/internal/store"
)

var fixtureCertificate tls.Certificate

func TestMain(m *testing.M) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true,
		DNSNames: []string{"api.ipify.org", "speed.cloudflare.com", "httpbin.org"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	if err := os.Setenv("GODEBUG", os.Getenv("GODEBUG")+",x509usefallbackroots=1"); err != nil {
		panic(err)
	}
	x509.SetFallbackRoots(pool)
	fixtureCertificate = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	os.Exit(m.Run())
}

func TestInvalidScanArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-top=-1"}, "-top must be non-negative"},
		{[]string{"-top=-1", "-best"}, "-top must be non-negative"},
		{[]string{"-workers=0"}, "-workers must be greater than zero"},
		{[]string{"-workers=-1", "-best"}, "-workers must be greater than zero"},
		{[]string{"-timeout=0"}, "-timeout must be greater than zero"},
		{[]string{"-timeout=-1s"}, "-timeout must be greater than zero"},
		{[]string{"-limit=-1"}, "-limit must be non-negative"},
		{[]string{"-source-cap=-1"}, "-source-cap must be non-negative"},
		{[]string{"-format=xml"}, "use text or json"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cache.json")
			args := append([]string{"-db-only", "-db", path, "-speed=false", "-anon=false", "-quiet"}, tc.args...)
			out, _, err := runCLI(t, context.Background(), args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if out != "" {
				t.Errorf("invalid arguments produced stdout: %q", out)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("invalid arguments touched cache: %v", err)
			}
		})
	}
}

func TestVersionExitsBeforeScanAndCache(t *testing.T) {
	// A directory is an invalid cache path; no scan-specific validation is needed.
	path := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, stderr, err := runCLI(t, ctx, "-version", "-db", path, "-workers=0")
	if err != nil || out != "ProxyPulse "+version+"\n" || stderr != "" {
		t.Fatalf("version output=%q stderr=%q err=%v", out, stderr, err)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("version touched cache directory: %v, %v", entries, err)
	}
}

func TestRunPersistsChecksAndEnrichmentBeforeSpeedFilter(t *testing.T) {
	good := localProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__down" {
			fmt.Fprint(w, strings.Repeat("x", 8192))
		} else {
			fmt.Fprint(w, "203.0.113.7")
		}
	}))
	bad := localProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed proxy", http.StatusBadGateway)
	}))
	old := time.Now().Add(-time.Hour)
	path := seedCache(t,
		proxy.Result{Addr: good, Kind: proxy.KindHTTP, Alive: true, CheckedAt: old},
		proxy.Result{Addr: bad, Kind: proxy.KindHTTP, Alive: true, CheckedAt: old},
	)
	out, stderr, err := runCLI(t, context.Background(), "-db-only", "-db", path,
		"-best", "-workers=2", "-timeout=2s", "-anon=false", "-min-speed="+strconv.Itoa(int(^uint(0)>>1)))
	if err == nil || err.Error() != "no working proxy found" || out != "" || stderr != "" {
		t.Fatalf("filtered best: output=%q stderr=%q err=%v", out, stderr, err)
	}
	rows := readCache(t, path)
	if len(rows) != 2 {
		t.Fatalf("cache has %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if !r.CheckedAt.After(old) {
			t.Errorf("check was not saved: %+v", r)
		}
		switch r.Addr {
		case good:
			if !r.Alive || r.ExitIP != "203.0.113.7" || r.SpeedKBs <= 0 {
				t.Errorf("filtered survivor lost its enrichment: %+v", r)
			}
		case bad:
			if r.Alive || r.Err == "" {
				t.Errorf("failed cached proxy remained alive: %+v", r)
			}
		default:
			t.Errorf("unexpected cache entry: %+v", r)
		}
	}
}

func TestRunCancellationPreservesUntestedCacheEntries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	good := localProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "203.0.113.7")
	}))
	bad := localProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed proxy", http.StatusBadGateway)
	}))
	interrupted := localProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	var unstartedCalls atomic.Int32
	unstarted := localProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unstartedCalls.Add(1)
		fmt.Fprint(w, "203.0.113.8")
	}))
	old := time.Now().Add(-time.Hour)
	path := seedCache(t,
		proxy.Result{Addr: good, Kind: proxy.KindHTTP, Alive: true, LatencyMs: 1, CheckedAt: old},
		proxy.Result{Addr: bad, Kind: proxy.KindHTTP, Alive: true, LatencyMs: 2, CheckedAt: old},
		proxy.Result{Addr: interrupted, Kind: proxy.KindHTTP, Alive: true, LatencyMs: 3, CheckedAt: old},
		proxy.Result{Addr: unstarted, Kind: proxy.KindHTTP, Alive: true, LatencyMs: 4, CheckedAt: old},
	)
	out, stderr, err := runCLI(t, ctx, "-db-only", "-db", path, "-best",
		"-workers=1", "-timeout=2s", "-speed=false", "-anon=false")
	if err != nil || out != "http://"+good+"\n" || stderr != "" {
		t.Fatalf("partial best: output=%q stderr=%q err=%v", out, stderr, err)
	}
	if unstartedCalls.Load() != 0 {
		t.Error("-best ignored -workers=1 or started queued checks after cancellation")
	}
	rows := readCache(t, path)
	if len(rows) != 4 {
		t.Fatalf("cache has %d rows, want 4", len(rows))
	}
	for _, r := range rows {
		switch r.Addr {
		case good:
			if !r.Alive || !r.CheckedAt.After(old) {
				t.Errorf("completed success not saved: %+v", r)
			}
		case bad:
			if r.Alive || !r.CheckedAt.After(old) {
				t.Errorf("completed failure not saved: %+v", r)
			}
		case interrupted, unstarted:
			if !r.Alive || !r.CheckedAt.Equal(old) {
				t.Errorf("cancellation invalidated cached result: %+v", r)
			}
		default:
			t.Errorf("unexpected cache entry: %+v", r)
		}
	}
}

func TestBestOutputContract(t *testing.T) {
	addr := localProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "203.0.113.7")
	}))
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			path := seedCache(t, proxy.Result{Addr: addr, Kind: proxy.KindHTTP, Alive: true})
			out, stderr, err := runCLI(t, context.Background(), "-db-only", "-db", path,
				"-best", "-format", format, "-workers=1", "-speed=false", "-anon=false")
			if err != nil || stderr != "" {
				t.Fatalf("best stderr=%q err=%v", stderr, err)
			}
			if format == "text" {
				if out != "http://"+addr+"\n" {
					t.Fatalf("best text = %q", out)
				}
			} else {
				var r proxy.Result
				if err := json.Unmarshal([]byte(out), &r); err != nil || r.Addr != addr || !r.Alive {
					t.Fatalf("best JSON = %q, error=%v", out, err)
				}
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), "best.txt")); !os.IsNotExist(err) {
				t.Errorf("-best unexpectedly created best.txt: %v", err)
			}
		})
	}
}

func runCLI(t *testing.T, ctx context.Context, args ...string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	out, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	errOut, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer errOut.Close()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, errOut
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	runErr := run(ctx, args)
	stdout, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.ReadFile(errOut.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(stdout), string(stderr), runErr
}

func seedCache(t *testing.T, rows ...proxy.Result) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cache.json")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Save(rows); err != nil {
		t.Fatal(err)
	}
	return path
}

func readCache(t *testing.T, path string) []proxy.Result {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cache struct {
		Results []proxy.Result `json:"results"`
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatal(err)
	}
	return cache.Results
}

// Every CONNECT goes to this local TLS server, regardless of the requested host.
func localProxy(t *testing.T, handler http.Handler) string {
	t.Helper()
	upstream := httptest.NewUnstartedServer(handler)
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{fixtureCertificate}}
	upstream.StartTLS()
	t.Cleanup(upstream.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		target, err := net.DialTimeout("tcp", upstream.Listener.Addr().String(), time.Second)
		if err != nil {
			t.Errorf("local upstream dial: %v", err)
			http.Error(w, "local upstream unavailable", http.StatusBadGateway)
			return
		}
		defer target.Close()
		client, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack local CONNECT: %v", err)
			return
		}
		defer client.Close()
		if _, err := rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}
		done := make(chan struct{})
		go func() {
			io.Copy(target, rw)
			target.Close()
			close(done)
		}()
		io.Copy(client, target)
		client.Close()
		<-done
	}))
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}
