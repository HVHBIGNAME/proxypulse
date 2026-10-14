package main

import (
	"slices"
	"testing"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	"github.com/lxn/walk"
)

func sampleResults() []proxy.Result {
	return []proxy.Result{
		{Addr: "192.0.2.10:8080", Kind: proxy.KindHTTP, Source: "alpha", Alive: true, LatencyMs: 95, Country: "DE", ExitIP: "203.0.113.9", Anonymity: proxy.AnonElite, SpeedKBs: 3200},
		{Addr: "192.0.2.20:1080", Kind: proxy.KindSOCKS5, Source: "beta", Alive: true, LatencyMs: 260, Country: "NL", ExitIP: "203.0.113.2", Anonymity: proxy.AnonAnonymous, SpeedKBs: 800},
		{Addr: "192.0.2.30:80", Kind: proxy.KindHTTP, Source: "alpha", Alive: true, LatencyMs: 500, Country: "US", Anonymity: proxy.AnonTransparent},
		{Addr: "192.0.2.40:3128", Kind: proxy.KindHTTP, Source: "gamma", Alive: true, LatencyMs: 1800},
	}
}

func TestResultFiltersAndActionTargets(t *testing.T) {
	results := sampleResults()
	m := newResultModel()
	m.Set(results)
	cases := []struct {
		name, query, kind string
		quality           int
		want              []int
	}{
		{"protocol", "", proxy.KindSOCKS5, 0, []int{1}},
		{"Russian multiword", " ЭЛИТНЫЙ de alpha ", "", 0, []int{0}},
		{"exit IP", "203.0.113.2", "", 0, []int{1}},
		{"latency boundary", "", "", 1, []int{0, 1}},
		{"elite", "", "", 2, []int{0}},
		{"measured speed", "", "", 3, []int{0, 1}},
		{"combined excludes", "DE", proxy.KindSOCKS5, 2, nil},
		{"empty search", "", "", 0, []int{0, 1, 2, 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m.Filter(tc.query, tc.kind, tc.quality)
			if m.RowCount() != len(tc.want) {
				t.Fatalf("got %d results, want %d", m.RowCount(), len(tc.want))
			}
			for row, index := range tc.want {
				got, ok := m.result(row)
				if !ok || got.URL() != results[index].URL() {
					t.Fatalf("row %d action target = %q, want %q", row, got.URL(), results[index].URL())
				}
				if m.Value(row, 0) != index+1 {
					t.Errorf("global rank changed after filtering: %v", m.Value(row, 0))
				}
			}
		})
	}
	if _, ok := m.result(-1); ok {
		t.Fatal("no-selection index resolved to a proxy")
	}
	if _, ok := m.result(m.RowCount()); ok {
		t.Fatal("out-of-range index resolved to a proxy")
	}
}

func TestResultSortPersistsAcrossUpdates(t *testing.T) {
	results := sampleResults()
	original := slices.Clone(results)
	m := newResultModel()
	m.Set(results)
	if err := m.Sort(6, walk.SortDescending); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.result(0); got.ExitIP != "203.0.113.9" {
		t.Fatalf("exit IP sort used a different column: %+v", got)
	}
	if err := m.Sort(3, walk.SortDescending); err != nil {
		t.Fatal(err)
	}
	m.Filter("alpha", "", 0)
	if got, _ := m.result(0); got.URL() != results[2].URL() {
		t.Fatal("filter reset the descending sort or changed its action target")
	}
	results[0].LatencyMs = 900
	m.Set(results)
	if got, _ := m.result(0); got.URL() != results[0].URL() {
		t.Fatal("updated results lost the active sort/filter")
	}
	results[0] = proxy.Result{}
	if got, _ := m.result(0); got.Addr != original[0].Addr {
		t.Fatal("model retained a mutable worker-owned result slice")
	}
}

func TestUpsertSeparatesProtocols(t *testing.T) {
	a := proxy.Result{Addr: "192.0.2.1:1080", Kind: proxy.KindHTTP, LatencyMs: 20}
	b := a
	b.Kind = proxy.KindSOCKS5
	results := upsert([]proxy.Result{a}, b)
	b.LatencyMs = 10
	results = upsert(results, b)
	if len(results) != 2 || results[0] != a || results[1] != b {
		t.Fatalf("protocol identity lost: %+v", results)
	}
}

func TestExportPath(t *testing.T) {
	for _, tc := range []struct {
		path   string
		filter int
		want   string
	}{
		{"proxies", 1, "proxies.txt"}, {"proxies", 2, "proxies.json"},
		{"proxies.JSON", 1, "proxies.JSON"}, {"proxies.txt", 2, "proxies.txt"},
	} {
		if got := exportPath(tc.path, tc.filter); got != tc.want {
			t.Errorf("exportPath(%q, %d) = %q, want %q", tc.path, tc.filter, got, tc.want)
		}
	}
}
