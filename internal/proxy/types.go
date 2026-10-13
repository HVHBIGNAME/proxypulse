// Package proxy provides proxy candidate/result models and live checking.
package proxy

import "time"

// Supported proxy kinds.
const (
	KindHTTP   = "http"
	KindSOCKS5 = "socks5"
)

// Anonymity levels reported for a checked proxy.
const (
	AnonElite       = "elite"       // only proxy IP seen, no forwarding headers
	AnonAnonymous   = "anonymous"   // proxy identified but real IP hidden
	AnonTransparent = "transparent" // real client IP leaked
)

// Candidate is a not-yet-tested proxy endpoint.
type Candidate struct {
	Addr    string `json:"addr"`
	Kind    string `json:"kind"`
	Source  string `json:"source"`
	Country string `json:"country,omitempty"`
}

// Key uniquely identifies an endpoint (kind + address).
func (c Candidate) Key() string { return c.Kind + "://" + c.Addr }

// Result is the outcome of testing a candidate.
type Result struct {
	Addr      string    `json:"addr"`
	Kind      string    `json:"kind"`
	Source    string    `json:"source"`
	Alive     bool      `json:"alive"`
	LatencyMs int       `json:"latency_ms"`
	SpeedKBs  int       `json:"speed_kbs,omitempty"`
	ExitIP    string    `json:"exit_ip,omitempty"`
	Country   string    `json:"country,omitempty"`
	Anonymity string    `json:"anonymity,omitempty"`
	Err       string    `json:"err,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// URL returns the full proxy URL, ready to paste into other tools.
func (r Result) URL() string { return r.Kind + "://" + r.Addr }
