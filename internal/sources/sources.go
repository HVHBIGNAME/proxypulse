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

// DefaultCapPerSource limits how many entries a single pool may contribute, so
// one huge list (100k+ addresses) cannot crowd out the rest of the pools.
const DefaultCapPerSource = 1200

// Source describes one upstream proxy pool.
type Source struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Kind   string `json:"kind"`   // default proxy kind for entries
	Format string `json:"format"` // ipport | colon | vakhov | geonode | proxifly
	Cap    int    `json:"cap"`    // max entries from this source (0 = DefaultCapPerSource)
}

// Registry lists the built-in public pools. All are plain public lists; the
// tool always re-verifies every entry before it is shown to the user.
func Registry() []Source {
	return []Source{
		// --- monosans ---
		{ID: "monosans-http", Name: "monosans/http", Kind: proxy.KindHTTP, Format: "ipport",
			URL: "https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/http.txt"},
		{ID: "monosans-socks5", Name: "monosans/socks5", Kind: proxy.KindSOCKS5, Format: "ipport",
			URL: "https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/socks5.txt"},
		// --- TheSpeedX ---
		{ID: "thespeedx-http", Name: "TheSpeedX/http", Kind: proxy.KindHTTP, Format: "ipport",
			URL: "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/http.txt"},
		{ID: "thespeedx-socks5", Name: "TheSpeedX/socks5", Kind: proxy.KindSOCKS5, Format: "ipport",
			URL: "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/socks5.txt"},
		// --- roosterkid ---
		{ID: "roosterkid-https", Name: "roosterkid/https", Kind: proxy.KindHTTP, Format: "ipport",
			URL: "https://raw.githubusercontent.com/roosterkid/openproxylist/main/HTTPS_RAW.txt"},
		{ID: "roosterkid-socks5", Name: "roosterkid/socks5", Kind: proxy.KindSOCKS5, Format: "ipport",
			URL: "https://raw.githubusercontent.com/roosterkid/openproxylist/main/SOCKS5_RAW.txt"},
		// --- APIs with metadata ---
		{ID: "proxifly-http", Name: "proxifly/http", Kind: proxy.KindHTTP, Format: "proxifly",
			URL: "https://raw.githubusercontent.com/proxifly/free-proxy-list/main/proxies/all/data.json"},
		{ID: "proxyscrape-http", Name: "proxyscrape/http", Kind: proxy.KindHTTP, Format: "ipport",
			URL: "https://api.proxyscrape.com/v4/free-proxy-list/get?request=display_proxies&protocol=http&proxy_format=ipport&format=text"},
		{ID: "proxyscrape-socks5", Name: "proxyscrape/socks5", Kind: proxy.KindSOCKS5, Format: "ipport",
			URL: "https://api.proxyscrape.com/v4/free-proxy-list/get?request=display_proxies&protocol=socks5&proxy_format=ipport&format=text"},
		{ID: "geonode", Name: "geonode/api", Kind: proxy.KindHTTP, Format: "geonode",
			URL: "https://proxylist.geonode.com/api/proxy-list?limit=500&page=1&sort_by=lastChecked&sort_type=desc"},
		// --- vakhov (JSON with per-protocol flags, fresh) ---
		{ID: "vakhov", Name: "vakhov/fresh", Kind: proxy.KindHTTP, Format: "vakhov",
			URL: "https://raw.githubusercontent.com/vakhov/fresh-proxy-list/master/proxylist.json"},
		// --- MuRongPIG (very large mirror) ---
		{ID: "murong-http", Name: "murong/http", Kind: proxy.KindHTTP, Format: "ipport",
			URL: "https://raw.githubusercontent.com/MuRongPIG/Proxy-Master/main/http.txt"},
		{ID: "murong-socks5", Name: "murong/socks5", Kind: proxy.KindSOCKS5, Format: "ipport",
			URL: "https://raw.githubusercontent.com/MuRongPIG/Proxy-Master/main/socks5.txt"},
		// --- hideip.me (host:port:country) ---
		{ID: "hideipme-http", Name: "hideip.me/http", Kind: proxy.KindHTTP, Format: "colon",
			URL: "https://raw.githubusercontent.com/zloi-user/hideip.me/main/http.txt"},
		{ID: "hideipme-socks5", Name: "hideip.me/socks5", Kind: proxy.KindSOCKS5, Format: "colon",
			URL: "https://raw.githubusercontent.com/zloi-user/hideip.me/main/socks5.txt"},
		// --- hookzof (large socks5) ---
		{ID: "hookzof-socks5", Name: "hookzof/socks5", Kind: proxy.KindSOCKS5, Format: "ipport",
			URL: "https://raw.githubusercontent.com/hookzof/socks5_list/master/proxy.txt"},
		// --- ShiftyTR ---
		{ID: "shifty-http", Name: "shifty/http", Kind: proxy.KindHTTP, Format: "ipport",
			URL: "https://raw.githubusercontent.com/ShiftyTR/Proxy-List/master/http.txt"},
		{ID: "shifty-socks5", Name: "shifty/socks5", Kind: proxy.KindSOCKS5, Format: "ipport",
			URL: "https://raw.githubusercontent.com/ShiftyTR/Proxy-List/master/socks5.txt"},
		// --- clarketm ---
		{ID: "clarketm", Name: "clarketm/http", Kind: proxy.KindHTTP, Format: "ipport",
			URL: "https://raw.githubusercontent.com/clarketm/proxy-list/master/proxy-list-raw.txt"},
		// --- ALIILAPRO ---
		{ID: "aliilapro", Name: "aliilapro/http", Kind: proxy.KindHTTP, Format: "ipport",
			URL: "https://raw.githubusercontent.com/ALIILAPRO/Proxy/main/http.txt"},
		// --- anonym0uswork (https:// prefixed) ---
		{ID: "anonwork-http", Name: "anonwork/http", Kind: proxy.KindHTTP, Format: "ipport",
			URL: "https://raw.githubusercontent.com/anonym0uswork1221/free-proxies/main/proxy_files/http_proxies.txt"},
		// --- prxchk ---
		{ID: "prxchk-http", Name: "prxchk/http", Kind: proxy.KindHTTP, Format: "ipport",
			URL: "https://raw.githubusercontent.com/prxchk/proxy-list/main/http.txt"},
		{ID: "prxchk-socks5", Name: "prxchk/socks5", Kind: proxy.KindSOCKS5, Format: "ipport",
			URL: "https://raw.githubusercontent.com/prxchk/proxy-list/main/socks5.txt"},
	}
}

