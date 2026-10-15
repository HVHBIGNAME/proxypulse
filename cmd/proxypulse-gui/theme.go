package main

import (
	"fmt"
	"math"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
	"github.com/lxn/walk"
)

// Dark surfaces share one palette; ink is reserved for text on the teal action.
var (
	navy         = walk.RGB(12, 21, 36) // window backdrop, behind every panel
	surface      = walk.RGB(19, 30, 45) // settings and results panels
	panel        = walk.RGB(25, 40, 60) // cards, header strip, menu bar
	field        = walk.RGB(14, 23, 36) // text inputs and the table canvas
	rowAlt       = walk.RGB(22, 34, 50) // alternating table rows
	control      = walk.RGB(31, 47, 65) // button, spin button and combo arrow faces
	hover        = walk.RGB(41, 61, 81) // pointer over an enabled control
	pressed      = walk.RGB(24, 37, 52) // control held down
	border       = walk.RGB(42, 61, 83) // resting outlines and column dividers
	headerBg     = walk.RGB(26, 41, 62) // table header strip
	scrollBg     = walk.RGB(20, 32, 47) // scrollbar tracks
	selection    = walk.RGB(26, 84, 83) // selected row or combo item while focused
	fadedRow     = walk.RGB(44, 62, 82) // selected row while the table is unfocused
	teal         = walk.RGB(56, 214, 190)
	tealDark     = walk.RGB(17, 94, 87)
	white        = walk.RGB(224, 232, 241)
	secondary    = walk.RGB(163, 181, 202)
	muted        = walk.RGB(142, 161, 184)
	disabledText = walk.RGB(108, 126, 147)
	ink          = walk.RGB(10, 31, 35)
	warning      = walk.RGB(240, 186, 104)
	errorColor   = walk.RGB(244, 130, 136)
)

type theme struct {
	brushes     map[walk.Color]*walk.SolidColorBrush
	pens        map[walk.Color]*walk.CosmeticPen
	fonts       map[int]*walk.Font
	scaledFonts map[[2]int]*walk.Font
}

func newTheme() (*theme, error) {
	t := &theme{brushes: make(map[walk.Color]*walk.SolidColorBrush), pens: make(map[walk.Color]*walk.CosmeticPen), fonts: make(map[int]*walk.Font), scaledFonts: make(map[[2]int]*walk.Font)}
	for _, color := range []walk.Color{navy, surface, panel, field, rowAlt, control, hover, pressed, border, headerBg, scrollBg, selection, fadedRow, teal, tealDark, white, secondary, muted, disabledText, ink, warning, errorColor} {
		brush, err := walk.NewSolidColorBrush(color)
		if err != nil {
			t.dispose()
			return nil, err
		}
		t.brushes[color] = brush
		pen, err := walk.NewCosmeticPen(walk.PenSolid, color)
		if err != nil {
			t.dispose()
			return nil, err
		}
		t.pens[color] = pen
	}
	for _, size := range []int{9, 10, 11, 13, 23, 26} {
		style := walk.FontStyle(0)
		if size >= 11 {
			style = walk.FontBold
		}
		font, err := walk.NewFont("Segoe UI", size, style)
		if err != nil {
			t.dispose()
			return nil, err
		}
		t.fonts[size] = font
	}
	return t, nil
}

func (t *theme) dispose() {
	for _, b := range t.brushes {
		b.Dispose()
	}
	for _, p := range t.pens {
		p.Dispose()
	}
	for _, f := range t.fonts {
		f.Dispose()
	}
	for _, f := range t.scaledFonts {
		f.Dispose()
	}
}

type painter struct {
	c   *walk.Canvas
	t   *theme
	err error
	dpi int
}

func (p *painter) check(err error) {
	if p.err == nil {
		p.err = err
	}
}

func (p *painter) rect(x, y, w, h int) walk.Rectangle {
	return walk.RectangleFrom96DPI(walk.Rectangle{X: x, Y: y, Width: w, Height: h}, p.dpi)
}

// Walk's buffered canvas uses the display DC's DPI rather than the widget's
// per-monitor DPI. Scale both geometry and cached fonts for that difference.
func (p *painter) font(size int) *walk.Font {
	if p.dpi == p.c.DPI() {
		return p.t.fonts[size]
	}
	scaled := max(1, (size*p.dpi+p.c.DPI()/2)/p.c.DPI())
	style := walk.FontStyle(0)
	if size >= 11 {
		style = walk.FontBold
	}
	key := [2]int{scaled, int(style)}
	if f := p.t.scaledFonts[key]; f != nil {
		return f
	}
	f, err := walk.NewFont("Segoe UI", scaled, style)
	if err != nil {
		p.check(err)
		return p.t.fonts[size]
	}
	p.t.scaledFonts[key] = f
	return f
}

func (p *painter) fill(color walk.Color, x, y, w, h int) {
	if w > 0 && h > 0 {
		p.check(p.c.FillRectanglePixels(p.t.brushes[color], p.rect(x, y, w, h)))
	}
}

