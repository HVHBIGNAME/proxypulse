package main

import (
	"unsafe"

	"github.com/lxn/win"
)

var getScrollBarInfo = user32.NewProc("GetScrollBarInfo")

type scrollBarInfo struct {
	Size                                    uint32
	Rect                                    win.RECT
	Button, ThumbTop, ThumbBottom, Reserved int32
	States                                  [6]uint32
}

// Use Windows' actual hit-test geometry, including arrow buttons and the thumb.
func (h *darkHook) paintScrollbars() bool {
	dc, _, _ := getWindowDC.Call(uintptr(h.hwnd))
	if dc == 0 {
		return false
	}
	defer win.ReleaseDC(h.hwnd, win.HDC(dc))
	var window win.RECT
	win.GetWindowRect(h.hwnd, &window)
	style := uint32(win.GetWindowLong(h.hwnd, win.GWL_STYLE))
	for _, bar := range []struct {
		flag     uint32
		object   int32
		vertical bool
	}{
		{win.WS_VSCROLL, -5, true}, {win.WS_HSCROLL, -6, false},
	} {
		if style&bar.flag == 0 {
			continue
		}
		info := scrollBarInfo{Size: uint32(unsafe.Sizeof(scrollBarInfo{}))}
		ok, _, _ := getScrollBarInfo.Call(uintptr(h.hwnd), uintptr(uint32(bar.object)), uintptr(unsafe.Pointer(&info)))
		if ok == 0 || info.States[0]&0x8000 != 0 {
			continue
		}
		r := info.Rect
		r.Left -= window.Left
		r.Right -= window.Left
		r.Top -= window.Top
		r.Bottom -= window.Top
		h.owner.fill(win.HDC(dc), r, scrollBg)
		thumb := r
		if bar.vertical {
			thumb.Top, thumb.Bottom = r.Top+info.ThumbTop, r.Top+info.ThumbBottom
			thumb.Left += h.unit(3)
			thumb.Right -= h.unit(3)
			h.owner.chevron(win.HDC(dc), (r.Left+r.Right)/2, r.Top+info.Button/2, h.unit(3), 1, false, muted)
			h.owner.chevron(win.HDC(dc), (r.Left+r.Right)/2, r.Bottom-info.Button/2, h.unit(3), 1, true, muted)
		} else {
			thumb.Left, thumb.Right = r.Left+info.ThumbTop, r.Left+info.ThumbBottom
			thumb.Top += h.unit(3)
			thumb.Bottom -= h.unit(3)
			for _, end := range []struct{ x, sign int32 }{{r.Left + info.Button/2, -1}, {r.Right - info.Button/2, 1}} {
				y, radius := (r.Top+r.Bottom)/2, h.unit(3)
				h.owner.stroke(win.HDC(dc), end.x-end.sign*radius/2, y-radius, end.x+end.sign*radius/2, y, 1, muted)
				h.owner.stroke(win.HDC(dc), end.x+end.sign*radius/2, y, end.x-end.sign*radius/2, y+radius, 1, muted)
			}
		}
		if info.ThumbBottom > info.ThumbTop {
			h.owner.fill(win.HDC(dc), thumb, hover)
		}
	}
	if style&(win.WS_VSCROLL|win.WS_HSCROLL) == win.WS_VSCROLL|win.WS_HSCROLL {
		client := h.clientRect()
		h.owner.fill(win.HDC(dc), win.RECT{Left: client.Right, Top: client.Bottom, Right: window.Right - window.Left, Bottom: window.Bottom - window.Top}, scrollBg)
	}
	return true
}
