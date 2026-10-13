// Command proxypulse finds, checks and ranks free public proxies.
//
// One command does the whole job: fetch every pool, validate each proxy in
// parallel, rank survivors by latency, and hand you a ready-to-use proxy.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/app"
	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	"github.com/HVHBIGNAME/proxypulse/internal/sources"
	"github.com/HVHBIGNAME/proxypulse/internal/store"
)

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		srcFlag  = flag.String("sources", "", "comma-separated source IDs (default: all pools)")
		workers  = flag.Int("workers", 300, "concurrent proxy checks")
		timeout  = flag.Duration("timeout", 4*time.Second, "per-proxy timeout")
		limit    = flag.Int("limit", 0, "max candidates to test (0 = all)")
		top      = flag.Int("top", 10, "how many results to print")
		speed    = flag.Bool("speed", true, "measure throughput of survivors")
		anon     = flag.Bool("anon", true, "classify anonymity of survivors")
		jsonOut  = flag.String("json", "", "write results to this JSON file")
		textOut  = flag.String("out", "", "write proxy URLs (one per line) to this file")
		dbPath   = flag.String("db", "", "result cache file path")
		listSrc  = flag.Bool("list-sources", false, "print known pools and exit")
		dbOnly   = flag.Bool("db-only", false, "skip fetching, rank what is already cached")
		minSpeed = flag.Int("min-speed", 0, "drop survivors slower than this KB/s")
		quiet    = flag.Bool("quiet", false, "suppress progress output")
		best     = flag.Bool("best", false, "print ONLY the single best proxy URL and exit (machine-readable)")
		format   = flag.String("format", "text", "with -best: text|json")
	)
	flag.Parse()

	if *listSrc {
		for _, s := range sources.Registry {
			fmt.Printf("%-20s %-7s %s\n", s.ID, s.Kind, s.Name)
		}
		return nil
	}
	if *best {
		// Machine-readable mode: silence progress, print one proxy, exit.
		*quiet = true
		*top = 1
		*workers = max(*workers, 300)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *dbPath == "" {
		*dbPath = defaultDBPath()
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	progress := func(format string, a ...any) {
		if !*quiet {
			fmt.Fprintf(os.Stderr, format, a...)
		}
	}

	var cands []proxy.Candidate
	if *dbOnly {
		cached, err := db.Top(100000)
		if err != nil {
			return err
		}
		for _, r := range cached {
			cands = append(cands, proxy.Candidate{Addr: r.Addr, Kind: r.Kind, Source: r.Source, Country: r.Country})
		}
		progress("re-checking %d cached proxies\n", len(cands))
	} else {
		selected := splitCSV(*srcFlag)
		progress("fetching %d pool(s)...\n", len(sources.Registry))
		var results []sources.FetchResult
		cands, results, err = app.Fetch(ctx, selected, 25*time.Second)
		if err != nil {
			return err
		}
		for _, r := range results {
			status := fmt.Sprintf("%d proxies", r.Count)
			if r.Err != nil {
				status = "ERR " + r.Err.Error()
			}
			progress("  %-20s %s\n", r.Source.ID, status)
		}
		progress("total candidates (deduped): %d\n", len(cands))
	}

	if *limit > 0 && len(cands) > *limit {
		cands = cands[:*limit]
		progress("limited to %d candidates\n", *limit)
	}

	directIP := proxy.RealIP(context.Background(), proxy.DefaultIPURL, 6*time.Second)
	if directIP != "" {
		progress("your real IP: %s\n", directIP)
	}

	opt := app.Options{
		Workers:   *workers,
		Timeout:   *timeout,
		SpeedTest: *speed,
		Anonymity: *anon,
		DirectIP:  directIP,
	}

	var checked, alive int64
	total := int64(len(cands))
	start := time.Now()
	live := app.Validate(ctx, cands, opt, func(r proxy.Result) {
		n := atomic.AddInt64(&checked, 1)
		if r.Alive {
			atomic.AddInt64(&alive, 1)
		}
		if !*quiet && (n%50 == 0 || n == total) {
			fmt.Fprintf(os.Stderr, "\r  checked %d/%d  alive %d  ...", n, total, atomic.LoadInt64(&alive))
		}
	})
	if !*quiet {
		fmt.Fprintln(os.Stderr)
	}

	ranked := app.Finalise(ctx, live, opt)
	if *minSpeed > 0 {
		filtered := ranked[:0]
		for _, r := range ranked {
			if r.SpeedKBs >= *minSpeed {
				filtered = append(filtered, r)
			}
		}
		ranked = filtered
	}

	if err := db.Save(ranked); err != nil {
		progress("warn: db save: %v\n", err)
	}

	elapsed := time.Since(start)
	progress("done in %s: %d/%d alive\n", elapsed.Round(time.Millisecond), len(ranked), total)

	// Machine-readable mode: emit exactly one proxy (or nothing) and exit.
	if *best {
		if len(ranked) == 0 {
			return fmt.Errorf("no working proxy found")
		}
		b := ranked[0]
		if *format == "json" {
			out, _ := json.MarshalIndent(b, "", "  ")
			fmt.Println(string(out))
		} else {
			fmt.Println(b.URL())
		}
		return nil
	}

	printTable(ranked, *top)

	// Always leave the single fastest proxy in best.txt for a true one-click copy.
	if len(ranked) > 0 {
		best := filepath.Join(filepath.Dir(*dbPath), "best.txt")
		if err := store.ExportText(best, ranked[:1]); err == nil {
			fmt.Printf("\nBEST → %s  (saved to %s)\n", ranked[0].URL(), best)
		}
	}
	if *textOut != "" {
		if err := store.ExportText(*textOut, ranked); err != nil {
			return err
		}
		fmt.Printf("wrote %d proxies to %s\n", len(ranked), *textOut)
	}
	if *jsonOut != "" {
		if err := store.ExportJSON(*jsonOut, ranked); err != nil {
			return err
		}
		fmt.Printf("wrote JSON to %s\n", *jsonOut)
	}
	return nil
}

func printTable(rs []proxy.Result, top int) {
	if len(rs) == 0 {
		fmt.Println("no working proxies found this round")
		return
	}
	if top > len(rs) {
		top = len(rs)
	}
	fmt.Printf("\n%-4s %-24s %-7s %8s %9s %-11s %-15s\n", "#", "PROXY", "KIND", "LATENCY", "SPEED", "ANONYMITY", "EXIT IP")
	for i, r := range rs[:top] {
		speed := "-"
		if r.SpeedKBs > 0 {
			speed = fmt.Sprintf("%d KB/s", r.SpeedKBs)
		}
		anon := r.Anonymity
		if anon == "" {
			anon = "-"
		}
		exit := r.ExitIP
		if exit == "" {
			exit = "-"
		}
		fmt.Printf("%-4d %-24s %-7s %7dms %9s %-11s %-15s\n", i+1, r.Addr, r.Kind, r.LatencyMs, speed, anon, exit)
	}
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func defaultDBPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	if base == "" {
		base = "."
	}
	return filepath.Join(base, "ProxyPulse", "proxies.json")
}
