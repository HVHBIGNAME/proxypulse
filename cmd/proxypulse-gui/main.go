// Command proxypulse-gui is a two-click Windows desktop front-end for the
// ProxyPulse engine: press one button, get a working proxy on your clipboard.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/app"
	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	"github.com/HVHBIGNAME/proxypulse/internal/store"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// ---------- table model ----------

type resultModel struct {
	walk.TableModelBase
	walk.SorterBase
	items []proxy.Result
}

func (m *resultModel) RowCount() int { return len(m.items) }

func (m *resultModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.items) {
		return ""
	}
	r := m.items[row]
	switch col {
	case 0:
		return row + 1
	case 1:
		return r.Addr
	case 2:
		return strings.ToUpper(r.Kind)
	case 3:
		return r.LatencyMs
	case 4:
		return r.SpeedKBs
	case 5:
		return r.Anonymity
	case 6:
		return r.ExitIP
	case 7:
		return r.Country
	}
	return ""
}

// Sort makes column headers clickable.
func (m *resultModel) Sort(col int, order walk.SortOrder) error {
	_ = m.SorterBase.Sort(col, order)
	asc := order == walk.SortAscending
	less := func(i, j int) bool {
		a, b := m.items[i], m.items[j]
		switch col {
		case 4:
			return a.SpeedKBs < b.SpeedKBs
		case 1:
			return a.Addr < b.Addr
		case 2:
			return a.Kind < b.Kind
		case 5:
			return a.Anonymity < b.Anonymity
		case 7:
			return a.Country < b.Country
		default:
			return a.LatencyMs < b.LatencyMs
		}
	}
	sort.SliceStable(m.items, func(i, j int) bool {
		if asc {
			return less(i, j)
		}
		return less(j, i)
	})
	m.PublishRowsReset()
	return nil
}

func (m *resultModel) Set(rs []proxy.Result) {
	m.items = append([]proxy.Result(nil), rs...)
	m.PublishRowsReset()
}

// ---------- cell styling ----------

type cellStyler struct{ m *resultModel }

func (s cellStyler) StyleCell(style *walk.CellStyle) {
	row, col := style.Row(), style.Col()
	if row < 0 || row >= len(s.m.items) {
		return
	}
	r := s.m.items[row]
	switch col {
	case 0, 1:
		if r.Alive {
			style.TextColor = walk.RGB(0, 130, 0)
		}
	case 3:
		switch {
		case r.LatencyMs < 500:
			style.TextColor = walk.RGB(0, 140, 0)
		case r.LatencyMs < 1500:
			style.TextColor = walk.RGB(200, 130, 0)
		default:
			style.TextColor = walk.RGB(170, 60, 60)
		}
	case 4:
		if r.SpeedKBs >= 1000 {
			style.TextColor = walk.RGB(0, 140, 0)
		}
	}
}

// ---------- app ----------

type state struct {
	results  []proxy.Result
	db       *store.DB
	scanning bool
	cancel   context.CancelFunc
}

