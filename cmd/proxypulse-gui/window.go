package main

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/app"
	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	"github.com/HVHBIGNAME/proxypulse/internal/store"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

const (
	animationTimer = 0x5050
	autoTimer      = 0x5051
)

type desktop struct {
	mw                          *walk.MainWindow
	tv                          *walk.TableView
	header, cards, steps, empty *walk.CustomWidget
	status, counts, selected    *walk.Label
	best                        *walk.LineEdit
	search                      *walk.LineEdit
	preset, kind, quality       *walk.ComboBox
	auto                        *walk.ComboBox
	limit                       *walk.NumberEdit
	speed, anon                 *walk.CheckBox
	copy, copyBest, recheck     *walk.PushButton
	export, exportView          *walk.PushButton
	primary                     *actionButton
	model                       *resultModel
	theme                       *theme
	db                          *store.DB
	dir                         string
	results                     []proxy.Result
	phase                       scanPhase
	busy, stopping, closing     bool
	cached, applyingPreset      bool
	cancel                      context.CancelFunc
	mailbox                     *scanMailbox
	timerProc                   uintptr
	frame, progress             float64
	started                     time.Time
	done, total, pools          int
	available                   int
	selectionKey                string
	refreshing                  bool
	rechecking, enrichment      bool
	workers                     sync.WaitGroup
	copyAction, recheckAction   *walk.Action
	animated                    bool
	animationInterval           uint32
	native                      *darkControls
}

func newDesktop(db *store.DB, dir string) *desktop {
	return &desktop{db: db, dir: dir, model: newResultModel(), animated: clientAnimationsEnabled()}
}

func (u *desktop) create() error {
	u.applyingPreset = true
	var err error
	u.theme, err = newTheme()
	if err != nil {
		return err
	}
	u.primary = &actionButton{theme: u.theme, text: "Найти прокси", onClick: u.primaryAction}
	err = (MainWindow{
		AssignTo: &u.mw, Title: fmt.Sprintf("ProxyPulse %s — поиск рабочих прокси", version), Name: "ProxyPulse",
		Font: Font{Family: "Segoe UI", PointSize: 9}, Background: SolidColorBrush{Color: navy},
		MinSize: Size{Width: 1000, Height: 680}, Size: Size{Width: 1160, Height: 780},
		Layout: VBox{Margins: Margins{Left: 18, Top: 10, Right: 18, Bottom: 12}, Spacing: 6},
		Children: []Widget{
			Composite{MinSize: Size{Height: 72}, MaxSize: Size{Height: 72}, Layout: HBox{MarginsZero: true, Spacing: 24, Alignment: AlignHNearVCenter}, Children: []Widget{
				CustomWidget{AssignTo: &u.header, MinSize: Size{Width: 450, Height: 72}, MaxSize: Size{Height: 72}, StretchFactor: 1000, Alignment: AlignHNearVCenter, PaintMode: PaintBuffered, PaintPixels: u.paintHeader, InvalidatesOnResize: true, Accessibility: Accessibility{Name: "ProxyPulse — рабочий прокси в один клик", Role: AccRoleGraphic}},
				Composite{MinSize: Size{Width: 190, Height: 72}, MaxSize: Size{Width: 190, Height: 72}, Alignment: AlignHFarVCenter, Layout: VBox{MarginsZero: true, SpacingZero: true}, Children: []Widget{VSpacer{}, u.primary, VSpacer{}}},
			}},
			CustomWidget{AssignTo: &u.cards, MinSize: Size{Height: 84}, MaxSize: Size{Height: 84}, PaintMode: PaintBuffered, PaintPixels: u.paintCards, InvalidatesOnResize: true, Accessibility: Accessibility{Name: "Сводка проверки", Role: AccRoleGrouping}},
			u.settingsPanel(),
			CustomWidget{AssignTo: &u.steps, MinSize: Size{Height: 32}, MaxSize: Size{Height: 32}, PaintMode: PaintBuffered, PaintPixels: u.paintSteps, InvalidatesOnResize: true, Accessibility: Accessibility{Name: "Источники, доступность, скорость и анонимность", Role: AccRoleProgressbar}},
			u.resultsPanel(),
			Label{AssignTo: &u.status, Text: "Готово к поиску • выберите профиль и нажмите «Найти прокси».", TextColor: muted, EllipsisMode: EllipsisEnd, MinSize: Size{Height: 24}, MaxSize: Size{Height: 24}, NoPrefix: true},
		},
	}).Create()
	if err != nil {
		u.dispose()
		return err
	}
	u.applyingPreset = false
	u.applyPreset()
	if err := u.installDarkControls(); err != nil {
		u.dispose()
		return err
	}
	if err := u.installShortcuts(); err != nil {
		u.dispose()
		return err
	}
	styleTitleBar(u.mw.Handle())
	u.timerProc = syscall.NewCallback(func(hwnd win.HWND, msg uint32, id uintptr, tick uint32) uintptr {
		if u.closing {
			return 0
		}
		switch id {
		case animationTimer:
			u.tick()
		case autoTimer:
			if !u.busy {
				u.startScan()
			}
		}
		return 0
	})
	u.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) { u.shutdown() })
	u.tv.SetRestoringCurrentItemOnReset(false)
	u.model.before = func() {
		u.refreshing = true
		u.selectionKey = ""
		if r, ok := u.model.result(u.tv.CurrentIndex()); ok {
			u.selectionKey = r.URL()
		}
	}
	u.model.after = func() {
		u.tv.SetCurrentIndex(u.model.index(u.selectionKey))
		u.refreshing = false
		u.updateResultsUI()
	}
	u.model.Sort(3, walk.SortAscending)
	u.updateResultsUI()
	u.primary.widget.SetFocus()
	return nil
}

