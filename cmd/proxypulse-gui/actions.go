package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/HVHBIGNAME/proxypulse/internal/store"
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

func (u *desktop) copySelected() {
	r, ok := u.model.result(u.tv.CurrentIndex())
	if !ok {
		u.notify("Выберите прокси в таблице.", false)
		return
	}
	u.copyURL(r.URL())
}

func (u *desktop) copyTop() {
	if len(u.results) > 0 {
		u.copyURL(u.results[0].URL())
	}
}

func (u *desktop) copyURL(url string) {
	if err := walk.Clipboard().SetText(url); err != nil {
		u.notify("Не удалось скопировать: "+err.Error(), true)
		return
	}
	u.notify("Скопировано: "+url+" • Ctrl+V, чтобы вставить", false)
}

func (u *desktop) recheckSelected() {
	if u.busy {
		return
	}
	r, ok := u.model.result(u.tv.CurrentIndex())
	if !ok {
		u.notify("Выберите прокси в таблице.", false)
		return
	}
	settings := u.settings()
	ctx, ok := u.begin(phaseCheck)
	if !ok {
		return
	}
	u.done, u.total, u.pools = 0, 1, 0
	u.notify("Перепроверяем "+r.URL()+" • доступность и выбранные дополнительные тесты…", false)
	u.cards.Invalidate()
	u.workers.Add(1)
	go func() {
		defer u.workers.Done()
		runRecheck(ctx, r, settings, u.db, u.mailbox)
	}()
}

func (u *desktop) saveResults(visibleOnly bool) {
	results := slices.Clone(u.results)
	if visibleOnly {
		results = slices.Clone(u.model.items)
	}
	if len(results) == 0 {
		return
	}
	dlg := walk.FileDialog{
		Title: "Экспорт рабочих прокси", Filter: "Текстовый список (*.txt)|*.txt|JSON (*.json)|*.json",
		FilterIndex: 1, InitialDirPath: u.dir, FilePath: "proxies",
		Flags: win.OFN_OVERWRITEPROMPT | win.OFN_PATHMUSTEXIST,
	}
	ok, err := dlg.ShowSave(u.mw)
	if err != nil {
		u.notify("Не удалось открыть диалог сохранения: "+err.Error(), true)
		return
	}
	if !ok {
		return
	}
	path := exportPath(dlg.FilePath, dlg.FilterIndex)
	// The native overwrite prompt runs before we supply a missing extension.
	if path != dlg.FilePath {
		if _, err := os.Stat(path); err == nil {
			if walk.MsgBox(u.mw, "Заменить файл?", path+"\n\nФайл уже существует. Заменить его?", walk.MsgBoxOKCancel|walk.MsgBoxIconQuestion) != walk.DlgCmdOK {
				return
			}
		}
	}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		err = store.ExportJSON(path, results)
	} else {
		err = store.ExportText(path, results)
	}
	if err != nil {
		u.notify("Ошибка сохранения: "+err.Error(), true)
		return
	}
	u.notify(fmt.Sprintf("Экспортировано %d прокси • %s", len(results), path), false)
}

func exportPath(path string, filterIndex int) string {
	if filepath.Ext(path) != "" {
		return path
	}
	if filterIndex == 2 {
		return path + ".json"
	}
	return path + ".txt"
}
