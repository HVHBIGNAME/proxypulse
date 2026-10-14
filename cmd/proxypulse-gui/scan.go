package main

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/app"
	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	"github.com/HVHBIGNAME/proxypulse/internal/store"
)

type scanPhase int

const (
	phaseReady scanPhase = iota
	phaseFetch
	phaseCheck
	phaseEnrich
	phaseDone
	phaseEmpty
	phaseError
	phaseStopped
)

type scanSettings struct {
	limit int
	speed bool
	anon  bool
}

type scanUpdate struct {
	phase     scanPhase
	done      int
	total     int
	pools     int
	results   []proxy.Result
	message   string
	finished  bool
	recheck   bool
	removed   string
	unchanged bool
	saveError bool
}

// A one-slot mailbox coalesces progress instead of posting a Windows message
// for every network result. Workers never touch controls or desktop state.
type scanMailbox struct {
	mu     sync.Mutex
	latest *scanUpdate
}

func (m *scanMailbox) put(u scanUpdate) {
	m.mu.Lock()
	m.latest = &u
	m.mu.Unlock()
}

func (m *scanMailbox) take() *scanUpdate {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.latest
	m.latest = nil
	return u
}

func runScan(ctx context.Context, settings scanSettings, db *store.DB, updates *scanMailbox) {
	candidates, fetches, err := app.Fetch(ctx, nil, 25*time.Second)
	if ctx.Err() != nil {
		updates.put(scanUpdate{phase: phaseStopped, finished: true, unchanged: true, message: "Поиск остановлен. Можно начать снова."})
		return
	}
	if err != nil {
		updates.put(scanUpdate{phase: phaseError, finished: true, unchanged: true, message: "Не удалось загрузить источники: " + err.Error()})
		return
	}
	pools := 0
	for _, f := range fetches {
		if f.Err == nil {
			pools++
		}
	}
	if pools == 0 || len(candidates) == 0 {
		updates.put(scanUpdate{phase: phaseError, finished: true, unchanged: true, message: "Источники не вернули кандидатов. Проверьте подключение и повторите поиск."})
		return
	}
	if len(candidates) > settings.limit {
		candidates = candidates[:settings.limit]
	}
	total := len(candidates)
	updates.put(scanUpdate{phase: phaseCheck, total: total, pools: pools})
	opt := app.Options{Workers: 400, Timeout: 4 * time.Second, SpeedTest: settings.speed, SpeedTop: 60, Anonymity: settings.anon, AnonTop: 150}
	var mu sync.Mutex
	var done int
	var live []proxy.Result
	var checked []proxy.Result
	last := time.Now()
	validated := app.Validate(ctx, candidates, opt, func(r proxy.Result) {
		mu.Lock()
		defer mu.Unlock()
		done++
		// Cancellation is not evidence of a dead proxy. Persist genuine failures
		// as well as survivors so a failed cached endpoint cannot reappear.
		if r.Alive || ctx.Err() == nil {
			checked = append(checked, r)
		}
		if r.Alive {
			live = append(live, r)
		}
		if time.Since(last) >= 200*time.Millisecond {
			snapshot := slices.Clone(live)
			app.Sort(snapshot)
			updates.put(scanUpdate{phase: phaseCheck, done: done, total: total, pools: pools, results: snapshot})
			last = time.Now()
		}
	})
	app.Sort(validated)
	if ctx.Err() == nil && len(validated) > 0 && (settings.anon || settings.speed) {
		updates.put(scanUpdate{phase: phaseEnrich, done: done, total: total, pools: pools, results: slices.Clone(validated)})
		if settings.anon {
			opt.DirectIP = proxy.RealIP(ctx, proxy.DefaultIPURL, 6*time.Second)
		}
		validated = app.Finalise(ctx, validated, opt)
	}
	u := scanUpdate{phase: phaseDone, done: done, total: total, pools: pools, results: validated, finished: true}
	switch {
	case ctx.Err() != nil:
		u.phase = phaseStopped
		u.message = fmt.Sprintf("Остановлено • проверено %d из %d • найдено %d. Частичные результаты сохранены.", done, total, len(validated))
	case len(validated) == 0:
		u.phase = phaseEmpty
		u.message = "Рабочих прокси не найдено. Увеличьте лимит или повторите поиск позже."
	default:
		u.message = fmt.Sprintf("Готово • %d рабочих из %d • выберите строку или скопируйте лучший.", len(validated), total)
		if settings.anon && opt.DirectIP == "" {
			u.message += " Анонимность не определена: внешний IP недоступен."
		}
	}
	// Enriched survivors override their initial reachability-only records.
	if err := db.Save(append(checked, validated...)); err != nil {
		u.saveError = true
		u.message += " Не удалось записать кэш: " + err.Error()
	}
	updates.put(u)
}

func runRecheck(ctx context.Context, r proxy.Result, settings scanSettings, db *store.DB, updates *scanMailbox) {
	opt := app.Options{Workers: 1, Timeout: 6 * time.Second, SpeedTest: settings.speed, SpeedTop: 1, Anonymity: settings.anon, AnonTop: 1}
	candidate := proxy.Candidate{Addr: r.Addr, Kind: r.Kind, Source: r.Source, Country: r.Country}
	results := app.Validate(ctx, []proxy.Candidate{candidate}, opt, nil)
	if len(results) > 0 && ctx.Err() == nil && (settings.anon || settings.speed) {
		updates.put(scanUpdate{phase: phaseEnrich, done: 1, total: 1, recheck: true, unchanged: true})
		if settings.anon {
			opt.DirectIP = proxy.RealIP(ctx, proxy.DefaultIPURL, 6*time.Second)
		}
		results = app.Finalise(ctx, results, opt)
	}
	u := scanUpdate{phase: phaseDone, finished: true, recheck: true, total: 1, done: 1, results: results}
	if ctx.Err() != nil {
		u.phase, u.unchanged, u.message = phaseStopped, true, "Перепроверка остановлена. Предыдущий результат сохранён."
		updates.put(u)
		return
	}
	toSave := results
	if len(results) == 0 {
		r.Alive, r.CheckedAt = false, time.Now()
		r.Err = "recheck failed"
		toSave = []proxy.Result{r}
		u.phase, u.removed = phaseEmpty, r.URL()
		u.message = "Прокси больше не отвечает и удалён из рабочих: " + r.URL()
	} else {
		u.message = fmt.Sprintf("Перепроверен: %s • %d мс", r.URL(), results[0].LatencyMs)
		if settings.anon && opt.DirectIP == "" {
			u.message += " • анонимность не определена"
		}
	}
	if err := db.Save(toSave); err != nil {
		u.saveError = true
		u.message += " • ошибка записи кэша: " + err.Error()
	}
	updates.put(u)
}