func (p *painter) round(color walk.Color, x, y, w, h, radius int) {
	p.check(p.c.FillRoundedRectanglePixels(p.t.brushes[color], p.rect(x, y, w, h), walk.SizeFrom96DPI(walk.Size{Width: radius, Height: radius}, p.dpi)))
}

func (p *painter) text(text string, size int, color walk.Color, x, y, w, h int) {
	p.check(p.c.DrawTextPixels(text, p.font(size), color, p.rect(x, y, w, h), walk.TextLeft|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis|walk.TextNoPrefix))
}

func (p *painter) line(color walk.Color, x1, y1, x2, y2 int) {
	p.check(p.c.DrawLinePixels(p.t.pens[color], walk.PointFrom96DPI(walk.Point{X: x1, Y: y1}, p.dpi), walk.PointFrom96DPI(walk.Point{X: x2, Y: y2}, p.dpi)))
}

func (u *desktop) paintHeader(c *walk.Canvas, update walk.Rectangle) error {
	p := painter{c: c, t: u.theme, dpi: u.header.DPI()}
	p.check(c.FillRectanglePixels(u.theme.brushes[navy], update))
	w := u.header.ClientBounds().Width
	p.round(panel, 0, 12, 54, 54, 16)
	pulse := []walk.Point{{X: 8, Y: 39}, {X: 17, Y: 39}, {X: 23, Y: 25}, {X: 30, Y: 49}, {X: 36, Y: 33}, {X: 42, Y: 39}, {X: 48, Y: 39}}
	for i := 1; i < len(pulse); i++ {
		p.line(teal, pulse[i-1].X, pulse[i-1].Y, pulse[i].X, pulse[i].Y)
		p.line(teal, pulse[i-1].X, pulse[i-1].Y+1, pulse[i].X, pulse[i].Y+1)
	}
	if u.busy && u.animated {
		x := 7 + int((math.Sin(u.frame*0.16)+1)*19)
		p.round(white, x, 57, 4, 4, 4)
	}
	p.text("ProxyPulse", 26, white, 70, 2, w-70, 42)
	p.text("Рабочий прокси. В один клик.", 10, muted, 73, 44, w-73, 24)
	if w >= 480 {
		text, color := u.stateBadge()
		p.round(panel, w-147, 28, 140, 26, 12)
		p.round(color, w-134, 38, 6, 6, 6)
		p.text(text, 9, color, w-120, 29, 108, 24)
	}
	return p.err
}

func (u *desktop) stateBadge() (string, walk.Color) {
	if u.stopping {
		return "Остановка", warning
	}
	if u.busy {
		return "Поиск активен", teal
	}
	switch u.phase {
	case phaseDone:
		return "Готово", teal
	case phaseEmpty:
		return "Нет результатов", warning
	case phaseError:
		return "Ошибка", errorColor
	case phaseStopped:
		return "Остановлено", warning
	}
	if u.cached {
		return "Данные из кэша", muted
	}
	return "Готов к поиску", muted
}

func (u *desktop) paintCards(c *walk.Canvas, update walk.Rectangle) error {
	p := painter{c: c, t: u.theme, dpi: u.cards.DPI()}
	p.check(c.FillRectanglePixels(u.theme.brushes[navy], update))
	w := (u.cards.ClientBounds().Width - 20) / 3
	values := []string{"—", fmt.Sprint(len(u.results)), "—"}
	hints := []string{"Публичные пулы прокси", "Готовы к копированию", "Минимальная задержка"}
	if u.total > 0 {
		values[0] = fmt.Sprintf("%d / %d", u.done, u.total)
		hints[0] = fmt.Sprintf("Проверено • источников: %d", u.pools)
		if u.available > 0 {
			hints[0] = fmt.Sprintf("Собрано %d • пулов: %d", u.available, u.pools)
		}
		if u.rechecking {
			hints[0] = "Перепроверка выбранного прокси"
		}
	}
	if len(u.results) > 0 {
		values[2] = fmt.Sprintf("%d мс", u.results[0].LatencyMs)
	}
	if u.cached {
		hints[1] = "Из кэша • нужна свежая проверка"
	}
	titles := []string{"ОХВАТ ПРОВЕРКИ", "РАБОЧИЕ ПРОКСИ", "ЛУЧШИЙ ОТКЛИК"}
	for i := 0; i < 3; i++ {
		x := i * (w + 10)
		p.round(panel, x, 0, w, u.cards.ClientBounds().Height, 12)
		p.fill(border, x+16, 0, w-32, 2)
		if i == 1 {
			p.fill(teal, x+16, 0, w-32, 2)
		}
		p.text(titles[i], 9, muted, x+16, 9, w-32, 18)
		color := white
		if i == 1 {
			color = teal
		}
		p.text(values[i], 23, color, x+16, 28, w-32, 34)
		p.text(hints[i], 9, muted, x+16, 64, w-32, 18)
	}
	return p.err
}