// FetchResult reports per-source fetch outcome.
type FetchResult struct {
	Source   Source
	Count    int
	Err      error
	Duration time.Duration
}

// FetchAll downloads all selected sources in parallel and returns deduplicated
// candidates plus per-source stats. Candidates from every pool are interleaved
// round-robin, so no single source dominates the front of the list. Passing an
// empty selection uses every source.
func FetchAll(ctx context.Context, selected []string, timeout time.Duration, workers int) ([]proxy.Candidate, []FetchResult, error) {
	reg := Registry()
	want := map[string]bool{}
	for _, id := range selected {
		want[id] = true
	}
	list := make([]Source, 0, len(reg))
	for _, s := range reg {
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
	type fetched struct {
		src Source
		got []proxy.Candidate
		err error
		dur time.Duration
	}
	perSource := make([]fetched, len(list))

	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, workers)
	)
	for i, s := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, s Source) {
			defer wg.Done()
			defer func() { <-sem }()
			start := time.Now()
			got, err := fetchOne(ctx, cl, s)
			perSource[i] = fetched{src: s, got: got, err: err, dur: time.Since(start)}
		}(i, s)
	}
	wg.Wait()

	// Interleave sources round-robin for a fair, diverse candidate list.
	var (
		results []FetchResult
		queues  [][]proxy.Candidate
	)
	for _, f := range perSource {
		results = append(results, FetchResult{Source: f.src, Count: len(f.got), Err: f.err, Duration: f.dur})
		queues = append(queues, f.got)
	}
	var cands []proxy.Candidate
	for more := true; more; {
		more = false
		for i := range queues {
			if len(queues[i]) > 0 {
				cands = append(cands, queues[i][0])
				queues[i] = queues[i][1:]
				more = true
			}
		}
	}

	// Deduplicate, keeping the first (round-robin) occurrence.
	seen := make(map[string]struct{}, len(cands))
	out := make([]proxy.Candidate, 0, len(cands))
	for _, c := range cands {
		if c.Addr == "" {
			continue
		}
		k := c.Key()
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
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
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var got []proxy.Candidate
	switch s.Format {
	case "geonode":
		got = parseGeonode(data, s)
	case "proxifly":
		got = parseProxifly(data, s)
	case "vakhov":
		got = parseVakhov(data, s)
	case "colon":
		got = parseColon(data, s)
	default:
		got = parseIPPort(data, s)
	}
	return capSample(got, s.Cap), nil
}

// capSample limits a list to n entries spread evenly across it (not just the
// head), so a 100k-entry pool still contributes a diverse 1200.
func capSample(in []proxy.Candidate, n int) []proxy.Candidate {
	if n <= 0 {
		n = DefaultCapPerSource
	}
	if len(in) <= n {
		return in
	}
	out := make([]proxy.Candidate, 0, n)
	step := float64(len(in)) / float64(n)
	for i := 0; i < n; i++ {
		out = append(out, in[int(float64(i)*step)])
	}
	return out
}

// parseIPPort handles "ip:port" lines, optionally with scheme or trailing data.
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

// parseColon handles "ip:port:country" (extra fields after the port).
func parseColon(data []byte, s Source) []proxy.Candidate {
	var out []proxy.Candidate
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		addr := normaliseAddr(parts[0] + ":" + parts[1])
		if addr == "" {
			continue
		}
		c := proxy.Candidate{Addr: addr, Kind: s.Kind, Source: s.ID}
		if len(parts) >= 3 {
			c.Country = strings.TrimSpace(parts[2])
		}
		out = append(out, c)
	}
	return out
}

// parseVakhov handles the vakhov/fresh-proxy-list JSON (per-protocol flags).
func parseVakhov(data []byte, s Source) []proxy.Candidate {
	var payload []struct {
		IP      string `json:"ip"`
		Port    string `json:"port"`
		Country string `json:"country_code"`
		HTTP    string `json:"http"`
		SSL     string `json:"ssl"`
		Socks4  string `json:"socks4"`
		Socks5  string `json:"socks5"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	var out []proxy.Candidate
	for _, e := range payload {
		kind := s.Kind
		switch {
		case e.Socks5 == "1" || e.Socks4 == "1":
			kind = proxy.KindSOCKS5
		case e.HTTP == "1" || e.SSL == "1":
			kind = proxy.KindHTTP
		}
		addr := normaliseAddr(e.IP + ":" + e.Port)
		if addr == "" {
			continue
		}
		out = append(out, proxy.Candidate{Addr: addr, Kind: kind, Source: s.ID, Country: e.Country})
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
