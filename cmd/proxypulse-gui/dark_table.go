package main

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

type tableColumn struct {
	model int
	align walk.Alignment1D
}

func (d *darkControls) visibleColumns(frozen bool) []tableColumn {
	columns := d.u.tv.Columns()
	out := make([]tableColumn, 0, columns.Len())
	for i := 0; i < columns.Len(); i++ {
		c := columns.At(i)
		if c.Visible() && c.Frozen() == frozen {
			out = append(out, tableColumn{model: i, align: c.Alignment()})
		}
	}
	return out
}

func (h *darkHook) isFrozenList() bool {
	header := win.HWND(win.SendMessage(h.hwnd, win.LVM_GETHEADER, 0, 0))
	return int(win.SendMessage(header, win.HDM_GETITEMCOUNT, 0, 0)) == len(h.owner.visibleColumns(true))
}

func (h *darkHook) paintHeader(dc win.HDC, r win.RECT) {
	d := h.owner
	d.fill(dc, r, headerBg)
	lv := d.hooks[win.GetParent(h.hwnd)]
	if lv == nil {
		return
	}
	cols := d.visibleColumns(lv.isFrozenList())
	for i, col := range cols {
		var box win.RECT
		win.SendMessage(h.hwnd, win.HDM_GETITEMRECT, uintptr(i), uintptr(unsafe.Pointer(&box)))
		text := white
		if col.model == d.u.model.SortedColumn() {
			text = teal
			d.triangle(dc, box.Right-h.unit(9), (box.Top+box.Bottom)/2, h.unit(4), d.u.model.SortOrder() == walk.SortAscending, teal)
			box.Right -= h.unit(14)
		}
		box.Left += h.unit(7)
		box.Right -= h.unit(5)
		drawNativeText(dc, nativeHeaderText(h.hwnd, uintptr(i)), box, text, cellDrawFlags(col.align))
		if i > 0 {
			d.fill(dc, win.RECT{Left: box.Left - h.unit(7), Top: box.Top + h.unit(5), Right: box.Left - h.unit(7) + 1, Bottom: box.Bottom - h.unit(5)}, border)
		}
	}
	d.fill(dc, win.RECT{Left: r.Left, Top: r.Bottom - 1, Right: r.Right, Bottom: r.Bottom}, border)
}

func nativeHeaderText(header win.HWND, index uintptr) string {
	var buf [256]uint16
	item := win.HDITEM{Mask: win.HDI_TEXT, PszText: &buf[0], CchTextMax: int32(len(buf))}
	if win.SendMessage(header, win.HDM_GETITEM, index, uintptr(unsafe.Pointer(&item))) == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:])
}

func (d *darkControls) tableFocused() bool {
	table := d.u.tv.Handle()
	focus := win.GetFocus()
	return focus == table || win.IsChild(table, focus)
}

func (h *darkHook) customDraw(lp uintptr) uintptr {
	cd := lParam[win.NMLVCUSTOMDRAW](lp)
	switch cd.Nmcd.DwDrawStage {
	case win.CDDS_PREPAINT:
		h.cols = h.owner.visibleColumns(h.isFrozenList())
		return win.CDRF_NOTIFYITEMDRAW
	case win.CDDS_ITEMPREPAINT:
		h.owner.fill(cd.Nmcd.Hdc, cd.Nmcd.Rc, h.rowBackground(int(cd.Nmcd.DwItemSpec)))
		return win.CDRF_NOTIFYSUBITEMDRAW
	case win.CDDS_SUBITEM | win.CDDS_ITEMPREPAINT:
		h.paintCell(cd)
		return win.CDRF_SKIPDEFAULT
	}
	return win.CDRF_DODEFAULT
}

func (h *darkHook) rowBackground(row int) walk.Color {
	if h.owner.u.tv.CurrentIndex() == row {
		if h.owner.tableFocused() {
			return selection
		}
		return fadedRow
	}
	if row%2 != 0 {
		return rowAlt
	}
	return field
}

func (h *darkHook) paintCell(cd *win.NMLVCUSTOMDRAW) {
	sub := int(cd.ISubItem)
	if sub < 0 || sub >= len(h.cols) || cd.Nmcd.Hdc == 0 {
		return
	}
	d := h.owner
	col, row := h.cols[sub], int(cd.Nmcd.DwItemSpec)
	box := win.RECT{Top: int32(sub), Left: win.LVIR_BOUNDS}
	win.SendMessage(h.hwnd, win.LVM_GETSUBITEMRECT, uintptr(row), uintptr(unsafe.Pointer(&box)))
	// Header rectangles follow the real visual order, including horizontal scroll.
	header := win.HWND(win.SendMessage(h.hwnd, win.LVM_GETHEADER, 0, 0))
	var bounds win.RECT
	win.SendMessage(header, win.HDM_GETITEMRECT, uintptr(sub), uintptr(unsafe.Pointer(&bounds)))
	left := win.POINT{X: bounds.Left}
	win.ClientToScreen(header, &left)
	win.ScreenToClient(h.hwnd, &left)
	box.Left, box.Right = left.X, left.X+bounds.Right-bounds.Left
	d.fill(cd.Nmcd.Hdc, box, h.rowBackground(row))
	color := white
	if result, ok := d.u.model.result(row); ok {
		color = resultTextColor(result, col.model)
	}
	if d.u.tv.CurrentIndex() == row {
		color = white
	}
	box.Left += h.unit(6)
	box.Right -= h.unit(6)
	drawNativeText(cd.Nmcd.Hdc, fmt.Sprint(d.u.model.Value(row, col.model)), box, color, cellDrawFlags(col.align))
}

func cellDrawFlags(align walk.Alignment1D) uint32 {
	switch align {
	case walk.AlignFar:
		return win.DT_RIGHT
	case walk.AlignCenter:
		return win.DT_CENTER
	}
	return win.DT_LEFT
}
