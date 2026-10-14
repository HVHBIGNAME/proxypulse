package main

import (
	"cmp"
	"slices"
	"strings"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	"github.com/lxn/walk"
)

// All model access belongs to the UI thread. items is the current view, not the
// authoritative scan: filtering and sorting must never change action targets.
type resultModel struct {
	walk.TableModelBase
	walk.SorterBase
	all     []proxy.Result
	items   []proxy.Result
	ranks   map[string]int
	query   string
	kind    string
	quality int
	before  func()
	after   func()
}

func newResultModel() *resultModel {
	m := &resultModel{}
	_ = m.SorterBase.Sort(3, walk.SortAscending)
	return m
}

func (m *resultModel) RowCount() int { return len(m.items) }

func (m *resultModel) ID(row int) interface{} {
	if r, ok := m.result(row); ok {
		return r.URL()
	}
	return nil
}

func (m *resultModel) result(row int) (proxy.Result, bool) {
	if row < 0 || row >= len(m.items) {
		return proxy.Result{}, false
	}
	return m.items[row], true
}

func (m *resultModel) index(key string) int {
	for i, r := range m.items {
		if r.URL() == key {
			return i
		}
	}
	return -1
}

func (m *resultModel) Value(row, col int) interface{} {
	r, ok := m.result(row)
	if !ok {
		return ""
	}
	switch col {
	case 0:
		return m.ranks[r.URL()]
	case 1:
		return r.Addr
	case 2:
		return strings.ToUpper(r.Kind)
	case 3:
		return r.LatencyMs
	case 4:
		if r.SpeedKBs > 0 {
			return r.SpeedKBs
		}
		return "—"
	case 5:
		return anonymityLabel(r.Anonymity)
	case 6:
		return available(r.ExitIP)
	case 7:
		return available(r.Country)
	}
	return ""
}

func available(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func anonymityLabel(s string) string {
	switch s {
	case proxy.AnonElite:
		return "Элитный"
	case proxy.AnonAnonymous:
		return "Анонимный"
	case proxy.AnonTransparent:
		return "Прозрачный"
	default:
		return "Не определена"
	}
}

func (m *resultModel) Set(rs []proxy.Result) {
	m.all = slices.Clone(rs)
	m.ranks = make(map[string]int, len(rs))
	for i, r := range rs {
		m.ranks[r.URL()] = i + 1
	}
	m.rebuild(m.SortedColumn(), m.SortOrder())
}

func (m *resultModel) Filter(query, kind string, quality int) {
	m.query, m.kind, m.quality = strings.ToLower(strings.TrimSpace(query)), kind, quality
	m.rebuild(m.SortedColumn(), m.SortOrder())
}

func (m *resultModel) Sort(col int, order walk.SortOrder) error {
	m.rebuild(col, order)
	return m.SorterBase.Sort(col, order)
}

func (m *resultModel) matches(r proxy.Result) bool {
	if m.kind != "" && r.Kind != m.kind {
		return false
	}
	if m.quality == 1 && r.LatencyMs >= 500 {
		return false
	}
	if m.quality == 2 && r.Anonymity != proxy.AnonElite {
		return false
	}
	if m.quality == 3 && r.SpeedKBs <= 0 {
		return false
	}
	haystack := strings.ToLower(strings.Join([]string{r.URL(), r.Country, r.ExitIP, r.Source, r.Anonymity, anonymityLabel(r.Anonymity)}, " "))
	for _, term := range strings.Fields(m.query) {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}

func (m *resultModel) rebuild(col int, order walk.SortOrder) {
	if m.before != nil {
		m.before()
	}
	m.items = m.items[:0]
	for _, r := range m.all {
		if m.matches(r) {
			m.items = append(m.items, r)
		}
	}
	slices.SortStableFunc(m.items, func(a, b proxy.Result) int {
		var n int
		switch col {
		case 1:
			n = cmp.Compare(a.Addr, b.Addr)
		case 2:
			n = cmp.Compare(a.Kind, b.Kind)
		case 3:
			n = cmp.Compare(a.LatencyMs, b.LatencyMs)
		case 4:
			n = cmp.Compare(a.SpeedKBs, b.SpeedKBs)
		case 5:
			n = cmp.Compare(anonymityLabel(a.Anonymity), anonymityLabel(b.Anonymity))
		case 6:
			n = cmp.Compare(a.ExitIP, b.ExitIP)
		case 7:
			n = cmp.Compare(a.Country, b.Country)
		default:
			n = cmp.Compare(m.ranks[a.URL()], m.ranks[b.URL()])
		}
		if order == walk.SortDescending {
			n = -n
		}
		return n
	})
	m.PublishRowsReset()
	if m.after != nil {
		m.after()
	}
}

func upsert(rs []proxy.Result, r proxy.Result) []proxy.Result {
	for i := range rs {
		if rs[i].URL() == r.URL() {
			rs[i] = r
			return rs
		}
	}
	return append(rs, r)
}
