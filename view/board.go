package view

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/halqme/govial/locales"
	"github.com/halqme/govial/protocol"
)

func (a *App) board(c *ui.Context) {
	if a.sel >= 0 && (a.sel >= len(a.kb.Keys) || !a.kb.Shows(a.kb.Keys[a.sel])) {
		a.sel = -1
	}
	keys := a.placedKeys()
	width, height := boardSize(keys)
	// pad is the inset around the keycaps, inside the painted canvas.
	const pad = float32(18)
	// Painted, not a grid of buttons: keycaps rotate and overlap,
	// which no collection widget lays out. MyGo v0.9.0.
	box := ui.Box(c).Grow(1).Focusable().Role(ui.RoleGroup).Label(locales.BoardLabel())
	switch {
	case box.Shortcut(0, ui.KeyLeft):
		a.moveSelection(-1, 0)
	case box.Shortcut(0, ui.KeyRight):
		a.moveSelection(1, 0)
	case box.Shortcut(0, ui.KeyUp):
		a.moveSelection(0, -1)
	case box.Shortcut(0, ui.KeyDown):
		a.moveSelection(0, 1)
	case box.Shortcut(0, ui.KeyEscape):
		a.sel = -1
	}
	a.hot = -1
	if px, py, ok := box.PointerPosition(); ok && a.unit > 0 {
		for i := len(keys) - 1; i >= 0; i-- {
			if keys[i].W < 0 {
				continue
			}
			if keys[i].Contains(float64((px-a.inset.X)/a.unit), float64((py-a.inset.Y)/a.unit)) {
				a.hot = i
				break
			}
		}
	}
	switch {
	case a.hot >= 0 && (box.Clicked() || box.RightClicked()):
		a.sel = a.hot
	case box.Clicked():
		a.sel = -1
	}
	box.ContextMenu(func(m *ui.Menu) {
		if a.sel >= 0 && a.sel < len(keys) {
			name, _ := capLabels(a.kb, keys[a.sel], a.layer)
			if name == "" {
				name = locales.KeyFallback()
			}
			m.Item(name).Disabled(true)
			m.Separator()
		}
		if m.Item(locales.Reload()).Disabled(a.busy).Chosen() {
			a.Refresh()
		}
		if a.dev != nil && m.Item(a.lockCommand()).Disabled(a.busy).Chosen() {
			a.toggleLock()
		}
	})
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		// The board shrinks to the space it is given, never past 1u = keyUnit.
		unit := float32(keyUnit)
		if fit := (r.W - pad*2) / width; fit < unit {
			unit = fit
		}
		if fit := (r.H - pad*2) / height; fit < unit {
			unit = fit
		}
		if unit < 1 {
			return
		}
		a.unit = unit
		a.inset = ui.Point{X: (r.W - width*unit) / 2, Y: (r.H - height*unit) / 2}
		origin := r
		origin.X += a.inset.X
		origin.Y += a.inset.Y
		for i, key := range keys {
			if key.W < 0 {
				continue
			}
			a.drawKey(c, p, origin, unit, i, key)
		}
	})
}

func (a *App) moveSelection(dx, dy int) {
	if a.kb == nil {
		return
	}
	keys := a.placedKeys()
	if a.sel < 0 || a.sel >= len(keys) || !choosable(keys[a.sel]) {
		for i, key := range keys {
			if choosable(key) {
				a.sel = i
				return
			}
		}
		return
	}
	cur := keys[a.sel]
	cx, cy := cur.X+cur.W/2, cur.Y+cur.H/2
	best := -1
	bestScore := math.MaxFloat64
	for i, key := range keys {
		if i == a.sel || !choosable(key) {
			continue
		}
		ox := (key.X + key.W/2) - cx
		oy := (key.Y + key.H/2) - cy
		var primary, secondary float64
		switch {
		case dx > 0:
			primary, secondary = ox, oy
		case dx < 0:
			primary, secondary = -ox, oy
		case dy > 0:
			primary, secondary = oy, ox
		default:
			primary, secondary = -oy, ox
		}
		if primary <= 0.2 {
			continue
		}
		score := primary + math.Abs(secondary)*4
		if score < bestScore {
			bestScore = score
			best = i
		}
	}
	if best >= 0 {
		a.sel = best
	}
}