func (u *desktop) settingsPanel() Widget {
	return Composite{
		Background: SolidColorBrush{Color: surface},
		Layout:     VBox{Margins: Margins{Left: 12, Top: 9, Right: 12, Bottom: 9}, Spacing: 7},
		Children: []Widget{
			Composite{Layout: HBox{MarginsZero: true, Spacing: 10}, Children: []Widget{
				Label{Text: "ПРОФИЛЬ", TextColor: secondary, Font: Font{Bold: true}},
				ComboBox{AssignTo: &u.preset, Model: []string{"Быстрый", "Баланс", "Глубокий", "Весь пул", "Свой"}, CurrentIndex: 1, MinSize: Size{Width: 128}, MaxSize: Size{Width: 128}, OnCurrentIndexChanged: u.applyPreset, Accessibility: Accessibility{Name: "Профиль проверки"}, ToolTipText: "Быстрый: 1 500; Баланс: 10 000; Глубокий: 50 000; Весь пул: без ограничений сбора и проверки"},
				Label{Text: "Кандидатов", TextColor: white},
				NumberEdit{AssignTo: &u.limit, Background: SolidColorBrush{Color: field}, MinSize: Size{Width: 100}, MaxSize: Size{Width: 100}, MinValue: 0, MaxValue: 200000, Value: float64(10000), Increment: 1000, Decimals: 0, SpinButtonsVisible: true, TextColor: white, OnValueChanged: u.customPreset, Accessibility: Accessibility{Name: "Лимит кандидатов; 0 — весь пул"}, ToolTipText: "0 — загрузить и проверить весь пул без выборки. Большие списки проверяются дольше."},
				HSpacer{},
				CheckBox{AssignTo: &u.speed, Text: "Замер скорости", Checked: true, OnCheckedChanged: u.customPreset, ToolTipText: "Дополнительный тест для 60 самых быстрых прокси"},
				CheckBox{AssignTo: &u.anon, Text: "Анонимность", Checked: true, OnCheckedChanged: u.customPreset, ToolTipText: "Дополнительный тест для 150 самых быстрых прокси"},
			}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 10}, Children: []Widget{
				Label{Text: "Автообновление", TextColor: secondary},
				ComboBox{AssignTo: &u.auto, Model: []string{"Выключено", "Раз в минуту"}, CurrentIndex: 0, MinSize: Size{Width: 145}, MaxSize: Size{Width: 145}, OnCurrentIndexChanged: u.configureAuto, Accessibility: Accessibility{Name: "Автообновление"}},
				Label{Text: "Новый поиск — только после завершения текущего", TextColor: secondary},
				HSpacer{},
				Label{Text: "F5 — поиск   •   Esc — стоп", TextColor: secondary},
			}},
		},
	}
}

