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
	"sync"
	"syscall"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/app"
	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	"github.com/HVHBIGNAME/proxypulse/internal/sources"
	"github.com/HVHBIGNAME/proxypulse/internal/store"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("proxypulse", flag.ContinueOnError)
	var (
		srcFlag     = flags.String("sources", "", "comma-separated source IDs (default: all pools)")
		workers     = flags.Int("workers", 300, "concurrent proxy checks")
		timeout     = flags.Duration("timeout", 4*time.Second, "per-proxy timeout")
		limit       = flags.Int("limit", 0, "max candidates to test (0 = all)")
		top         = flags.Int("top", 10, "how many results to print")
		speed       = flags.Bool("speed", true, "measure throughput of survivors")
		anon        = flags.Bool("anon", true, "classify anonymity of survivors")
		jsonOut     = flags.String("json", "", "write results to this JSON file")
		textOut     = flags.String("out", "", "write proxy URLs (one per line) to this file")
		dbPath      = flags.String("db", "", "result cache file path")
		listSrc     = flags.Bool("list-sources", false, "print known pools and exit")
		dbOnly      = flags.Bool("db-only", false, "skip fetching, rank what is already cached")
		minSpeed    = flags.Int("min-speed", 0, "drop survivors slower than this KB/s")
		quiet       = flags.Bool("quiet", false, "suppress progress output")
		best        = flags.Bool("best", false, "print ONLY the single best proxy URL and exit (machine-readable)")
		format      = flags.String("format", "text", "with -best: text|json")
		showVersion = flags.Bool("version", false, "print version and exit")
	)
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Printf("ProxyPulse %s\n", version)
		return nil
	}
	switch {
	case *top < 0:
		return fmt.Errorf("-top must be non-negative (got %d)", *top)
	case *workers <= 0:
		return fmt.Errorf("-workers must be greater than zero (got %d)", *workers)
	case *timeout <= 0:
		return fmt.Errorf("-timeout must be greater than zero (got %s)", *timeout)
	case *limit < 0:
		return fmt.Errorf("-limit must be non-negative; 0 means all candidates (got %d)", *limit)
	case *format != "text" && *format != "json":
		return fmt.Errorf("unsupported -format %q: use text or json", *format)
	}

	if *listSrc {
		for _, s := range sources.Registry() {
			fmt.Printf("%-20s %-7s %s\n", s.ID, s.Kind, s.Name)
		}
		return nil
	}
	if *best {
		// Machine-readable mode: silence progress, print one proxy, exit.
		*quiet = true
		*top = 1
	}

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
		progress("fetching %d pool(s)...\n", len(sources.Registry()))
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

	opt := app.Options{
		Workers:   *workers,
		Timeout:   *timeout,
		SpeedTest: *speed,
		Anonymity: *anon,
	}

	var mu sync.Mutex
	var checked, alive int
	var checkedResults []proxy.Result
	total := len(cands)
	start := time.Now()
	live := app.Validate(ctx, cands, opt, func(r proxy.Result) {
		mu.Lock()
		defer mu.Unlock()
		checked++
		if r.Alive {
			alive++
		}
		// Cancellation is not evidence that a cached proxy has died.
		if r.Alive || ctx.Err() == nil {
			checkedResults = append(checkedResults, r)
		}
		if !*quiet && (checked%50 == 0 || checked == total) {
			fmt.Fprintf(os.Stderr, "\r  checked %d/%d  alive %d  ...", checked, total, alive)
		}
	})
	if !*quiet {
		fmt.Fprintln(os.Stderr)
	}

	if opt.Anonymity && len(live) > 0 && ctx.Err() == nil {
		opt.DirectIP = proxy.RealIP(ctx, proxy.DefaultIPURL, 6*time.Second)
		if opt.DirectIP != "" {
			progress("your real IP: %s\n", opt.DirectIP)
		}
	}
	ranked := app.Finalise(ctx, live, opt)
	// Enrichment replaces reachability-only rows; output filters must not affect the cache.
	if err := db.Save(append(checkedResults, ranked...)); err != nil {
		progress("warn: db save: %v\n", err)
	}
	if *minSpeed > 0 {
		filtered := ranked[:0]
		for _, r := range ranked {
			if r.SpeedKBs >= *minSpeed {
				filtered = append(filtered, r)
			}
		}
		ranked = filtered
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