func (a *App) drawKey(c *ui.Context, p *ui.Painter, r ui.Rect, unit float32, i int, key protocol.Key) {
	t := c.Theme()
	ox := r.X + float32(key.RotX)*unit
	oy := r.Y + float32(key.RotY)*unit
	p.Transform(ui.Rotation(float32(key.Rot)), ox, oy, func() {
		gap := unit * 0.065
		lip := unit * 0.075
		x := r.X + float32(key.X)*unit + gap
		y := r.Y + float32(key.Y)*unit + gap
		w := float32(key.W)*unit - gap*2
		h := float32(key.H)*unit - gap*2
		if w < 8 || h < 8 {
			return
		}
		radius := t.Radius
		if radius <= 0 {
			radius = 8
		}
		accent := t.Accent
		if !c.WindowActive() {
			accent = t.TextMuted
			if t.InactiveAccent.A != 0 {
				accent = t.InactiveAccent
			}
		}
		body := t.Surface.Mix(ui.RGB(0, 0, 0), 0.22)
		face := t.Surface
		labelColor := t.Text
		switch {
		case a.holding(key):
			body = t.Warning
			face = t.Surface.Mix(t.Warning, 0.28)
		case i == a.sel:
			body = accent
			face = t.Surface.Mix(accent, 0.22)
		case i == a.hot:
			face = t.SurfaceHover
		}
		code, named := keyCode(a.kb, key, a.layer)
		if named && code == 1 && i != a.sel {
			labelColor = t.TextMuted
		}
		cap := ui.Rect{X: x, Y: y, W: w, H: h}
		p.Shadow(cap, radius, 0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.18))
		p.Fill(cap, body, radius)
		faceH := h - lip
		if faceH < h*0.72 {
			faceH = h * 0.72
		}
		p.Fill(ui.Rect{X: x, Y: y, W: w, H: faceH}, face, radius)
		main, sub := capLabels(a.kb, key, a.layer)
		// A custom name can break into lines, as "Next\nBT"; they stack.
		lines := strings.Split(main, "\n")
		longest := 0
		for _, line := range lines {
			longest = max(longest, utf8.RuneCountInString(line))
		}
		// Two sizes, so labels line up across the board; a label that still
		// does not fit shrinks on its own as a last resort.
		size := unit * 0.3
		if longest > 3 || len(lines) > 1 {
			size *= 0.78
		}
		spans := make([]ui.Span, len(lines))
		widths := make([]float32, len(lines))
		var lineH float32
		for {
			fits := true
			for i, line := range lines {
				spans[i] = ui.Span{Text: line, Weight: 600, Size: size, Color: labelColor}
				widths[i], lineH = p.MeasureText(0, spans[i])
				fits = fits && widths[i] <= w-unit*0.16
			}
			if fits || size <= 7 {
				break
			}
			size--
		}
		mainH := faceH
		if sub != "" {
			mainH = faceH * 0.7
			small := ui.Span{Text: sub, Weight: 500, Size: unit * 0.2, Color: t.TextMuted}
			sw, sh := p.MeasureText(0, small)
			p.RichText(x+(w-sw)/2, y+faceH-sh-unit*0.06, 0, small)
		}
		if main == "" {
			return
		}
		top := y + (mainH-lineH*float32(len(lines)))/2
		for i := range lines {
			p.RichText(x+(w-widths[i])/2, top+lineH*float32(i), 0, spans[i])
		}
	})
}

func (a *App) holdText() string {
	if len(a.hold) == 0 {
		return locales.UnlockKeys()
	}
	names := make([]string, 0, len(a.hold))
	for _, pos := range a.hold {
		if a.kb != nil {
			if code, ok := a.kb.Code(0, pos.Row, pos.Col); ok {
				name := buttonName(a.kb, code)
				names = append(names, name)
				continue
			}
		}
		names = append(names, fmt.Sprintf("%d,%d", pos.Row, pos.Col))
	}
	return strings.Join(names, locales.HoldAnd())
}

