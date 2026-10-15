package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/HVHBIGNAME/proxypulse/internal/store"
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// Run explicitly on Windows with a Common Controls v6 manifest embedded in the
// test executable. It exercises real controls using offline scan updates.
func TestNativeDesktop(t *testing.T) {
	if os.Getenv("PROXYPULSE_GUI_SMOKE") != "1" {
		t.Skip("set PROXYPULSE_GUI_SMOKE=1 and embed the Common Controls manifest")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	u := newDesktop(db, dir)
	hookMu.Lock()
	initialHooks := len(hookTable)
	hookMu.Unlock()
	if err := u.create(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		u.dispose()
		hookMu.Lock()
		defer hookMu.Unlock()
		if len(hookTable) != initialHooks {
			t.Errorf("native color hooks leaked after window disposal: %d", len(hookTable)-initialHooks)
		}
	}()
	if u.preset.CurrentIndex() != 1 || u.limit.Value() != 1500 || !u.speed.Checked() || !u.anon.Checked() {
		t.Error("default profile did not initialize consistently")
	}
	if u.copy.Enabled() || u.recheck.Enabled() || u.export.Enabled() || u.copyBest.Enabled() || !u.empty.Visible() {
		t.Error("empty state offers unavailable actions")
	}
	if u.mw.ShortcutActions().Len() != 3 {
		t.Error("form-wide shortcuts are missing")
	}
	if u.model.SortedColumn() != 3 {
		t.Error("default sort indicator is not latency")
	}
	u.mw.SetSize(walk.Size{Width: 1000, Height: 680})
	u.preset.SetCurrentIndex(0)
	if u.limit.Value() != 500 || u.speed.Checked() || u.anon.Checked() {
		t.Error("quick preset did not apply")
	}
	u.limit.SetValue(123)
	if u.preset.CurrentIndex() != 3 {
		t.Error("manual limit did not select custom profile")
	}
	ctx, ok := u.begin(phaseCheck)
	if !ok {
		t.Fatal("could not start UI progress timer")
	}
	if u.limit.Enabled() || u.recheck.Enabled() || u.primary.text != "Остановить" {
		t.Error("busy controls are inconsistent")
	}
	if _, ok := u.begin(phaseFetch); ok {
		t.Error("a second operation was allowed while busy")
	}
	u.stop()
	if ctx.Err() == nil || !u.stopping || u.primary.widget.Enabled() {
		t.Error("stop did not cancel and disable repeated cancellation")
	}
	u.applyUpdate(scanUpdate{phase: phaseStopped, unchanged: true, finished: true, message: "Остановлено"})
	if u.busy || u.stopping || !u.limit.Enabled() || u.cancel != nil {
		t.Error("completion did not restore idle state")
	}
	if _, ok := u.begin(phaseFetch); !ok {
		t.Fatal("restart failed")
	}
	rs := sampleResults()
	u.applyUpdate(scanUpdate{phase: phaseDone, finished: true, results: rs, total: 10, done: 10, message: "Готово"})
	if size := u.mw.Size(); size.Height > 700 {
		t.Errorf("swapping empty state for results inflated the window: %v", size)
	}
	u.tv.SetCurrentIndex(1)
	key := rs[1].URL()
	u.model.Sort(3, walk.SortDescending)
	if r, ok := u.model.result(u.tv.CurrentIndex()); !ok || r.URL() != key {
		t.Error("sorting changed the selected action target")
	}
	u.search.SetText("beta")
	if u.model.RowCount() != 1 || !u.copy.Enabled() || u.best.Text() != rs[0].URL() {
		t.Error("filtering broke selection or global best")
	}
	u.search.SetText("no-such-endpoint")
	if !u.empty.Visible() || u.tv.Visible() || u.copy.Enabled() || u.exportView.Enabled() || !u.export.Enabled() {
		t.Error("no-match state and export scopes disagree")
	}
	u.clearFilters()
	if u.model.RowCount() != len(rs) || u.tv.CurrentIndex() != -1 {
		t.Error("reset selected a different proxy silently")
	}
	if _, ok := u.begin(phaseFetch); !ok {
		t.Fatal("error-state setup failed")
	}
	u.applyUpdate(scanUpdate{phase: phaseError, finished: true, unchanged: true, message: "Тестовая ошибка"})
	if u.model.RowCount() != len(rs) || u.busy || u.status.TextColor() != errorColor {
		t.Error("error did not preserve results and expose retry")
	}
	// Exercise actual dialog-message routing, not just the event publishers.
	// Replace the primary handler so synthetic keys cannot start a network scan.
	clicks := 0
	u.primary.onClick = func() { clicks++ }
	onUI := func(fn func()) {
		done := make(chan struct{})
		u.mw.Synchronize(func() { defer close(done); fn() })
		<-done
	}
	u.mw.Starting().Attach(func() {
		go func() {
			onUI(func() {
				for _, widget := range []walk.Widget{u.preset, u.quality, u.speed, u.copy} {
					win.InvalidateRect(widget.Handle(), nil, true)
					win.UpdateWindow(widget.Handle())
					invalid, _, _ := user32.NewProc("GetUpdateRect").Call(uintptr(widget.Handle()), 0, 0)
					if invalid != 0 {
						t.Errorf("paint did not validate %T; it would repaint continuously", widget)
					}
				}
				info := comboInfo(u.preset.Handle())
				if info.List == 0 || u.native.hooks[info.List] == nil {
					t.Error("native combo popup is missing its dark renderer")
				}
			})
			for i, key := range []walk.Key{walk.KeyReturn, walk.KeySpace} {
				onUI(func() {
					u.primary.widget.SetFocus()
					win.PostMessage(u.primary.widget.Handle(), win.WM_KEYDOWN, uintptr(key), 0)
					win.PostMessage(u.primary.widget.Handle(), win.WM_KEYUP, uintptr(key), 0)
				})
				time.Sleep(100 * time.Millisecond)
				onUI(func() {
					if clicks != i+1 {
						t.Errorf("primary key %v delivered %d clicks, want %d", key, clicks, i+1)
					}
				})
			}
			onUI(func() { u.mw.Close() })
		}()
	})
	u.mw.Run()
}