func (u *desktop) resultsPanel() Widget {
	return Composite{
		// Walk counts fixed-size painted siblings as greedy too. Give spare
		// height to results instead of distributing it into inter-panel gaps.
		StretchFactor: 1000, Background: SolidColorBrush{Color: surface},
		Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}, Spacing: 7},
		Children: []Widget{
			Composite{Layout: HBox{MarginsZero: true, Spacing: 12}, Children: []Widget{
				Label{Text: "Рабочие прокси", Font: Font{PointSize: 12, Bold: true}, TextColor: white},
				Label{AssignTo: &u.counts, Text: "0 из 0", TextColor: secondary},
				HSpacer{},
				Label{Text: "Заголовок — сортировка  •  Двойной клик — копировать", TextColor: secondary},
			}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 8}, Children: []Widget{
				LineEdit{AssignTo: &u.search, Background: SolidColorBrush{Color: field}, CueBanner: "Поиск: адрес, страна, IP, источник…", StretchFactor: 1, MinSize: Size{Width: 235}, TextColor: white, OnTextChanged: u.filterResults, Accessibility: Accessibility{Name: "Поиск по результатам"}, ToolTipText: "Ctrl+F • можно ввести несколько слов"},
				ComboBox{AssignTo: &u.kind, Model: []string{"Все протоколы", "HTTP", "SOCKS5"}, CurrentIndex: 0, MinSize: Size{Width: 140}, MaxSize: Size{Width: 140}, OnCurrentIndexChanged: u.filterResults, Accessibility: Accessibility{Name: "Фильтр протокола"}},
				ComboBox{AssignTo: &u.quality, Model: []string{"Любое качество", "Пинг < 500 мс", "Только элитные", "С замером скорости"}, CurrentIndex: 0, MinSize: Size{Width: 178}, MaxSize: Size{Width: 178}, OnCurrentIndexChanged: u.filterResults, Accessibility: Accessibility{Name: "Фильтр качества"}},
				PushButton{Text: "Сбросить", MinSize: Size{Width: 85, Height: 28}, OnClicked: u.clearFilters},
			}},
			CustomWidget{AssignTo: &u.empty, StretchFactor: 1, MinSize: Size{Height: 120}, PaintMode: PaintBuffered, PaintPixels: u.paintEmpty, InvalidatesOnResize: true, Accessibility: Accessibility{Name: "Результатов пока нет", Role: AccRoleStatictext}},
			TableView{
				AssignTo: &u.tv, Model: u.model, CellStyler: cellStyler{u}, Visible: false, Background: SolidColorBrush{Color: field},
				MinSize: Size{Height: 120}, StretchFactor: 1, ColumnsOrderable: true, ColumnsSizable: true,
				CustomRowHeight: 29, CustomHeaderHeight: 30, LastColumnStretched: true,
				OnCurrentIndexChanged: u.selectionChanged, OnItemActivated: u.copySelected,
				OnKeyDown: func(key walk.Key) {
					if key == walk.KeyC && win.GetKeyState(win.VK_CONTROL) < 0 {
						u.copySelected()
					}
				},
				ContextMenuItems: []MenuItem{
					Action{AssignTo: &u.copyAction, Text: "Копировать выбранный", OnTriggered: u.copySelected},
					Action{AssignTo: &u.recheckAction, Text: "Перепроверить выбранный", OnTriggered: u.recheckSelected},
				},
				Columns: []TableViewColumn{
					{Title: "№", Width: 40}, {Title: "Адрес прокси", Width: 190},
					{Title: "Протокол", Width: 82}, {Title: "Пинг, мс", Width: 85, Alignment: AlignFar},
					{Title: "КБ/с", Width: 85, Alignment: AlignFar}, {Title: "Анонимность", Width: 138},
					{Title: "Выходной IP", Width: 138}, {Title: "Страна", Width: 72},
				},
			},
			Label{AssignTo: &u.selected, Text: "Выберите строку для копирования или перепроверки", TextColor: secondary, EllipsisMode: EllipsisEnd, MinSize: Size{Height: 19}, MaxSize: Size{Height: 19}, NoPrefix: true},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 8}, Children: []Widget{
				PushButton{AssignTo: &u.copy, Text: "Копировать", MinSize: Size{Width: 110, Height: 30}, OnClicked: u.copySelected, ToolTipText: "Копирует URL выбранной строки • Ctrl+C в таблице"},
				PushButton{AssignTo: &u.recheck, Text: "Перепроверить", MinSize: Size{Width: 125, Height: 30}, OnClicked: u.recheckSelected},
				HSpacer{},
				PushButton{AssignTo: &u.exportView, Text: "Экспорт видимых…", MinSize: Size{Width: 150, Height: 30}, OnClicked: func() { u.saveResults(true) }},
				PushButton{AssignTo: &u.export, Text: "Экспорт всех…", MinSize: Size{Width: 125, Height: 30}, OnClicked: func() { u.saveResults(false) }},
			}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 10}, Children: []Widget{
				Label{Text: "ЛУЧШИЙ", Font: Font{Bold: true}, TextColor: teal},
				LineEdit{AssignTo: &u.best, ReadOnly: true, Text: "Появится после проверки", Background: SolidColorBrush{Color: field}, TextColor: white, StretchFactor: 1, Accessibility: Accessibility{Name: "Самый быстрый прокси"}},
				PushButton{AssignTo: &u.copyBest, Text: "Копировать лучший", MinSize: Size{Width: 165, Height: 30}, Font: Font{Bold: true}, OnClicked: u.copyTop, ToolTipText: "Самый быстрый из всех рабочих прокси, независимо от фильтров"},
			}},
		},
	}
}