func main() {
	if lf, err := os.OpenFile(filepath.Join(appDir(), "proxypulse.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		log.SetOutput(lf)
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	st := &state{}
	db, err := store.Open(filepath.Join(appDir(), "proxies.json"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	st.db = db
	defer db.Close()

	model := &resultModel{}

	var (
		mw       *walk.MainWindow
		tv       *walk.TableView
		status   *walk.Label
		bar      *walk.ProgressBar
		bestLbl  *walk.Label
		autoChk  *walk.CheckBox
		limitTE  *walk.LineEdit
		anonChk  *walk.CheckBox
		speedChk *walk.CheckBox
	)

	ui := func(fn func()) { mw.Synchronize(fn) }

	// Auto-refresh loop driven by the checkbox.
	go func() {
		for range time.Tick(60 * time.Second) {
			if autoChk != nil && autoChk.Checked() && !st.scanning {
				ui(func() { goStartScan(st, model, ui, status, bar, bestLbl, limitTE, anonChk, speedChk, true) })
			}
		}
	}()

	if err := (MainWindow{
		AssignTo: &mw,
		Title:    "ProxyPulse — быстрый поиск рабочих прокси",
		MinSize:  Size{1040, 580},
		Size:     Size{1180, 660},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout: HBox{},
				Children: []Widget{
					PushButton{
						Text:      "НАЙТИ ПРОКСИ",
						MinSize:   Size{180, 42},
						Font:      Font{PointSize: 11, Bold: true},
						OnClicked: func() { goStartScan(st, model, ui, status, bar, bestLbl, limitTE, anonChk, speedChk, false) },
					},
					PushButton{
						Text:    "ПРОВЕРИТЬ ВЫБРАННЫЙ",
						MinSize: Size{200, 42},
						OnClicked: func() {
							goRecheckSelected(st, model, ui, tv, status, bestLbl, anonChk, speedChk)
						},
					},
					PushButton{
						Text:      "КОПИРОВАТЬ ТОП",
						MinSize:   Size{170, 42},
						OnClicked: func() { copyRow(st, 0, status) },
					},
					PushButton{
						Text:    "СТОП",
						MinSize: Size{110, 42},
						OnClicked: func() {
							if st.cancel != nil {
								st.cancel()
								status.SetText("Остановлено пользователем.")
							}
						},
					},
					PushButton{
						Text:      "СОХРАНИТЬ ВСЕ",
						MinSize:   Size{150, 42},
						OnClicked: func() { saveAll(st, mw, status) },
					},
					HSpacer{},
					Label{Text: "HVHBIGNAME/proxypulse", TextColor: walk.RGB(120, 120, 120)},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Проверять кандидатов:"},
					LineEdit{AssignTo: &limitTE, Text: "1500", MaxLength: 6, MinSize: Size{70, 0}},
					CheckBox{AssignTo: &speedChk, Text: "замер скорости", Checked: true},
					CheckBox{AssignTo: &anonChk, Text: "проверка анонимности", Checked: true},
					CheckBox{AssignTo: &autoChk, Text: "автообновление раз в минуту", Checked: false},
					HSpacer{},
					Label{AssignTo: &status, Text: "Готово. Нажмите «НАЙТИ ПРОКСИ».", TextAlignment: AlignFar},
				},
			},
			ProgressBar{AssignTo: &bar, MaxValue: 100},
			TableView{
				AssignTo:         &tv,
				AlternatingRowBG: true,
				ColumnsOrderable: true,
				ColumnsSizable:   true,
				MinSize:          Size{0, 380},
				Model:            model,
				CellStyler:       cellStyler{model},
				OnItemActivated: func() {
					if i := tv.CurrentIndex(); i >= 0 {
						copyRow(st, i, status)
					}
				},
				Columns: []TableViewColumn{
					{Title: "#", Width: 45},
					{Title: "ПРОКСИ", Width: 235},
					{Title: "ТИП", Width: 80},
					{Title: "ПИНГ, мс", Width: 95, Alignment: AlignFar},
					{Title: "СКОРОСТЬ, КБ/с", Width: 130, Alignment: AlignFar},
					{Title: "АНОНИМНОСТЬ", Width: 130},
					{Title: "ВЫХОДНОЙ IP", Width: 150},
					{Title: "СТРАНА", Width: 70},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Лучший прокси:", Font: Font{Bold: true}},
					Label{AssignTo: &bestLbl, Text: "—"},
					HSpacer{},
					Label{Text: "двойной клик по строке — скопировать прокси в буфер", TextColor: walk.RGB(110, 110, 110)},
				},
			},
		},
	}.Create()); err != nil {
		return err
	}

	if cached, err := db.Top(500); err == nil && len(cached) > 0 {
		model.Set(cached)
		bestLbl.SetText(cached[0].URL())
		n, total, _ := db.Count()
		status.SetText(fmt.Sprintf("Из кэша: живых %d из %d. Нажмите «НАЙТИ ПРОКСИ» для свежих.", n, total))
	}

	mw.Run()
	return nil
}

// ---------- actions ----------

func goStartScan(st *state, model *resultModel, ui func(func()), status *walk.Label,
	bar *walk.ProgressBar, bestLbl *walk.Label, limitTE *walk.LineEdit,
	anonChk, speedChk *walk.CheckBox, quiet bool) {
	if st.scanning {
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(limitTE.Text()))
	if limit <= 0 {
		limit = 1500
	}
	ctx, cancel := context.WithCancel(context.Background())
	st.cancel = cancel
	st.scanning = true
	if !quiet {
		status.SetText("Скачиваю пулы прокси…")
		bar.SetValue(0)
	}

	go func() {
		defer func() { st.scanning = false }()
		defer cancel()

		cands, fetches, err := app.Fetch(ctx, nil, 25*time.Second)
		if err != nil {
			ui(func() { status.SetText("Ошибка загрузки пулов: " + err.Error()) })
			return
		}
		ok := 0
		for _, f := range fetches {
			if f.Err == nil {
				ok++
			}
		}
		ui(func() {
			status.SetText(fmt.Sprintf("Загружено %d прокси из %d пулов. Проверяю до %d…", len(cands), ok, limit))
		})
		if len(cands) > limit {
			cands = cands[:limit]
		}
		total := len(cands)

		opt := app.Options{
			Workers:   400,
			Timeout:   4 * time.Second,
			SpeedTest: speedChk.Checked(),
			SpeedTop:  60,
			Anonymity: anonChk.Checked(),
			AnonTop:   150,
			DirectIP:  proxy.RealIP(context.Background(), proxy.DefaultIPURL, 6*time.Second),
		}

		var mu sync.Mutex
		var done int
		live := app.Validate(ctx, cands, opt, func(r proxy.Result) {
			mu.Lock()
			done++
			if !r.Alive {
				d, t := done, total
				mu.Unlock()
				ui(func() {
					bar.SetValue(d * 70 / max(t, 1))
					status.SetText(fmt.Sprintf("Проверено %d/%d…", d, t))
				})
				return
			}
			st.results = upsert(st.results, r)
			snap := append([]proxy.Result(nil), st.results...)
			app.Sort(snap)
			d, t := done, total
			mu.Unlock()
			ui(func() {
				model.Set(snap)
				bestLbl.SetText(snap[0].URL())
				bar.SetValue(d * 70 / max(t, 1))
				status.SetText(fmt.Sprintf("Проверено %d/%d, живых %d, лучший пинг %d мс", d, t, len(snap), snap[0].LatencyMs))
			})
		})

		ui(func() {
			status.SetText(fmt.Sprintf("Живых %d. Замеряю скорость и анонимность…", len(live)))
		})
		ranked := app.Finalise(ctx, live, opt)
		st.results = ranked
		if err := st.db.Save(ranked); err != nil {
			log.Println("db save:", err)
		}

		ui(func() {
			model.Set(ranked)
			bar.SetValue(100)
			if len(ranked) > 0 {
				bestLbl.SetText(ranked[0].URL())
				status.SetText(fmt.Sprintf("Готово: %d рабочих из %d. Лучший — %d мс. Жмите «КОПИРОВАТЬ ТОП».", len(ranked), total, ranked[0].LatencyMs))
			} else {
				bestLbl.SetText("—")
				status.SetText("Рабочих прокси не найдено. Попробуйте ещё раз.")
			}
		})
	}()
}

func goRecheckSelected(st *state, model *resultModel, ui func(func()), tv *walk.TableView,
	status *walk.Label, bestLbl *walk.Label, anonChk, speedChk *walk.CheckBox) {
	i := tv.CurrentIndex()
	if i < 0 || i >= len(st.results) {
		status.SetText("Выберите строку в таблице.")
		return
	}
	r := st.results[i]
	go func() {
		ui(func() { status.SetText("Перепроверяю " + r.Addr + "…") })
		opt := app.Options{Workers: 1, Timeout: 6 * time.Second,
			SpeedTest: speedChk.Checked(), SpeedTop: 1,
			Anonymity: anonChk.Checked(), AnonTop: 1,
			DirectIP: proxy.RealIP(context.Background(), proxy.DefaultIPURL, 6*time.Second)}
		res := app.Validate(context.Background(), []proxy.Candidate{{Addr: r.Addr, Kind: r.Kind, Source: r.Source}}, opt, nil)
		if len(res) == 0 {
			ui(func() { status.SetText("Прокси больше не отвечает: " + r.Addr) })
			return
		}
		st.results = upsert(st.results, res[0])
		app.Sort(st.results)
		ui(func() {
			model.Set(st.results)
			bestLbl.SetText(st.results[0].URL())
			status.SetText(fmt.Sprintf("Актуален: %s — %d мс", r.Addr, res[0].LatencyMs))
		})
	}()
}

// copyRow puts the chosen proxy URL on the clipboard — the "one click" payoff.
func copyRow(st *state, i int, status *walk.Label) {
	if i < 0 || i >= len(st.results) {
		status.SetText("Сначала найдите прокси.")
		return
	}
	u := st.results[i].URL()
	if err := walk.Clipboard().SetText(u); err != nil {
		status.SetText("Не удалось скопировать: " + err.Error())
		return
	}
	status.SetText("Скопировано в буфер: " + u + "  (Ctrl+V, чтобы вставить)")
}

func saveAll(st *state, mw *walk.MainWindow, status *walk.Label) {
	if len(st.results) == 0 {
		status.SetText("Нечего сохранять.")
		return
	}
	dlg := new(walk.FileDialog)
	dlg.Title = "Сохранить рабочие прокси"
	dlg.Filter = "Текстовый список (*.txt)|*.txt|JSON (*.json)|*.json"
	dlg.InitialDirPath = appDir()
	dlg.FilePath = filepath.Join(appDir(), "proxies.txt")
	ok, err := dlg.ShowSave(mw)
	if err != nil || !ok {
		return
	}
	if strings.HasSuffix(strings.ToLower(dlg.FilePath), ".json") {
		err = store.ExportJSON(dlg.FilePath, st.results)
	} else {
		err = store.ExportText(dlg.FilePath, st.results)
	}
	if err != nil {
		status.SetText("Ошибка сохранения: " + err.Error())
		return
	}
	status.SetText(fmt.Sprintf("Сохранено %d прокси в %s", len(st.results), dlg.FilePath))
}

// ---------- helpers ----------

func upsert(rs []proxy.Result, r proxy.Result) []proxy.Result {
	for i := range rs {
		if rs[i].Addr == r.Addr && rs[i].Kind == r.Kind {
			rs[i] = r
			return rs
		}
	}
	return append(rs, r)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func appDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = "."
	}
	dir := filepath.Join(base, "ProxyPulse")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}
