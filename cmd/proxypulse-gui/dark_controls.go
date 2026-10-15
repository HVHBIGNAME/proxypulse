package main

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// Per-window painting preserves native input and avoids changing system colors.

const (
	// Values github.com/lxn/win does not declare but this layer needs.
	bsTypeMask     = 0x0000000f
	darkSubclassID = 0x505044
	tableViewClass = `\o/ Walk_TableView_Class \o/`
)

var (
	comctl32       = syscall.NewLazyDLL("comctl32.dll")
	setSubclass    = comctl32.NewProc("SetWindowSubclass")
	removeSubclass = comctl32.NewProc("RemoveWindowSubclass")
	defSubclass    = comctl32.NewProc("DefSubclassProc")
	user32         = syscall.NewLazyDLL("user32.dll")
	getComboInfo   = user32.NewProc("GetComboBoxInfo")
	fillRect       = user32.NewProc("FillRect")
	frameRect      = user32.NewProc("FrameRect")
	getWindowDC    = user32.NewProc("GetWindowDC")

	// Assigned in init because a package-level initialiser for
	// darkSubclassProc would form an initialization cycle with darkWindowProc.
	darkSubclassProc uintptr

	hookMu    sync.Mutex
	hookTable = make(map[win.HWND]*darkHook)
)

func init() {
	darkSubclassProc = syscall.NewCallback(darkWindowProc)
}

type comboBoxInfo struct {
	Size              uint32
	Item, Button      win.RECT
	ButtonState       uint32
	Combo, Edit, List win.HWND
}

// darkControls owns every hook and GDI resource created for the window tree.
type darkControls struct {
	u               *desktop
	root            win.HWND
	hooks           map[win.HWND]*darkHook
	brushes         map[walk.Color]win.HBRUSH
	menus           map[win.HMENU]bool
	menuBackgrounds map[win.HMENU]win.HBRUSH
}

type darkHook struct {
	owner   *darkControls
	hwnd    win.HWND
	class   string
	kind    string
	combo   win.HWND // owning combo for a drop-down list window
	hot     bool
	hotItem int
	cols    []tableColumn
}

func nativeClass(hwnd win.HWND) string {
	var name [256]uint16
	n, _ := win.GetClassName(hwnd, &name[0], len(name))
	return syscall.UTF16ToString(name[:n])
}

func comboInfo(hwnd win.HWND) comboBoxInfo {
	info := comboBoxInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	getComboInfo.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&info)))
	return info
}

func (u *desktop) installDarkControls() error {
	d := &darkControls{
		u:               u,
		root:            u.mw.Handle(),
		hooks:           make(map[win.HWND]*darkHook),
		brushes:         make(map[walk.Color]win.HBRUSH),
		menus:           make(map[win.HMENU]bool),
		menuBackgrounds: make(map[win.HMENU]win.HBRUSH),
	}
	u.native = d
	for color := range u.theme.brushes {
		brush := win.CreateBrushIndirect(&win.LOGBRUSH{LbStyle: win.BS_SOLID, LbColor: win.COLORREF(color)})
		if brush == 0 {
			d.dispose()
			return fmt.Errorf("создание кисти тёмной темы")
		}
		d.brushes[color] = brush
	}
	if err := d.attachTree(d.root); err != nil {
		return err
	}
	// Walk's stock focus and hover effects paint light halos around control
	// HWNDs from the parent, outside the range of any control hook.
	var clear func(walk.Container)
	clear = func(parent walk.Container) {
		for i := 0; i < parent.Children().Len(); i++ {
			child := parent.Children().At(i)
			child.GraphicsEffects().Clear()
			if nested, ok := child.(walk.Container); ok {
				clear(nested)
			}
		}
	}
	clear(u.mw)
	return nil
}