func (u *desktop) applyPreset() {
	if u.preset == nil || u.limit == nil || u.speed == nil || u.anon == nil || u.applyingPreset {
		return
	}
	i := u.preset.CurrentIndex()
	if i < 0 || i > 3 {
		return
	}
	u.applyingPreset = true
	defer func() { u.applyingPreset = false }()
	u.limit.SetValue([]float64{1500, 10000, 50000, 0}[i])
	u.speed.SetChecked(i != 0)
	u.anon.SetChecked(i != 0)
}

func (u *desktop) customPreset() {
	if u.preset != nil && u.limit != nil && u.speed != nil && u.anon != nil && !u.applyingPreset {
		u.preset.SetCurrentIndex(4)
	}
}

func (u *desktop) settings() scanSettings {
	return scanSettings{limit: int(u.limit.Value()), speed: u.speed.Checked(), anon: u.anon.Checked()}
}

func (u *desktop) filterResults() {
	if u.search == nil || u.kind == nil || u.quality == nil || u.tv == nil {
		return
	}
	kind := ""
	switch u.kind.CurrentIndex() {
	case 1:
		kind = proxy.KindHTTP
	case 2:
		kind = proxy.KindSOCKS5
	}
	u.model.Filter(u.search.Text(), kind, u.quality.CurrentIndex())
}

func (u *desktop) clearFilters() {
	u.search.SetText("")
	u.kind.SetCurrentIndex(0)
	u.quality.SetCurrentIndex(0)
	u.search.SetFocus()
}

func (u *desktop) loadCache() {
	results, err := u.db.Top(0)
	if err != nil {
		u.notify("Не удалось прочитать кэш: "+err.Error(), true)
		return
	}
	if len(results) > 0 {
		u.results, u.cached = results, true
		u.model.Set(results)
		u.notify(fmt.Sprintf("Из кэша: %d прокси • актуальность не подтверждена. Нажмите «Найти прокси» или перепроверьте строку.", len(results)), false)
		u.cards.Invalidate()
	}
}

