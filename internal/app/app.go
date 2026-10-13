// Package app wires sources + checking into a scan pipeline.
package app

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	"github.com/HVHBIGNAME/proxypulse/internal/sources"
)

// Options controls a scan run.
type Options struct {
	Sources      []string        // source IDs; empty = all
	Workers      int             // concurrent validators
	Timeout      time.Duration   // per-proxy timeout
	Limit        int             // max candidates to test (0 = no limit)
	SpeedTest    bool            // measure throughput for survivors
	SpeedTop     int             // speed-test only the N fastest (0 = all)
	Anonymity    bool            // classify anonymity for survivors
	AnonTop      int             // anonymity-test only the N fastest (0 = all)
	IPURL        string          // target used for reachability + exit IP
	SpeedURL     string          // target used for throughput
	HeadersURL   string          // target used for anonymity
	DirectIP     string          // caller's real IP (for anonymity detection)
	OnCandidates func(total int) // optional progress callback after fetch
}

func (o *Options) withDefaults() {
	if o.Workers < 1 {
		o.Workers = 200
	}
	if o.Timeout <= 0 {
		o.Timeout = 5 * time.Second
	}
	if o.IPURL == "" {
		o.IPURL = proxy.DefaultIPURL
	}
	if o.SpeedURL == "" {
		o.SpeedURL = proxy.DefaultSpeedURL
	}
	if o.HeadersURL == "" {
		o.HeadersURL = proxy.DefaultHeadersURL
	}
	if o.SpeedTop == 0 {
		o.SpeedTop = 40
	}
	if o.AnonTop == 0 {
		o.AnonTop = 100
	}
}

// Fetch pulls candidates from the selected sources.
func Fetch(ctx context.Context, selected []string, timeout time.Duration) ([]proxy.Candidate, []sources.FetchResult, error) {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return sources.FetchAll(ctx, selected, timeout, 10)
}

// Validate tests every candidate with the given concurrency, invoking onResult
// for each finished proxy (may be called from multiple goroutines).
func Validate(ctx context.Context, cands []proxy.Candidate, opt Options, onResult func(proxy.Result)) []proxy.Result {
	opt.withDefaults()
	if len(cands) == 0 {
		return nil
	}
	var (
		wg    sync.WaitGroup
		sem   = make(chan struct{}, opt.Workers)
		mu    sync.Mutex
		alive []proxy.Result
		done  int64
	)
	total := int64(len(cands))
	for _, c := range cands {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(c proxy.Candidate) {
			defer wg.Done()
			defer func() { <-sem }()
			res := proxy.Check(ctx, c, opt.IPURL, opt.Timeout)
			n := atomic.AddInt64(&done, 1)
			_ = n
			_ = total
			if res.Alive {
				mu.Lock()
				alive = append(alive, res)
				mu.Unlock()
			}
			if onResult != nil {
				onResult(res)
			}
		}(c)
	}
	wg.Wait()
	return alive
}

// Finalise enriches survivors (speed + anonymity) concurrently and returns them
// sorted fastest-first. Speed/anonymity probing is capped to the fastest N to
// keep a full scan fast.
func Finalise(ctx context.Context, alive []proxy.Result, opt Options) []proxy.Result {
	opt.withDefaults()
	out := make([]proxy.Result, len(alive))
	copy(out, alive)
	Sort(out)

	speedN := len(out)
	if opt.SpeedTop > 0 && opt.SpeedTop < speedN {
		speedN = opt.SpeedTop
	}
	anonN := len(out)
	if opt.AnonTop > 0 && opt.AnonTop < anonN {
		anonN = opt.AnonTop
	}
	n := speedN
	if anonN > n {
		n = anonN
	}

	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, min(opt.Workers, 64))
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			c := proxy.Candidate{Addr: out[i].Addr, Kind: out[i].Kind, Source: out[i].Source, Country: out[i].Country}
			if opt.SpeedTest && i < speedN {
				if kbs, err := proxy.Speed(ctx, c, opt.SpeedURL, opt.Timeout*4); err == nil {
					out[i].SpeedKBs = kbs
				}
			}
			if opt.Anonymity && i < anonN && opt.DirectIP != "" {
				out[i].Anonymity = proxy.Anonymity(ctx, c, opt.HeadersURL, opt.DirectIP, opt.Timeout*2)
			}
		}(i)
	}
	wg.Wait()
	return out
}

// Sort orders results fastest-first.
func Sort(rs []proxy.Result) {
	sort.SliceStable(rs, func(i, j int) bool {
		return rs[i].LatencyMs < rs[j].LatencyMs
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