func (a *App) holding(key protocol.Key) bool {
	if !a.unlocking || !key.HasMatrix {
		return false
	}
	for _, pos := range a.hold {
		if pos.Row == key.Row && pos.Col == key.Col {
			return true
		}
	}
	return false
}

// capName is how a keycode reads on a keycap and in the choices: the
// keyboard's own name for its custom keys, the Mac's symbol where a key has
// one, and QMK's short name otherwise. KC_NO reads as nothing.
func capName(kb *protocol.Keyboard, code uint16) string {
	if sym, ok := rightCaps[code]; ok {
		return locales.RightPrefix() + sym
	}
	if sym, ok := capSymbols[code]; ok {
		return sym
	}
	return kb.Label(code)
}

// buttonName is capName for a control, on one line, where KC_NO needs a
// name. Custom names break lines for keycaps, as "Next\nBT".
func buttonName(kb *protocol.Keyboard, code uint16) string {
	if name := capName(kb, code); name != "" {
		return oneLine(name)
	}
	return "No"
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// rightCaps are the right-side modifiers. The prefix is the language's, so
// Japanese reads 右⌘ and English reads R⌘.
var rightCaps = map[uint16]string{
	0x00E4: "⌃",
	0x00E5: "⇧",
	0x00E6: "⌥",
	0x00E7: "⌘",
}
var capSymbols = map[uint16]string{
	0x0028: "⏎",
	0x002A: "⌫",
	0x0039: "⇪",
	0x004A: "↖",
	0x004B: "⇞",
	0x004C: "⌦",
	0x004D: "↘",
	0x004E: "⇟",
	0x004F: "→",
	0x0050: "←",
	0x0051: "↓",
	0x0052: "↑",
	0x00E0: "⌃",
	0x00E1: "⇧",
	0x00E2: "⌥",
	0x00E3: "⌘",
	0x0087: "ろ",
	0x0089: "¥",
	0x008A: "変換",
	0x008B: "無変換",
	0x0090: "かな",
	0x0091: "英数",
}

func keyCode(kb *protocol.Keyboard, key protocol.Key, layer int) (uint16, bool) {
	if key.Encoder {
		return kb.EncoderCode(layer, key.EncoderIndex, key.EncDir)
	}
	if !key.HasMatrix {
		return 0, false
	}
	return kb.Code(layer, key.Row, key.Col)
}

// capLabels is what a keycap shows: main, large, is the tap, and sub,
// small under it, is the hold or a knob's turn.
func capLabels(kb *protocol.Keyboard, key protocol.Key, layer int) (main, sub string) {
	if key.Encoder {
		code, _ := kb.EncoderCode(layer, key.EncoderIndex, key.EncDir)
		if key.EncDir == 1 {
			return capName(kb, code), "↻"
		}
		return capName(kb, code), "↺"
	}
	if !key.HasMatrix {
		if len(key.Labels) > 0 {
			return key.Labels[0], ""
		}
		return "", ""
	}
	code, ok := kb.Code(layer, key.Row, key.Col)
	if !ok {
		return "", ""
	}
	tap, hold := protocol.SplitTapHold(code)
	switch {
	case hold == 0:
		return capName(kb, tap), ""
	case tap == 0:
		return shortHold(hold), locales.WhileHeld()
	default:
		return capName(kb, tap), shortHold(hold)
	}
}

// shortHold is holdLabel cut to fit under a keycap's tap.
func shortHold(hold uint16) string {
	if hold >= 0x5220 && hold <= 0x523F {
		return fmt.Sprintf("L%d", hold-0x5220)
	}
	return holdLabel(hold)
}

func boardSize(keys []protocol.Key) (float32, float32) {
	var w, h float64
	for _, key := range keys {
		if key.W < 0 {
			continue
		}
		if x := key.X + key.W; x > w {
			w = x
		}
		if y := key.Y + key.H; y > h {
			h = y
		}
		if x := key.RotX + key.W + 2; key.Rot != 0 && x > w {
			w = x
		}
		if y := key.RotY + key.H + 2; key.Rot != 0 && y > h {
			h = y
		}
	}
	if w < 4 {
		w = 4
	}
	if h < 2 {
		h = 2
	}
	return float32(w), float32(h)
}