func (u *desktop) paintSteps(c *walk.Canvas, update walk.Rectangle) error {
	p := painter{c: c, t: u.theme, dpi: u.steps.DPI()}
	p.check(c.FillRectanglePixels(u.theme.brushes[navy], update))
	w := (u.steps.ClientBounds().Width - 24) / 3
	active := -1
	switch u.phase {
	case phaseFetch:
		active = 0
	case phaseCheck:
		active = 1
	case phaseEnrich:
		active = 2
	case phaseDone, phaseEmpty:
		active = 3
	}
	titles := []string{"01   Загрузка источников", "02   Проверка доступности", "03   Скорость и анонимность"}
	if u.rechecking {
		titles[0] = "01   Выбранный прокси"
	}
	if u.phase != phaseReady && !u.enrichment {
		titles[2] = "03   Доп. тесты выключены"
	}
	for i, title := range titles {
		x := i * (w + 12)
		color := muted
		if i <= active {
			color = teal
		}
		if i == 2 && u.phase != phaseReady && !u.enrichment {
			color = muted
		}
		p.text(title, 9, color, x, 0, w, 24)
		p.round(border, x, 29, w, 3, 3)
		if i == 2 && u.phase != phaseReady && !u.enrichment {
			continue
		}
		if i < active {
			p.fill(teal, x, 29, w, 3)
		} else if i == active && u.busy {
			if i == 1 && u.total > 0 {
				progress := u.progress
				if !u.animated {
					progress = float64(u.done) / float64(u.total)
				}
				p.fill(teal, x, 29, int(float64(w)*progress), 3)
			} else {
				segment := w / 5
				pos := int((math.Sin(u.frame*0.09) + 1) * float64(w-segment) / 2)
				p.fill(teal, x+pos, 29, segment, 3)
			}
		}
	}
	return p.err
}

func (u *desktop) emptyText() (string, string) {
	if len(u.results) > 0 {
		return "Ничего по вашим фильтрам", "Измените запрос или нажмите «Сбросить», чтобы увидеть все прокси."
	}
	if u.busy {
		return "Ищем рабочие соединения…", "Результаты появятся здесь по мере проверки. Поиск можно остановить."
	}
	switch u.phase {
	case phaseError:
		return "Не удалось завершить поиск", "Подробности в строке состояния. Проверьте сеть и повторите попытку."
	case phaseEmpty:
		return "Рабочих прокси пока нет", "Попробуйте профиль «Глубокий» или повторите поиск позже."
	case phaseStopped:
		return "Поиск остановлен", "Нажмите «Найти прокси», когда будете готовы продолжить."
	default:
		return "Ваш следующий прокси — здесь", "Нажмите «Найти прокси». Мы проверим доступность и выберем самый быстрый."
	}
}

func (u *desktop) paintEmpty(c *walk.Canvas, update walk.Rectangle) error {
	p := painter{c: c, t: u.theme, dpi: u.empty.DPI()}
	p.check(c.FillRectanglePixels(u.theme.brushes[surface], update))
	b := u.empty.ClientBounds()
	y := max(8, (b.Height-108)/2)
	p.round(tealDark, b.Width/2-20, y, 40, 32, 12)
	p.line(teal, b.Width/2-12, y+17, b.Width/2-4, y+17)
	p.line(teal, b.Width/2-4, y+17, b.Width/2, y+9)
	p.line(teal, b.Width/2, y+9, b.Width/2+5, y+24)
	p.line(teal, b.Width/2+5, y+24, b.Width/2+10, y+17)
	title, detail := u.emptyText()
	for i, text := range []string{title, detail} {
		size, color := 13, white
		if i == 1 {
			size, color = 10, secondary
		}
		p.check(c.DrawTextPixels(text, p.font(size), color, p.rect(16, y+40+i*29, b.Width-32, 28), walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis|walk.TextNoPrefix))
	}
	return p.err
}

type cellStyler struct{ u *desktop }

// StyleCell is the fallback palette for Walk's own table painting. The native
// subclass in dark_table.go normally draws every cell itself; keeping these
// colors dark means an unhandled path can never flash a light row.
func (s cellStyler) StyleCell(style *walk.CellStyle) {
	r, ok := s.u.model.result(style.Row())
	if !ok {
		style.BackgroundColor = field
		style.TextColor = white
		return
	}
	style.BackgroundColor = field
	if style.Row()%2 != 0 {
		style.BackgroundColor = rowAlt
	}
	style.TextColor = resultTextColor(r, style.Col())
}

func resultTextColor(r proxy.Result, col int) walk.Color {
	switch col {
	case 0:
		return muted
	case 3:
		if r.LatencyMs < 500 {
			return teal
		} else if r.LatencyMs >= 1500 {
			return warning
		}
	case 5:
		if r.Anonymity == proxy.AnonTransparent {
			return errorColor
		}
	}
	return white
}