func (u *desktop) selectionChanged() {
	if u.refreshing || u.copy == nil {
		return
	}
	r, ok := u.model.result(u.tv.CurrentIndex())
	u.copy.SetEnabled(ok)
	u.recheck.SetEnabled(ok && !u.busy)
	if u.copyAction != nil {
		u.copyAction.SetEnabled(ok)
		u.recheckAction.SetEnabled(ok && !u.busy)
	}
	if !ok {
		u.selected.SetText("Выберите строку для копирования или перепроверки")
		u.selected.SetToolTipText("")
		return
	}
	checked := "дата неизвестна"
	if !r.CheckedAt.IsZero() {
		checked = r.CheckedAt.Local().Format("02.01 15:04:05")
	}
	text := fmt.Sprintf("%s  •  источник: %s  •  проверен: %s", r.URL(), available(r.Source), checked)
	u.selected.SetText(text)
	u.selected.SetToolTipText(text)
}

func (u *desktop) updateResultsUI() {
	if u.tv == nil || u.copyBest == nil {
		return
	}
	// Swap the empty panel and table atomically. Otherwise Walk briefly lays
	// out both and permanently grows the window to fit their combined minima.
	wasSuspended := u.mw.Suspended()
	u.mw.SetSuspended(true)
	defer u.mw.SetSuspended(wasSuspended)
	n := len(u.model.items)
	u.counts.SetText(fmt.Sprintf("%d из %d", n, len(u.results)))
	u.tv.SetVisible(n > 0)
	u.empty.SetVisible(n == 0)
	if n == 0 {
		title, detail := u.emptyText()
		u.empty.Accessibility().SetName(title + ". " + detail)
		u.empty.Invalidate()
	}
	u.export.SetEnabled(len(u.results) > 0)
	u.exportView.SetEnabled(n > 0)
	u.copyBest.SetEnabled(len(u.results) > 0)
	if len(u.results) > 0 {
		u.best.SetText(u.results[0].URL())
	} else {
		u.best.SetText("Появится после проверки")
	}
	u.selectionChanged()
}

func (u *desktop) notify(text string, isError bool) {
	color := muted
	if isError {
		color = errorColor
		log.Print(text)
	}
	u.status.SetTextColor(color)
	u.status.SetText(text)
	u.status.SetToolTipText(text)
}

func (u *desktop) primaryAction() {
	if u.busy {
		u.stop()
	} else {
		u.startScan()
	}
}