func (d *darkControls) attachTree(hwnd win.HWND) error {
	if err := d.attach(hwnd, 0); err != nil {
		return err
	}
	for child := win.GetWindow(hwnd, win.GW_CHILD); child != 0; child = win.GetWindow(child, win.GW_HWNDNEXT) {
		if err := d.attachTree(child); err != nil {
			return err
		}
	}
	return nil
}

// buttonKind reports the semantic role of a "Button" window so painting can
// pick the matching shape without re-reading window styles on every frame.
func buttonKind(hwnd win.HWND) string {
	switch win.GetWindowLong(hwnd, win.GWL_STYLE) & bsTypeMask {
	case win.BS_CHECKBOX, win.BS_AUTOCHECKBOX:
		return "check"
	case win.BS_RADIOBUTTON, win.BS_AUTORADIOBUTTON:
		return "radio"
	case win.BS_OWNERDRAW:
		return "owner"
	}
	return "push"
}

func (d *darkControls) attach(hwnd, combo win.HWND) error {
	if _, exists := d.hooks[hwnd]; exists {
		return nil
	}
	h := &darkHook{owner: d, hwnd: hwnd, class: nativeClass(hwnd), combo: combo, hotItem: -1}
	h.kind = buttonKind(hwnd)
	d.register(h)
	if ok, _, _ := setSubclass.Call(uintptr(hwnd), darkSubclassProc, darkSubclassID, 0); ok == 0 {
		h.detach()
		return fmt.Errorf("подключение тёмной темы: %s", h.class)
	}
	d.prepare(h)
	if h.class == "ComboBox" {
		// The drop-down list is a separate window that Windows creates for
		// the combo, so it needs its own hook even while hidden.
		if info := comboInfo(hwnd); info.List != 0 {
			if err := d.attach(info.List, hwnd); err != nil {
				return err
			}
		}
	}
	return nil
}

// prepare applies the per-class window styles, themes and colours that the
// painting code then relies on.
func (d *darkControls) prepare(h *darkHook) {
	switch h.class {
	case "Button", "ComboBox", "ComboLBox", "Edit", "msctls_updown32", "SysHeader32", "SysListView32":
		walk.SetWindowFont(h.hwnd, d.u.theme.fonts[9])
		// Visual styles repaint these controls light; the subclass owns them.
		empty := uint16(0)
		win.SetWindowTheme(h.hwnd, &empty, &empty)
	}
	switch h.class {
	case "Edit", "SysListView32", tableViewClass:
		// Native 3D edges use system colours, so drop them and draw our own.
		ex := win.GetWindowLong(h.hwnd, win.GWL_EXSTYLE)
		win.SetWindowLong(h.hwnd, win.GWL_EXSTYLE, ex&^(win.WS_EX_CLIENTEDGE|win.WS_EX_STATICEDGE))
		style := win.GetWindowLong(h.hwnd, win.GWL_STYLE)
		win.SetWindowLong(h.hwnd, win.GWL_STYLE, style&^win.WS_BORDER)
		win.SetWindowPos(h.hwnd, 0, 0, 0, 0, 0,
			win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
	}
	if h.class == "SysListView32" {
		win.SendMessage(h.hwnd, win.LVM_SETBKCOLOR, 0, uintptr(field))
		win.SendMessage(h.hwnd, win.LVM_SETTEXTBKCOLOR, 0, uintptr(field))
		win.SendMessage(h.hwnd, win.LVM_SETTEXTCOLOR, 0, uintptr(white))
	}
}

func (d *darkControls) register(h *darkHook) {
	hookMu.Lock()
	hookTable[h.hwnd] = h
	hookMu.Unlock()
	d.hooks[h.hwnd] = h
}

func (h *darkHook) detach() {
	removeSubclass.Call(uintptr(h.hwnd), darkSubclassProc, darkSubclassID)
	delete(h.owner.hooks, h.hwnd)
	hookMu.Lock()
	delete(hookTable, h.hwnd)
	hookMu.Unlock()
}

func (d *darkControls) dispose() {
	for hwnd := range d.hooks {
		if h, ok := d.hooks[hwnd]; ok {
			h.detach()
		}
	}
	for hmenu := range d.menus {
		d.restoreMenu(hmenu)
	}
	d.menus = make(map[win.HMENU]bool)
	for color, brush := range d.brushes {
		win.DeleteObject(win.HGDIOBJ(brush))
		delete(d.brushes, color)
	}
}

func defaultDarkProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := defSubclass.Call(uintptr(hwnd), uintptr(msg), wp, lp)
	return r
}

