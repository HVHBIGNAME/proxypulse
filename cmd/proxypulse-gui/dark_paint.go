package main

import (
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// GDI helpers. Every drawing call here works on a DC that is either passed in
// by WM_PAINT or obtained for the duration of one call and released again, so
// no DC is ever leaked between frames.

func (d *darkControls) fill(dc win.HDC, r win.RECT, color walk.Color) {
	fillRect.Call(uintptr(dc), uintptr(unsafe.Pointer(&r)), uintptr(d.brushes[color]))
}

func (d *darkControls) frame(dc win.HDC, r win.RECT, color walk.Color) {
	frameRect.Call(uintptr(dc), uintptr(unsafe.Pointer(&r)), uintptr(d.brushes[color]))
}

func nativeText(hwnd win.HWND) string {
	n := win.SendMessage(hwnd, win.WM_GETTEXTLENGTH, 0, 0)
	if n == 0 {
		return ""
	}
	text := make([]uint16, min(n, 32767)+1)
	win.SendMessage(hwnd, win.WM_GETTEXT, uintptr(len(text)), uintptr(unsafe.Pointer(&text[0])))
	return syscall.UTF16ToString(text)
}

func comboText(hwnd win.HWND, index int) string {
	if index < 0 {
		return ""
	}
	n := int(win.SendMessage(hwnd, win.CB_GETLBTEXTLEN, uintptr(index), 0))
	if n < 0 || n > 32767 {
		return ""
	}
	text := make([]uint16, n+1)
	win.SendMessage(hwnd, win.CB_GETLBTEXT, uintptr(index), uintptr(unsafe.Pointer(&text[0])))
	return syscall.UTF16ToString(text)
}

func drawNativeText(dc win.HDC, text string, r win.RECT, color walk.Color, flags uint32) {
	if text == "" {
		return
	}
	win.SetTextColor(dc, win.COLORREF(color))
	win.SetBkMode(dc, win.TRANSPARENT)
	chars := syscall.StringToUTF16(text)
	win.DrawTextEx(dc, &chars[0], int32(len(chars)-1), &r,
		flags|win.DT_SINGLELINE|win.DT_VCENTER|win.DT_END_ELLIPSIS|win.DT_NOPREFIX, nil)
}

func (h *darkHook) unit(n int32) int32 {
	dpi := win.GetDpiForWindow(h.hwnd)
	if dpi <= 0 {
		dpi = 96
	}
	return win.MulDiv(n, int32(dpi), 96)
}

// selectFont installs the window font for the duration of one paint so the
// text metrics match what the control would have used.
func (h *darkHook) selectFont(dc win.HDC) {
	source := h.hwnd
	if h.combo != 0 {
		source = h.combo
	}
	if font := win.SendMessage(source, win.WM_GETFONT, 0, 0); font != 0 {
		win.SelectObject(dc, win.HGDIOBJ(font))
	}
}

func (h *darkHook) paintBackground(dc win.HDC) {
	switch h.class {
	case "Button":
		h.paintButton(dc, h.clientRect())
	case "ComboBox":
		h.paintCombo(dc, h.clientRect())
	case "ComboLBox":
		h.paintComboList(dc, h.clientRect())
	case "msctls_updown32":
		h.paintSpinner(dc, h.clientRect())
	case "SysHeader32":
		h.paintHeader(dc, h.clientRect())
	}
}

func (h *darkHook) clientRect() win.RECT {
	var r win.RECT
	win.GetClientRect(h.hwnd, &r)
	return r
}

// WM_PRINTCLIENT supplies a DC; WM_PAINT requires a balanced paint transaction.
func (h *darkHook) paint(wp uintptr) {
	var ps win.PAINTSTRUCT
	dc := win.HDC(wp)
	if dc == 0 {
		dc = win.BeginPaint(h.hwnd, &ps)
		defer win.EndPaint(h.hwnd, &ps)
	}
	if dc == 0 {
		return
	}
	saved := win.SaveDC(dc)
	defer win.RestoreDC(dc, saved)
	h.selectFont(dc)
	h.paintBackground(dc)
}

func abs32(n int32) int32 {
	if n < 0 {
		return -n
	}
	return n
}

// stroke draws a line as a run of filled pixels. Hover and animation frames run
// often enough that allocating a pen per stroke would churn the GDI heap.
func (d *darkControls) stroke(dc win.HDC, x1, y1, x2, y2, width int32, color walk.Color) {
	if width < 1 {
		width = 1
	}
	dx, dy := abs32(x2-x1), -abs32(y2-y1)
	sx, sy := int32(1), int32(1)
	if x1 > x2 {
		sx = -1
	}
	if y1 > y2 {
		sy = -1
	}
	err := dx + dy
	for {
		d.fill(dc, win.RECT{Left: x1, Top: y1, Right: x1 + width, Bottom: y1 + width}, color)
		if x1 == x2 && y1 == y2 {
			break
		}
		e := 2 * err
		if e >= dy {
			err += dy
			x1 += sx
		}
		if e <= dx {
			err += dx
			y1 += sy
		}
	}
}

func (d *darkControls) chevron(dc win.HDC, x, y, radius, width int32, down bool, color walk.Color) {
	dy := radius / 2
	if !down {
		dy = -dy
	}
	d.stroke(dc, x-radius, y-dy, x, y+dy, width, color)
	d.stroke(dc, x, y+dy, x+radius, y-dy, width, color)
}

// triangle is the sort indicator: a solid glyph drawn from pixels, never a
// themed image that would inherit a light system colour.
func (d *darkControls) triangle(dc win.HDC, cx, cy, size int32, up bool, color walk.Color) {
	for row := int32(0); row < size; row++ {
		span := size - 1 - row
		if span < 0 {
			span = 0
		}
		top := cy - size + 1 + row
		if up {
			d.fill(dc, win.RECT{Left: cx - span, Top: top, Right: cx + span + 1, Bottom: top + 1}, color)
		} else {
			d.fill(dc, win.RECT{Left: cx - row, Top: top, Right: cx + row + 1, Bottom: top + 1}, color)
		}
	}
}

func (h *darkHook) paintButton(dc win.HDC, r win.RECT) {
	d := h.owner
	if h.kind != "push" {
		h.paintToggle(dc, r)
		return
	}
	enabled := win.IsWindowEnabled(h.hwnd)
	focused := win.GetFocus() == h.hwnd
	down := win.SendMessage(h.hwnd, win.BM_GETSTATE, 0, 0)&win.BST_PUSHED != 0
	text := white
	bg, edge := control, border
	if !enabled {
		text, bg, edge = disabledText, surface, border
	}
	if enabled {
		if h.hot {
			bg, edge = hover, secondary
		}
		if down {
			bg, edge = pressed, teal
		}
		if focused {
			edge = teal
		}
	}
	d.fill(dc, r, bg)
	d.frame(dc, r, edge)
	r.Left += h.unit(6)
	r.Right -= h.unit(6)
	drawNativeText(dc, nativeText(h.hwnd), r, text, win.DT_CENTER)
	if focused {
		d.frame(dc, inset(h.clientRect(), h.unit(3), h.unit(3)), muted)
	}
}

// inset shrinks a rectangle on all sides.
func inset(r win.RECT, dx, dy int32) win.RECT {
	r.Left += dx
	r.Top += dy
	r.Right -= dx
	r.Bottom -= dy
	return r
}

// paintToggle covers checkboxes and radio buttons, including the indicator,
// its label and the visible focus ring that replaces the native dotted rect.
func (h *darkHook) paintToggle(dc win.HDC, r win.RECT) {
	d := h.owner
	enabled := win.IsWindowEnabled(h.hwnd)
	focused := win.GetFocus() == h.hwnd
	text := white
	if !enabled {
		text = disabledText
	}
	d.fill(dc, r, surface)

	size := h.unit(13)
	box := win.RECT{Left: h.unit(1), Top: (r.Bottom - size) / 2, Right: h.unit(1) + size, Bottom: (r.Bottom + size) / 2}
	edge := border
	if enabled && h.hot {
		edge = secondary
	}
	if focused {
		edge = teal
	}
	checked := win.SendMessage(h.hwnd, win.BM_GETCHECK, 0, 0) == win.BST_CHECKED
	mark := ink
	d.fill(dc, box, field)
	d.frame(dc, box, edge)
	if checked {
		if enabled {
			d.fill(dc, box, teal)
		} else {
			d.fill(dc, box, border)
			mark = disabledText
		}
		if h.kind == "check" {
			d.stroke(dc, box.Left+h.unit(3), box.Top+h.unit(6), box.Left+h.unit(5), box.Top+h.unit(9), h.unit(2), mark)
			d.stroke(dc, box.Left+h.unit(5), box.Top+h.unit(9), box.Left+h.unit(10), box.Top+h.unit(3), h.unit(2), mark)
		} else {
			inner := win.RECT{Left: box.Left + h.unit(4), Top: box.Top + h.unit(4), Right: box.Right - h.unit(4), Bottom: box.Bottom - h.unit(4)}
			d.fill(dc, inner, teal)
		}
	}
	r.Left = box.Right + h.unit(7)
	drawNativeText(dc, nativeText(h.hwnd), r, text, win.DT_LEFT)
	if focused {
		d.frame(dc, r, muted)
	}
}

func (h *darkHook) paintCombo(dc win.HDC, r win.RECT) {
	d := h.owner
	enabled := win.IsWindowEnabled(h.hwnd)
	text, edge := white, border
	if !enabled {
		text = disabledText
	}
	if win.GetFocus() == h.hwnd {
		edge = teal
	}
	d.fill(dc, r, field)
	d.frame(dc, r, edge)

	button := comboInfo(h.hwnd).Button
	button.Top++
	button.Bottom--
	button.Right--
	face := control
	if enabled {
		if win.SendMessage(h.hwnd, win.CB_GETDROPPEDSTATE, 0, 0) != 0 {
			face = pressed
		} else if h.hot {
			face = hover
		}
	}
	d.fill(dc, button, face)
	d.frame(dc, button, border)
	d.chevron(dc, (button.Left+button.Right)/2, (button.Top+button.Bottom)/2,
		h.unit(3), max(1, h.unit(1)), true, text)

	r.Left += h.unit(8)
	r.Right = button.Left - h.unit(4)
	drawNativeText(dc, comboText(h.hwnd, int(win.SendMessage(h.hwnd, win.CB_GETCURSEL, 0, 0))), r, text, win.DT_LEFT)
}

func (h *darkHook) paintComboList(dc win.HDC, r win.RECT) {
	d := h.owner
	d.fill(dc, r, field)
	hot := h.hotItem
	selected := int(win.SendMessage(h.hwnd, win.LB_GETCURSEL, 0, 0))
	count := int(win.SendMessage(h.combo, win.CB_GETCOUNT, 0, 0))
	for i := 0; i < count; i++ {
		var item win.RECT
		if int(win.SendMessage(h.hwnd, win.LB_GETITEMRECT, uintptr(i), uintptr(unsafe.Pointer(&item)))) == -1 {
			break
		}
		if item.Bottom <= r.Top || item.Top >= r.Bottom {
			continue
		}
		switch {
		case i == selected:
			d.fill(dc, item, selection)
		case i == hot:
			d.fill(dc, item, hover)
		}
		text := white
		if i == selected {
			text = teal
		}
		item.Left += h.unit(9)
		item.Right -= h.unit(5)
		drawNativeText(dc, comboText(h.combo, i), item, text, win.DT_LEFT)
	}
	d.frame(dc, r, teal)
}

func (h *darkHook) paintSpinner(dc win.HDC, r win.RECT) {
	d := h.owner
	enabled := win.IsWindowEnabled(h.hwnd)
	text := secondary
	if !enabled {
		text = disabledText
	}
	d.fill(dc, r, control)
	middle := (r.Top + r.Bottom) / 2
	d.fill(dc, win.RECT{Left: r.Left, Top: middle, Right: r.Right, Bottom: middle + 1}, border)
	if enabled && h.hot {
		d.fill(dc, win.RECT{Left: r.Left, Top: r.Top, Right: r.Right, Bottom: middle - 1}, hover)
		d.fill(dc, win.RECT{Left: r.Left, Top: middle + 2, Right: r.Right, Bottom: r.Bottom}, hover)
	}
	up := win.RECT{Left: r.Left + 1, Top: r.Top + 1, Right: r.Right - 1, Bottom: middle - 1}
	down := win.RECT{Left: r.Left + 1, Top: middle + 2, Right: r.Right - 1, Bottom: r.Bottom - 1}
	d.chevron(dc, (up.Left+up.Right)/2, (up.Top+up.Bottom)/2, h.unit(3), max(1, h.unit(1)), false, text)
	d.chevron(dc, (down.Left+down.Right)/2, (down.Top+down.Bottom)/2, h.unit(3), max(1, h.unit(1)), true, text)
}

// paintEditFrame outlines an edit control after its native text pass, so the
// caret, selection and IME behaviour stay exactly as Windows implements them.
func (h *darkHook) paintEditFrame() {
	dc := win.GetDC(h.hwnd)
	if dc == 0 {
		return
	}
	defer win.ReleaseDC(h.hwnd, dc)
	r := h.clientRect()
	edge := border
	if win.GetFocus() == h.hwnd {
		edge = teal
	}
	if !win.IsWindowEnabled(h.hwnd) {
		edge = field
	}
	h.owner.frame(dc, r, edge)
}

// trackComboItem records the highlighted row so the open drop-down shows the
// same hover cue as the rest of the interface.
func (h *darkHook) trackComboItem() {
	if h.combo == 0 {
		return
	}
	pt := win.POINT{}
	if !win.GetCursorPos(&pt) {
		return
	}
	win.ScreenToClient(h.hwnd, &pt)
	item := int32(-1)
	count := int(win.SendMessage(h.combo, win.CB_GETCOUNT, 0, 0))
	for i := 0; i < count; i++ {
		var r win.RECT
		win.SendMessage(h.hwnd, win.LB_GETITEMRECT, uintptr(i), uintptr(unsafe.Pointer(&r)))
		if pt.X >= r.Left && pt.X < r.Right && pt.Y >= r.Top && pt.Y < r.Bottom {
			item = int32(i)
			break
		}
	}
	if item != int32(h.hotItem) {
		h.hotItem = int(item)
		win.InvalidateRect(h.hwnd, nil, false)
	}
}
