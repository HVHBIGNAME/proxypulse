package main

import (
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// Walk builds its context menu from plain string items, which Windows paints
// with the light system colours. Every item is flipped to owner draw for the
// duration of one popup and flipped back when the popup closes, so no menu
// handle keeps a modified type after the window is gone.

const odtMenu = 1

func setMenuOwnerDraw(hmenu win.HMENU, on bool) {
	if hmenu == 0 {
		return
	}
	count := int(win.GetMenuItemCount(hmenu))
	for i := 0; i < count; i++ {
		mi := win.MENUITEMINFO{}
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		mi.FMask = win.MIIM_FTYPE
		if !win.GetMenuItemInfo(hmenu, uint32(i), win.TRUE, &mi) {
			continue
		}
		if mi.FType&win.MFT_SEPARATOR != 0 {
			continue
		}
		mi.FMask = win.MIIM_FTYPE
		if on {
			mi.FType |= win.MFT_OWNERDRAW
		} else {
			mi.FType &^= win.MFT_OWNERDRAW
		}
		win.SetMenuItemInfo(hmenu, uint32(i), true, &mi)
	}
}

func (d *darkControls) prepareMenu(menu win.HMENU) {
	if d.menus[menu] {
		return
	}
	info := win.MENUINFO{CbSize: uint32(unsafe.Sizeof(win.MENUINFO{})), FMask: win.MIM_BACKGROUND}
	if win.GetMenuInfo(menu, &info) {
		d.menuBackgrounds[menu] = info.HbrBack
		info.HbrBack = d.brushes[field]
		win.SetMenuInfo(menu, &info)
	}
	setMenuOwnerDraw(menu, true)
	d.menus[menu] = true
}

func (d *darkControls) restoreMenu(menu win.HMENU) {
	setMenuOwnerDraw(menu, false)
	if brush, ok := d.menuBackgrounds[menu]; ok {
		info := win.MENUINFO{CbSize: uint32(unsafe.Sizeof(win.MENUINFO{})), FMask: win.MIM_BACKGROUND, HbrBack: brush}
		win.SetMenuInfo(menu, &info)
		delete(d.menuBackgrounds, menu)
	}
	delete(d.menus, menu)
}

// menuMessage answers WM_MEASUREITEM and WM_DRAWITEM for the table's context
// menu. Anything that is not one of ours is forwarded untouched.
func (d *darkControls) menuMessage(msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case win.WM_MEASUREITEM:
		mi := lParam[win.MEASUREITEMSTRUCT](lp)
		if mi.CtlType != odtMenu {
			return 0, false
		}
		// Height only: the system keeps tracking the pointer and keyboard
		// selection exactly as it does for a normal menu item.
		mi.ItemHeight = uint32(d.scale(28))
		mi.ItemWidth = uint32(d.scale(240))
		return win.TRUE, true
	case win.WM_DRAWITEM:
		di := lParam[win.DRAWITEMSTRUCT](lp)
		if di.CtlType != odtMenu || di.HwndItem == 0 || di.HDC == 0 {
			return 0, false
		}
		d.paintMenuItem(di)
		return win.TRUE, true
	}
	return 0, false
}

func (d *darkControls) paintMenuItem(di *win.DRAWITEMSTRUCT) {
	mi := win.MENUITEMINFO{}
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	mi.FMask = win.MIIM_STRING | win.MIIM_STATE
	var buffer [256]uint16
	mi.DwTypeData = &buffer[0]
	mi.Cch = uint32(len(buffer))
	text := ""
	enabled := true
	if win.GetMenuItemInfo(win.HMENU(di.HwndItem), uint32(di.ItemID), win.FALSE, &mi) && mi.DwTypeData != nil {
		text = syscall.UTF16ToString(buffer[:])
		enabled = mi.FState&(win.MFS_DISABLED|win.MFS_GRAYED) == 0
	}
	hot := di.ItemState&win.ODS_SELECTED != 0
	saved := win.SaveDC(di.HDC)
	defer win.RestoreDC(di.HDC, saved)
	if font := win.SendMessage(d.u.search.Handle(), win.WM_GETFONT, 0, 0); font != 0 {
		win.SelectObject(di.HDC, win.HGDIOBJ(font))
	}
	if hot {
		d.fill(di.HDC, di.RcItem, selection)
	} else {
		d.fill(di.HDC, di.RcItem, field)
	}
	ink := white
	if !enabled {
		ink = disabledText
	}
	box := di.RcItem
	box.Left += d.scale(14)
	box.Right -= d.scale(8)
	drawNativeText(di.HDC, text, box, ink, win.DT_LEFT)
	if di.ItemState&win.ODS_FOCUS != 0 {
		d.frame(di.HDC, inset(di.RcItem, 2, 2), muted)
	}
	d.fill(di.HDC, win.RECT{
		Left:   di.RcItem.Left + d.scale(6),
		Top:    di.RcItem.Top + 4,
		Right:  di.RcItem.Left + d.scale(10),
		Bottom: di.RcItem.Bottom - 4,
	}, hotTeal(hot, enabled))
}

// hotTeal keeps the leading accent bar readable in both states.
func hotTeal(hot, enabled bool) walk.Color {
	switch {
	case !enabled:
		return border
	case hot:
		return teal
	}
	return control
}

func (d *darkControls) scale(n int32) int32 {
	dpi := win.GetDpiForWindow(d.root)
	if dpi <= 0 {
		dpi = 96
	}
	return win.MulDiv(n, int32(dpi), 96)
}
