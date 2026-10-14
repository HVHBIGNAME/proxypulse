package main

import (
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// Shortcuts belong to the form so they also work in the empty state and while
// editing settings. Ctrl+C stays local to the table and native text controls.
func (u *desktop) installShortcuts() error {
	for _, binding := range []struct {
		key       walk.Key
		modifiers walk.Modifiers
		run       func()
	}{
		{walk.KeyF5, 0, u.startScan},
		{walk.KeyEscape, 0, u.stop},
		{walk.KeyF, walk.ModControl, func() { u.search.SetFocus(); u.search.SetTextSelection(0, -1) }},
	} {
		action := walk.NewAction()
		if err := u.mw.ShortcutActions().Add(action); err != nil {
			return err
		}
		if err := action.SetShortcut(walk.Shortcut{Key: binding.key, Modifiers: binding.modifiers}); err != nil {
			return err
		}
		action.Triggered().Attach(binding.run)
	}
	return nil
}

func clientAnimationsEnabled() bool {
	const spiGetClientAreaAnimation = 0x1042
	enabled := int32(1)
	win.SystemParametersInfo(spiGetClientAreaAnimation, 0, unsafe.Pointer(&enabled), 0)
	return enabled != 0
}

func styleTitleBar(hwnd win.HWND) {
	var contrast win.HIGHCONTRAST
	contrast.CbSize = uint32(unsafe.Sizeof(contrast))
	if win.SystemParametersInfo(win.SPI_GETHIGHCONTRAST, contrast.CbSize, unsafe.Pointer(&contrast), 0) && contrast.DwFlags&win.HCF_HIGHCONTRASTON != 0 {
		return
	}
	proc := syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	if proc.Find() != nil {
		return
	}
	dark := int32(1)
	// Windows 10/11 support immersive dark captions; older systems retain their
	// native caption when DWM returns an unsupported-attribute HRESULT.
	proc.Call(uintptr(hwnd), 20, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
}
