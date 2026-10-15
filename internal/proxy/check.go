package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// Defaults used by the checker.
const (
	DefaultIPURL      = "https://api.ipify.org/"
	DefaultHeadersURL = "https://httpbin.org/headers"
	DefaultSpeedURL   = "https://speed.cloudflare.com/__down?bytes=1000000"
)

// transportFor builds an http.Transport routed through the given candidate.
func transportFor(c Candidate, timeout time.Duration) (*http.Transport, error) {
	t := &http.Transport{
		DisableKeepAlives:   true,
		MaxIdleConnsPerHost: 1,
		ForceAttemptHTTP2:   false,
	}
	switch c.Kind {
	case KindSOCKS5:
		d, err := proxy.SOCKS5("tcp", c.Addr, nil, &net.Dialer{Timeout: timeout})
		if err != nil {
			return nil, err
		}
		if cd, ok := d.(proxy.ContextDialer); ok {
			t.DialContext = cd.DialContext
		} else {
			t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return d.Dial(network, addr)
			}
		}
	default: // http / https CONNECT proxies
		pu, err := url.Parse("http://" + c.Addr)
		if err != nil {
			return nil, err
		}
		t.Proxy = http.ProxyURL(pu)
		t.DialContext = (&net.Dialer{Timeout: timeout}).DialContext
	}
	return t, nil
}

// clientFor builds a short-lived HTTP client for the candidate.
func clientFor(c Candidate, timeout time.Duration) (*http.Client, error) {
	tr, err := transportFor(c, timeout)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// ProbeResult is the raw outcome of an HTTP reachability probe.
type ProbeResult struct {
	StatusCode int
	Body       string
	Latency    time.Duration
	Err        error
}

// Probe performs one GET request through the proxy.
func Probe(ctx context.Context, c Candidate, target string, timeout time.Duration) ProbeResult {
	cl, err := clientFor(c, timeout)
	if err != nil {
		return ProbeResult{Err: err}
	}
	defer cl.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return ProbeResult{Err: err}
	}
	req.Header.Set("User-Agent", "ProxyPulse/1.0")
	req.Header.Set("Accept", "*/*")
	start := time.Now()
	resp, err := cl.Do(req)
	if err != nil {
		return ProbeResult{Err: err, Latency: time.Since(start)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if len(body) > 4096 && err == nil {
		err = fmt.Errorf("probe response exceeds 4096 bytes")
	}
	return ProbeResult{
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(body)),
		Latency:    time.Since(start),
		Err:        err,
	}
}

// Check tests reachability + latency and captures the exit IP.
func Check(ctx context.Context, c Candidate, ipURL string, timeout time.Duration) Result {
	res := Result{
		Addr:      c.Addr,
		Kind:      c.Kind,
		Source:    c.Source,
		Country:   c.Country,
		CheckedAt: time.Now(),
	}
	pr := Probe(ctx, c, ipURL, timeout)
	if pr.Err != nil {
		res.Err = shortErr(pr.Err)
		return res
	}
	if pr.StatusCode != http.StatusOK || pr.Body == "" {
		res.Err = "bad status"
		return res
	}
	ip := net.ParseIP(firstToken(pr.Body))
	if ip == nil {
		res.Err = "invalid exit IP"
		return res
	}
	res.LatencyMs = int(pr.Latency.Milliseconds())
	res.ExitIP = ip.String()
	res.Alive = true
	return res
}

// Speed measures throughput in KB/s by downloading a fixed-size payload.
func Speed(ctx context.Context, c Candidate, speedURL string, timeout time.Duration) (int, error) {
	cl, err := clientFor(c, timeout)
	if err != nil {
		return 0, err
	}
	defer cl.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, speedURL, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := cl.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("http %d", resp.StatusCode)
	}
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 1_000_000))
	if err != nil {
		return 0, err
	}
	secs := time.Since(start).Seconds()
	if secs <= 0 || n == 0 {
		return 0, nil
	}
	return int(float64(n) / 1024.0 / secs), nil
}

// RealIP returns the caller's real public IP (direct connection).
func RealIP(ctx context.Context, ipURL string, timeout time.Duration) string {
	cl := &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
		Timeout:   timeout,
	}
	defer cl.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipURL, nil)
	if err != nil {
		return ""
	}
	resp, err := cl.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 257))
	if err != nil || len(body) > 256 {
		return ""
	}
	ip := net.ParseIP(firstToken(string(body)))
	if ip == nil {
		return ""
	}
	return ip.String()
}

// Anonymity classifies how much the proxy reveals about the client.
func Anonymity(ctx context.Context, c Candidate, headersURL, realIP string, timeout time.Duration) string {
	if realIP == "" {
		return ""
	}
	pr := Probe(ctx, c, headersURL, timeout)
	if pr.Err != nil || pr.StatusCode != http.StatusOK {
		return ""
	}
	body := strings.ToLower(pr.Body)
	if strings.Contains(body, realIP) {
		return AnonTransparent
	}
	if strings.Contains(body, "x-forwarded-for") || strings.Contains(body, "via") || strings.Contains(body, "forwarded") {
		return AnonAnonymous
	}
	return AnonElite
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && len(s)-i < 60 {
		// keep the deepest, most useful part
		return strings.TrimSpace(s[i+2:])
	}
	return s
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \n\r\t"); i >= 0 {
		return s[:i]
	}
	return s
}
