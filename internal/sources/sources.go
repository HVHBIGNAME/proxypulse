// Package sources fetches public proxy pools and normalises them into candidates.
package sources

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
)

// Source describes one upstream proxy pool.
type Source struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Kind   string `json:"kind"`   // default proxy kind for entries
	Format string `json:"format"` // ipport | geonode | proxifly | proxyscrape
}

// Registry lists the built-in public pools. All are plain public lists; the
// tool always re-verifies every entry before it is shown to the user.
var Registry = []Source{
	{ID: "monosans-http", Name: "monosans/http", Kind: proxy.KindHTTP, Format: "ipport",
		URL: "https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/http.txt"},
	{ID: "monosans-socks5", Name: "monosans/socks5", Kind: proxy.KindSOCKS5, Format: "ipport",
		URL: "https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/socks5.txt"},
	{ID: "thespeedx-http", Name: "TheSpeedX/http", Kind: proxy.KindHTTP, Format: "ipport",
		URL: "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/http.txt"},
	{ID: "thespeedx-socks5", Name: "TheSpeedX/socks5", Kind: proxy.KindSOCKS5, Format: "ipport",
		URL: "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/socks5.txt"},
	{ID: "roosterkid-https", Name: "roosterkid/https", Kind: proxy.KindHTTP, Format: "ipport",
		URL: "https://raw.githubusercontent.com/roosterkid/openproxylist/main/HTTPS_RAW.txt"},
	{ID: "roosterkid-socks5", Name: "roosterkid/socks5", Kind: proxy.KindSOCKS5, Format: "ipport",
		URL: "https://raw.githubusercontent.com/roosterkid/openproxylist/main/SOCKS5_RAW.txt"},
	{ID: "proxifly-http", Name: "proxifly/http", Kind: proxy.KindHTTP, Format: "proxifly",
		URL: "https://raw.githubusercontent.com/proxifly/free-proxy-list/main/proxies/all/data.json"},
	{ID: "proxyscrape-http", Name: "proxyscrape/http", Kind: proxy.KindHTTP, Format: "proxyscrape",
		URL: "https://api.proxyscrape.com/v4/free-proxy-list/get?request=display_proxies&protocol=http&proxy_format=ipport&format=text"},
	{ID: "proxyscrape-socks5", Name: "proxyscrape/socks5", Kind: proxy.KindSOCKS5, Format: "proxyscrape",
		URL: "https://api.proxyscrape.com/v4/free-proxy-list/get?request=display_proxies&protocol=socks5&proxy_format=ipport&format=text"},
	{ID: "geonode", Name: "geonode/api", Kind: proxy.KindHTTP, Format: "geonode",
		URL: "https://proxylist.geonode.com/api/proxy-list?limit=500&page=1&sort_by=lastChecked&sort_type=desc"},
}

// FetchResult reports per-source fetch outcome.
type FetchResult struct {
	Source   Source
	Count    int
	Err      error
	Duration time.Duration
}

// FetchAll downloads all selected sources in parallel and returns deduplicated
// candidates plus per-source stats. Passing an empty selection uses every source.
func FetchAll(ctx context.Context, selected []string, timeout time.Duration, workers int) ([]proxy.Candidate, []FetchResult, error) {
	want := map[string]bool{}
	for _, id := range selected {
		want[id] = true
	}
	list := make([]Source, 0, len(Registry))
	for _, s := range Registry {
		if len(want) == 0 || want[s.ID] {
			list = append(list, s)
		}
	}
	if len(list) == 0 {
		return nil, nil, fmt.Errorf("no sources selected")
	}
	if workers < 1 {
		workers = len(list)
	}

	cl := &http.Client{Timeout: timeout}
	var (
		mu      sync.Mutex
		cands   []proxy.Candidate
		results []FetchResult
		wg      sync.WaitGroup
		sem     = make(chan struct{}, workers)
	)
	for _, s := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func(s Source) {
			defer wg.Done()
			defer func() { <-sem }()
			start := time.Now()
			got, err := fetchOne(ctx, cl, s)
			mu.Lock()
			results = append(results, FetchResult{Source: s, Count: len(got), Err: err, Duration: time.Since(start)})
			cands = append(cands, got...)
			mu.Unlock()
		}(s)
	}
	wg.Wait()

	dedup := make(map[string]proxy.Candidate, len(cands))
	for _, c := range cands {
		if c.Addr == "" {
			continue
		}
		k := c.Key()
		if _, ok := dedup[k]; !ok {
			dedup[k] = c
		}
	}
	out := make([]proxy.Candidate, 0, len(dedup))
	for _, c := range dedup {
		out = append(out, c)
	}
	return out, results, nil
}

func fetchOne(ctx context.Context, cl *http.Client, s Source) ([]proxy.Candidate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ProxyPulse/1.0")
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	switch s.Format {
	case "geonode":
		return parseGeonode(data, s), nil
	case "proxifly":
		return parseProxifly(data, s), nil
	default:
		return parseIPPort(data, s), nil
	}
}

func parseIPPort(data []byte, s Source) []proxy.Candidate {
	var out []proxy.Candidate
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		addr := normaliseAddr(sc.Text())
		if addr == "" {
			continue
		}
		out = append(out, proxy.Candidate{Addr: addr, Kind: s.Kind, Source: s.ID})
	}
	return out
}

func parseGeonode(data []byte, s Source) []proxy.Candidate {
	var payload struct {
		Data []struct {
			IP        string   `json:"ip"`
			Port      string   `json:"port"`
			Country   string   `json:"country"`
			Protocols []string `json:"protocols"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	var out []proxy.Candidate
	for _, e := range payload.Data {
		kind := s.Kind
		for _, p := range e.Protocols {
			switch strings.ToLower(p) {
			case "socks5", "socks4":
				kind = proxy.KindSOCKS5
			case "https", "http":
				kind = proxy.KindHTTP
			}
		}
		out = append(out, proxy.Candidate{
			Addr:    normaliseAddr(e.IP + ":" + e.Port),
			Kind:    kind,
			Source:  s.ID,
			Country: e.Country,
		})
	}
	return out
}

func parseProxifly(data []byte, s Source) []proxy.Candidate {
	var payload []struct {
		Proxy    string `json:"proxy"`
		Protocol string `json:"protocol"`
		Country  string `json:"geolocation"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	var out []proxy.Candidate
	for _, e := range payload {
		kind := kindFromProtocol(e.Protocol, s.Kind)
		out = append(out, proxy.Candidate{
			Addr:    normaliseAddr(e.Proxy),
			Kind:    kind,
			Source:  s.ID,
			Country: e.Country,
		})
	}
	return out
}

func kindFromProtocol(p, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "socks5", "socks4":
		return proxy.KindSOCKS5
	case "http", "https":
		return proxy.KindHTTP
	}
	return fallback
}

// normaliseAddr strips scheme prefixes and validates host:port shape.
func normaliseAddr(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return ""
	}
	for _, p := range []string{"http://", "https://", "socks5://", "socks4://", "socks://"} {
		raw = strings.TrimPrefix(raw, p)
	}
	if i := strings.IndexAny(raw, "\t ,;\"'"); i >= 0 {
		raw = raw[:i]
	}
	if !strings.Contains(raw, ":") {
		return ""
	}
	host, port, ok := strings.Cut(raw, ":")
	if !ok || host == "" || port == "" {
		return ""
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return host + ":" + port
}
