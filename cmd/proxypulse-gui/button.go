package main

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

// actionButton is the one prominent action in the header. The results toolbar
// uses native buttons; this painted control keeps Tab/Space and visible focus.
type actionButton struct {
	widget  *walk.CustomWidget
	theme   *theme
	text    string
	pressed bool
	onClick func()
}

func (b *actionButton) Create(builder *Builder) error {
	err := (CustomWidget{
		AssignTo: &b.widget, MinSize: Size{Width: 190, Height: 48}, MaxSize: Size{Width: 190, Height: 48}, Alignment: AlignHFarVCenter,
		Style: win.WS_TABSTOP, PaintMode: PaintBuffered, PaintPixels: b.paint,
		Accessibility: Accessibility{Name: b.text, Role: AccRolePushbutton, DefaultAction: "Нажать", Accelerator: "F5"},
		ToolTipText:   "F5 — начать поиск • Esc — остановить • Space — нажать",
	}).Create(builder)
	if err != nil {
		return err
	}
	// IsDialogMessage consumes Enter before CustomWidget.KeyDown. A scoped
	// shortcut arms the press; KeyUp performs it once, including key repeat.
	enter := walk.NewAction()
	if err := b.widget.ShortcutActions().Add(enter); err != nil {
		return err
	}
	if err := enter.SetShortcut(walk.Shortcut{Key: walk.KeyReturn}); err != nil {
		return err
	}
	enter.Triggered().Attach(func() {
		b.pressed = true
		b.widget.Invalidate()
	})
	b.widget.SetCursor(walk.CursorHand())
	b.widget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			b.widget.SetFocus()
			b.pressed = true
			b.widget.Invalidate()
		}
	})
	b.widget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			pressed := b.pressed
			b.pressed = false
			b.widget.Invalidate()
			r := b.widget.ClientBoundsPixels()
			if pressed && x >= 0 && y >= 0 && x < r.Width && y < r.Height {
				b.onClick()
			}
		}
	})
	b.widget.KeyDown().Attach(func(key walk.Key) {
		if key == walk.KeySpace || key == walk.KeyReturn {
			b.pressed = true
			b.widget.Invalidate()
		}
	})
	b.widget.KeyUp().Attach(func(key walk.Key) {
		if (key == walk.KeySpace || key == walk.KeyReturn) && b.pressed {
			b.pressed = false
			b.widget.Invalidate()
			b.onClick()
		}
	})
	b.widget.FocusedChanged().Attach(func() {
		b.pressed = false
		b.widget.Invalidate()
	})
	return nil
}

func (b *actionButton) setText(text string) {
	b.text = text
	b.widget.Accessibility().SetName(text)
	b.widget.Invalidate()
}

func (b *actionButton) paint(c *walk.Canvas, update walk.Rectangle) error {
	p := painter{c: c, t: b.theme, dpi: b.widget.DPI()}
	p.check(c.FillRectanglePixels(b.theme.brushes[navy], update))
	r := b.widget.ClientBounds()
	color := teal
	if b.pressed {
		color = muted
	}
	if !b.widget.Enabled() {
		color = border
	}
	p.round(color, 2, 2, r.Width-4, r.Height-4, 12)
	p.check(c.DrawTextPixels(b.text, p.font(11), ink, p.rect(8, 4, r.Width-16, r.Height-8), walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextNoPrefix))
	if b.widget.Focused() {
		p.check(c.DrawRoundedRectanglePixels(b.theme.pens[white], p.rect(0, 0, r.Width-1, r.Height-1), walk.SizeFrom96DPI(walk.Size{Width: 14, Height: 14}, p.dpi)))
	}
	return p.err
}
