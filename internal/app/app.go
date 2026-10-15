// Package app wires sources + checking into a scan pipeline.
package app

import (
	"context"
	"sort"
	"sync"
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
	SpeedTop     int             // speed-test only the N fastest (0 = 40, negative = all)
	Anonymity    bool            // classify anonymity for survivors
	AnonTop      int             // anonymity-test only the N fastest (0 = 100, negative = all)
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
	return FetchWithCap(ctx, selected, timeout, sources.DefaultCapPerSource)
}

func FetchWithCap(ctx context.Context, selected []string, timeout time.Duration, perSource int) ([]proxy.Candidate, []sources.FetchResult, error) {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return sources.FetchAllWithCap(ctx, selected, timeout, 12, perSource)
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
		sem   = make(chan struct{}, min(opt.Workers, len(cands)))
		mu    sync.Mutex
		alive []proxy.Result
	)
	for _, c := range cands {
		if !acquire(ctx, sem) {
			break
		}
		wg.Add(1)
		go func(c proxy.Candidate) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			res := proxy.Check(ctx, c, opt.IPURL, opt.Timeout)
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
	if !opt.SpeedTest {
		speedN = 0
	}
	if opt.SpeedTop > 0 && opt.SpeedTop < speedN {
		speedN = opt.SpeedTop
	}
	anonN := len(out)
	if !opt.Anonymity || opt.DirectIP == "" {
		anonN = 0
	}
	if opt.AnonTop > 0 && opt.AnonTop < anonN {
		anonN = opt.AnonTop
	}
	n := speedN
	if anonN > n {
		n = anonN
	}

	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, min(n, min(opt.Workers, 64)))
	)
	for i := 0; i < n; i++ {
		if !acquire(ctx, sem) {
			break
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			c := proxy.Candidate{Addr: out[i].Addr, Kind: out[i].Kind, Source: out[i].Source, Country: out[i].Country}
			if opt.SpeedTest && i < speedN {
				if kbs, err := proxy.Speed(ctx, c, opt.SpeedURL, opt.Timeout*4); err == nil {
					out[i].SpeedKBs = kbs
				}
			}
			if i < anonN && ctx.Err() == nil {
				anonymity := proxy.Anonymity(ctx, c, opt.HeadersURL, opt.DirectIP, opt.Timeout*2)
				if ctx.Err() == nil {
					out[i].Anonymity = anonymity
				}
			}
		}(i)
	}
	wg.Wait()
	return out
}

func acquire(ctx context.Context, sem chan struct{}) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case sem <- struct{}{}:
		if ctx.Err() != nil {
			<-sem
			return false
		}
		return true
	}
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