// Windows owns this pointer for the duration of the synchronous callback.
func lParam[T any](lp uintptr) *T {
	return *(**T)(unsafe.Pointer(&lp))
}

func darkWindowProc(hwnd win.HWND, msg uint32, wp, lp, id, ref uintptr) uintptr {
	hookMu.Lock()
	h := hookTable[hwnd]
	hookMu.Unlock()
	if h == nil {
		return defaultDarkProc(hwnd, msg, wp, lp)
	}
	// Walk restores the original WNDPROC while handling WM_DESTROY, so detach
	// before forwarding it instead of waiting for a WM_NCDESTROY that the
	// restored procedure will never deliver to us.
	if msg == win.WM_DESTROY || msg == win.WM_NCDESTROY {
		h.detach()
		return defaultDarkProc(hwnd, msg, wp, lp)
	}
	return h.dispatch(msg, wp, lp)
}

func (h *darkHook) dispatch(msg uint32, wp, lp uintptr) uintptr {
	d, hwnd := h.owner, h.hwnd
	if h.class == "SysListView32" && (msg == win.LVM_SETBKCOLOR || msg == win.LVM_SETTEXTBKCOLOR) {
		lp = uintptr(field)
	}
	if hwnd == d.root {
		if r, handled := d.formMessage(msg, wp, lp); handled {
			return r
		}
	}
	switch msg {
	case win.WM_CTLCOLOREDIT, win.WM_CTLCOLORSTATIC, win.WM_CTLCOLORLISTBOX:
		if class := nativeClass(win.HWND(lp)); class == "Edit" || class == "ComboLBox" {
			return d.controlColor(win.HDC(wp), win.HWND(lp))
		}
	case win.WM_NOTIFY:
		if r, handled := h.notify(lp); handled {
			return r
		}
	case win.WM_ERASEBKGND:
		if h.paintsClient() {
			d.fill(win.HDC(wp), h.clientRect(), surface)
			return 1
		}
		if h.class == "SysListView32" {
			// Cover everything below the last row so a short or empty result
			// set never shows the system window colour.
			h.owner.fill(win.HDC(wp), h.clientRect(), field)
			return 1
		}
	case win.WM_PAINT, win.WM_PRINTCLIENT:
		if h.paintsClient() {
			h.paint(wp)
			return 0
		}
	case win.WM_NCPAINT:
		if h.paintsScrollbars() && h.paintScrollbars() {
			return 0
		}
	case win.WM_MOUSEMOVE:
		if h.class == "ComboLBox" {
			h.trackComboItem()
		} else if h.paintsClient() {
			h.trackHover()
		}
	case win.WM_MOUSELEAVE:
		if h.hot || h.hotItem != -1 {
			h.hot, h.hotItem = false, -1
			win.InvalidateRect(hwnd, nil, false)
		}
	}
	r := defaultDarkProc(hwnd, msg, wp, lp)
	switch msg {
	case win.WM_PAINT, win.WM_PRINTCLIENT:
		if h.class == "Edit" {
			h.paintEditFrame()
		}
		if h.paintsScrollbars() {
			h.paintScrollbars()
		}
	case win.WM_NCMOUSEMOVE, win.WM_NCLBUTTONDOWN, win.WM_NCLBUTTONUP, win.WM_MOUSEWHEEL:
		if h.paintsScrollbars() {
			h.paintScrollbars()
		}
	case win.WM_ENABLE, win.WM_SETFOCUS, win.WM_KILLFOCUS, win.WM_SHOWWINDOW,
		win.BM_SETCHECK, win.BM_SETSTATE, win.CB_SETCURSEL, win.LB_SETCURSEL,
		win.WM_LBUTTONDOWN, win.WM_LBUTTONUP, win.WM_KEYDOWN, win.WM_KEYUP,
		win.WM_CAPTURECHANGED, win.WM_VSCROLL, win.WM_HSCROLL, win.WM_MOUSEMOVE:
		h.refresh()
	}
	return r
}