func (u *desktop) begin(phase scanPhase) (context.Context, bool) {
	if u.busy || u.closing {
		return nil, false
	}
	u.animationInterval = u.timerInterval()
	if win.SetTimer(u.mw.Handle(), animationTimer, u.animationInterval, u.timerProc) == 0 {
		u.notify("Не удалось запустить таймер интерфейса. Повторите попытку.", true)
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	win.KillTimer(u.mw.Handle(), autoTimer)
	u.cancel, u.busy, u.stopping = cancel, true, false
	u.rechecking = phase == phaseCheck
	u.enrichment = u.speed.Checked() || u.anon.Checked()
	u.phase, u.frame, u.progress, u.started = phase, 0, 0, time.Now()
	u.mailbox = &scanMailbox{}
	u.primary.setText("Остановить")
	u.primary.widget.SetEnabled(true)
	for _, w := range []walk.Widget{u.limit, u.preset, u.speed, u.anon} {
		w.SetEnabled(false)
	}
	u.selectionChanged()
	u.header.Invalidate()
	u.steps.Invalidate()
	return ctx, true
}

func (u *desktop) startScan() {
	settings := u.settings()
	ctx, ok := u.begin(phaseFetch)
	if !ok {
		return
	}
	u.done, u.total, u.pools, u.available = 0, 0, 0, 0
	u.notify("Загрузка источников… Предыдущие результаты доступны до начала проверки.", false)
	u.updateResultsUI()
	u.cards.Invalidate()
	u.workers.Add(1)
	go func() {
		defer u.workers.Done()
		runScan(ctx, settings, u.db, u.mailbox)
	}()
}

func (u *desktop) stop() {
	if !u.busy || u.stopping {
		return
	}
	u.stopping = true
	u.cancel()
	u.auto.SetCurrentIndex(0)
	u.primary.setText("Останавливаем…")
	u.primary.widget.SetEnabled(false)
	u.notify("Останавливаем запросы… Найденное сохраним. Автообновление выключено.", false)
}

func (u *desktop) tick() {
	if !u.busy {
		return
	}
	if update := u.mailbox.take(); update != nil {
		u.applyUpdate(*update)
	}
	if !u.busy {
		return
	}
	if interval := u.timerInterval(); interval != u.animationInterval {
		if win.SetTimer(u.mw.Handle(), animationTimer, interval, u.timerProc) != 0 {
			u.animationInterval = interval
		}
	}
	target := float64(u.done) / float64(max(u.total, 1))
	u.progress += (target - u.progress) * 0.3
	u.frame++
	if u.animated && !win.IsIconic(u.mw.Handle()) {
		u.header.Invalidate()
		u.steps.Invalidate()
	}
}

func (u *desktop) timerInterval() uint32 {
	if !u.animated || win.IsIconic(u.mw.Handle()) {
		return 250
	}
	return 60
}

func (u *desktop) applyUpdate(update scanUpdate) {
	u.phase, u.done, u.total, u.pools = update.phase, update.done, update.total, update.pools
	u.available = update.available
	if !update.unchanged {
		if update.recheck {
			if update.removed != "" {
				u.results = slices.DeleteFunc(u.results, func(r proxy.Result) bool { return r.URL() == update.removed })
			}
			for _, r := range update.results {
				u.results = upsert(u.results, r)
			}
			app.Sort(u.results)
		} else {
			u.results = update.results
			u.cached = false
		}
		u.model.Set(u.results)
	}
	if update.finished {
		u.busy, u.stopping = false, false
		u.cancel()
		u.cancel = nil
		win.KillTimer(u.mw.Handle(), animationTimer)
		u.primary.widget.SetEnabled(true)
		u.primary.setText("Найти прокси")
		for _, w := range []walk.Widget{u.limit, u.preset, u.speed, u.anon} {
			w.SetEnabled(true)
		}
		u.notify(update.message, update.phase == phaseError || update.saveError)
		u.configureAuto()
	} else if !u.stopping {
		switch update.phase {
		case phaseCheck:
			u.notify(fmt.Sprintf("Проверка • %d / %d • рабочих: %d • прошло %s", u.done, u.total, len(u.results), time.Since(u.started).Round(time.Second)), false)
		case phaseEnrich:
			if u.rechecking {
				u.notify("Прокси отвечает • уточняем выбранные дополнительные тесты…", false)
			} else {
				u.notify("Уточняем скорость и анонимность самых быстрых… Результаты уже можно копировать.", false)
			}
		}
	}
	u.updateResultsUI()
	u.cards.Invalidate()
	u.cards.Accessibility().SetName(fmt.Sprintf("Проверено %d из %d, рабочих прокси %d", u.done, u.total, len(u.results)))
	u.steps.Invalidate()
	u.header.Invalidate()
}

func (u *desktop) configureAuto() {
	if u.mw == nil || u.auto == nil || u.timerProc == 0 || u.closing {
		return
	}
	win.KillTimer(u.mw.Handle(), autoTimer)
	if u.auto.CurrentIndex() == 1 && !u.busy {
		if win.SetTimer(u.mw.Handle(), autoTimer, 60000, u.timerProc) == 0 {
			u.auto.SetCurrentIndex(0)
			u.notify("Автообновление не запущено: ошибка таймера.", true)
		}
	}
}

func (u *desktop) shutdown() {
	u.closing = true
	if u.cancel != nil {
		u.cancel()
	}
	if u.mw != nil {
		win.KillTimer(u.mw.Handle(), animationTimer)
		win.KillTimer(u.mw.Handle(), autoTimer)
	}
}

func (u *desktop) dispose() {
	u.shutdown()
	if u.mw != nil {
		u.mw.Dispose()
	}
	if u.native != nil {
		u.native.dispose()
	}
	u.workers.Wait()
	if u.theme != nil {
		u.theme.dispose()
	}
}
