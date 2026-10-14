package sources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
)

func TestNormaliseAddr(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4:8080":                 "1.2.3.4:8080",
		"http://1.2.3.4:8080":          "1.2.3.4:8080",
		"socks5://1.2.3.4:1080":        "1.2.3.4:1080",
		"https://1.2.3.4:9002":         "1.2.3.4:9002",
		"1.2.3.4:8080\tUS":             "1.2.3.4:8080",
		"  5.6.7.8:3128  ":             "5.6.7.8:3128",
		"# comment line":               "",
		"":                             "",
		"no-port-here":                 "",
		"1.2.3.4:abc":                  "",
		"1.2.3.4:0":                    "",
		"1.2.3.4:65536":                "",
		"1.2.3.4:99999999999999999999": "",
		"1.2.3.4:00080":                "1.2.3.4:80",
		"user@1.2.3.4:80":              "",
	}
	for in, want := range cases {
		if got := normaliseAddr(in); got != want {
			t.Errorf("normaliseAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseIPPort(t *testing.T) {
	s := Source{ID: "t", Kind: "http", Format: "ipport"}
	got, err := parseIPPort([]byte("1.1.1.1:80\n2.2.2.2:3128\n\n# x\n3.3.3.3:1080\n"), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d candidates, want 3", len(got))
	}
	if got[0].Addr != "1.1.1.1:80" || got[0].Kind != "http" || got[0].Source != "t" {
		t.Errorf("unexpected first candidate: %+v", got[0])
	}
}

func TestParseColon(t *testing.T) {
	s := Source{ID: "hideip", Kind: "http", Format: "colon"}
	got, err := parseColon([]byte("103.217.224.29:8089:Indonesia\n200.114.81.219:8080:Argentina\n"), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Addr != "103.217.224.29:8089" || got[0].Country != "Indonesia" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Country != "Argentina" {
		t.Errorf("second: %+v", got[1])
	}
}

func TestParseVakhov(t *testing.T) {
	s := Source{ID: "vakhov", Kind: "http", Format: "vakhov"}
	body := []byte(`[
	  {"ip":"72.195.101.99","port":"4145","country_code":"US","http":"0","ssl":"0","socks4":"1","socks5":"0"},
	  {"ip":"45.131.6.46","port":"80","country_code":"NL","http":"1","ssl":"0","socks4":"1","socks5":"1"}
	]`)
	got, err := parseVakhov(body, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Kind != "http" || got[0].Country != "NL" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Kind != "socks5" || got[1].Country != "NL" || got[1].Addr != got[0].Addr {
		t.Errorf("second: %+v", got[1])
	}
}

func TestParseGeonode(t *testing.T) {
	s := Source{ID: "geonode", Kind: "http", Format: "geonode"}
	body := []byte(`{"data":[
	  {"ip":"10.0.0.1","port":"8080","country":"DE","protocols":["https"]},
	  {"ip":"10.0.0.2","port":"1080","country":"US","protocols":["socks5","socks4"]},
	  {"ip":"10.0.0.3","port":"1080","country":"US","protocols":["socks4"]},
	  {"ip":"10.0.0.4","port":"65536","country":"US","protocols":["http"]}
	]}`)
	got, err := parseGeonode(body, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Kind != "http" || got[0].Country != "DE" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Kind != "socks5" {
		t.Errorf("socks entry kind = %q, want socks5", got[1].Kind)
	}
}

func TestParseProxifly(t *testing.T) {
	s := Source{ID: "proxifly", Kind: "http", Format: "proxifly"}
	body := []byte(`[{"proxy":"10.0.0.9:4145","protocol":"socks5","geolocation":"US"},
	  {"proxy":"10.0.0.8:4145","protocol":"socks4","geolocation":"US"}]`)
	got, err := parseProxifly(body, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if got[0].Kind != "socks5" || got[0].Addr != "10.0.0.9:4145" {
		t.Errorf("unexpected: %+v", got[0])
	}
}

// TestCapSampleSpread checks the cap keeps a spread across the input, not a head slice.
func TestCapSampleSpread(t *testing.T) {
	in := make([]proxy.Candidate, 1000)
	for i := range in {
		in[i] = proxy.Candidate{Addr: "10.0.0.1:" + strconv.Itoa(i+1), Kind: "http"}
	}
	out := capSample(in, 10)
	if len(out) != 10 {
		t.Fatalf("got %d, want 10", len(out))
	}
	// The cap must not simply take the head.
	if out[9].Addr == in[9].Addr {
		t.Errorf("cap only took the head: %+v", out)
	}
	if len(capSample(in, 5000)) != 1000 {
		t.Errorf("cap must not pad when input is smaller than the cap")
	}
}

func TestRegistryKindsAreValid(t *testing.T) {
	reg := Registry()
	if len(reg) < 18 {
		t.Fatalf("registry has %d pools, expected the expanded set", len(reg))
	}
	seen := map[string]bool{}
	for _, s := range reg {
		if s.ID == "" || s.URL == "" {
			t.Errorf("source missing id/url: %+v", s)
		}
		if s.Kind != "http" && s.Kind != "socks5" {
			t.Errorf("source %s has invalid kind %q", s.ID, s.Kind)
		}
		if seen[s.ID] {
			t.Errorf("duplicate source id %q", s.ID)
		}
		seen[s.ID] = true
	}
}

func TestParseIPPortHonorsExplicitProtocol(t *testing.T) {
	got, err := parseIPPort([]byte("socks4://10.0.0.1:1080\nsocks5://10.0.0.2:1080\nhttps://10.0.0.3:80\n"), Source{Kind: proxy.KindHTTP})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != proxy.KindSOCKS5 || got[1].Kind != proxy.KindHTTP {
		t.Fatalf("unexpected protocol mapping: %+v", got)
	}
}

func TestFetchSourcesInterleavesUniqueCappedCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			fmt.Fprint(w, "10.0.0.1:80\n10.0.0.1:00080\n10.0.0.1:80\n10.0.0.2:80\n10.0.0.3:80\ninvalid:65536\n")
		case "/b":
			fmt.Fprint(w, "10.0.0.2:80\n10.0.0.4:80\n")
		default:
			fmt.Fprint(w, "not JSON")
		}
	}))
	defer srv.Close()
	list := []Source{
		{ID: "a", URL: srv.URL + "/a", Kind: proxy.KindHTTP, Cap: 2},
		{ID: "b", URL: srv.URL + "/b", Kind: proxy.KindHTTP, Cap: 2},
		{ID: "bad", URL: srv.URL + "/bad", Kind: proxy.KindHTTP, Format: "geonode"},
	}
	got, stats, err := fetchSources(context.Background(), list, time.Second, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 3 || stats[0].Count != 2 || stats[1].Count != 2 || stats[2].Count != 0 || stats[2].Err == nil {
		t.Fatalf("unexpected fetch stats: %+v", stats)
	}
	want := []string{"http://10.0.0.1:80", "http://10.0.0.2:80", "http://10.0.0.4:80"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %+v, want %v", got, want)
	}
	for i, c := range got {
		if c.Key() != want[i] {
			t.Errorf("candidate %d = %s, want %s", i, c.Key(), want[i])
		}
	}
	if got[1].Source != "b" {
		t.Error("dedup did not preserve the first round-robin occurrence")
	}
}

func TestFetchSourcesCancellationKeepsSourceStats(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		cancel()
		<-r.Context().Done()
	}))
	defer srv.Close()
	list := []Source{
		{ID: "a", URL: srv.URL, Kind: proxy.KindHTTP},
		{ID: "b", URL: srv.URL, Kind: proxy.KindHTTP},
		{ID: "c", URL: srv.URL, Kind: proxy.KindHTTP},
	}
	got, stats, err := fetchSources(ctx, list, time.Second, 1)
	if !errors.Is(err, context.Canceled) || len(got) != 0 || requests.Load() != 1 {
		t.Fatalf("cancelled fetch: candidates=%v requests=%d err=%v", got, requests.Load(), err)
	}
	if len(stats) != len(list) {
		t.Fatalf("stats length = %d, want %d", len(stats), len(list))
	}
	for i, s := range stats {
		if s.Source.ID != list[i].ID || !errors.Is(s.Err, context.Canceled) || s.Count != 0 {
			t.Errorf("cancelled source stat %d = %+v", i, s)
		}
	}
}

func TestFetchOneRejectsMalformedAndTruncatedSources(t *testing.T) {
	for _, format := range []string{"geonode", "proxifly", "vakhov", "ipport", "colon"} {
		t.Run(format, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if format == "ipport" || format == "colon" {
					fmt.Fprint(w, "10.0.0.1:80\n", strings.Repeat("x", 1<<20))
				} else {
					fmt.Fprint(w, "{bad json")
				}
			}))
			defer srv.Close()
			got, err := fetchOne(context.Background(), srv.Client(), Source{URL: srv.URL, Kind: proxy.KindHTTP, Format: format})
			if err == nil || len(got) != 0 {
				t.Fatalf("malformed source: got %d candidates, err=%v", len(got), err)
			}
		})
	}
}