// paintsScrollbars reports whether this window draws its own scroll bars. A
// scroll bar lives in the non-client area, so it is only reachable by taking
// WM_NCPAINT over as well.
func (h *darkHook) paintsScrollbars() bool {
	if h.class != "SysListView32" {
		return false
	}
	style := uint32(win.GetWindowLong(h.hwnd, win.GWL_STYLE))
	return style&(win.WS_VSCROLL|win.WS_HSCROLL) != 0
}

// paintsClient reports whether this window's entire client area is drawn by the
// subclass, which lets WM_ERASEBKGND and WM_PAINT be answered locally.
func (h *darkHook) paintsClient() bool {
	switch h.class {
	case "Button", "ComboBox", "ComboLBox", "msctls_updown32", "SysHeader32":
		return true
	}
	return false
}

func (h *darkHook) refresh() {
	switch {
	case h.paintsClient(), h.class == "Edit", h.class == "SysListView32":
		win.InvalidateRect(h.hwnd, nil, false)
	}
}

func (h *darkHook) trackHover() {
	if h.hot {
		return
	}
	h.hot = true
	tme := win.TRACKMOUSEEVENT{
		CbSize:    uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})),
		DwFlags:   win.TME_LEAVE,
		HwndTrack: h.hwnd,
	}
	win.TrackMouseEvent(&tme)
	win.InvalidateRect(h.hwnd, nil, false)
}

// formMessage handles the notifications the form window receives on behalf of
// its children: control colours, the table's notify messages and the table's
// owner-drawn context menu.
func (d *darkControls) formMessage(msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case win.WM_MEASUREITEM, win.WM_DRAWITEM:
		return d.menuMessage(msg, wp, lp)
	case win.WM_INITMENUPOPUP:
		if lp>>16 == 0 {
			r := defaultDarkProc(d.root, msg, wp, lp)
			d.prepareMenu(win.HMENU(wp))
			return r, true
		}
	case win.WM_UNINITMENUPOPUP:
		if hmenu := win.HMENU(wp); d.menus[hmenu] {
			d.restoreMenu(hmenu)
		}
	}
	return 0, false
}

func (d *darkControls) controlColor(dc win.HDC, target win.HWND) uintptr {
	win.SetTextColor(dc, win.COLORREF(white))
	win.SetBkColor(dc, win.COLORREF(field))
	if !win.IsWindowEnabled(target) {
		win.SetTextColor(dc, win.COLORREF(disabledText))
	}
	return uintptr(d.brushes[field])
}

// notify answers the notifications a hooked window receives. Header custom
// draw reaches us through the list view that owns the header, and the table's
// own notify codes arrive at the table window itself.
func (h *darkHook) notify(lp uintptr) (uintptr, bool) {
	if lp == 0 {
		return 0, false
	}
	nm := lParam[win.NMHDR](lp)
	switch {
	case nm.Code == win.NM_CUSTOMDRAW && nativeClass(nm.HwndFrom) == "SysHeader32":
		// The header paints itself entirely; suppress Walk's header custom
		// draw so no themed default pass can run underneath it.
		return win.CDRF_SKIPDEFAULT, true
	case h.class == tableViewClass && nativeClass(nm.HwndFrom) == "SysListView32":
		if nm.Code == win.NM_CUSTOMDRAW {
			if sender := h.owner.hooks[nm.HwndFrom]; sender != nil {
				return sender.customDraw(lp), true
			}
		}
	}
	return 0, false
}
