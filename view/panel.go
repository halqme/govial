package view

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/halqme/govial/locales"
	"github.com/halqme/govial/protocol"
)

// panel is the floating pane for the chosen key: its slots and their
// choices, or why it cannot be assigned yet. It is always there, at one
// height, so choosing a key or switching slots never moves the board: the
// six rows of the basic board, under the kind switch.
func (a *App) panel(c *ui.Context) {
	t := c.Theme()
	u := pickUnit(c)
	panel := ui.Row(c.Key("panel")).Height(6*u + 5*pickSpacing(c) + t.Rem(2.25) + t.Space(10) + t.Space(1))
	panel.Margin(0, t.Space(4), t.Space(4), t.Space(4)).Radius(t.Rem(1.25)).Material(glass.Glass{Clarity: 0.3}).Border(1, t.Border).AlignItems(ui.Stretch).Role(ui.RoleGroup).Label(locales.PanelLabel()).Children(func() {
		if a.unlocking {
			ui.Column(c).Grow(1).Center().Gap(t.Space(2)).Padding(t.Space(4)).Children(func() {
				a.unlockStatus(c)
			})
			return
		}
		if a.sel < 0 || a.sel >= len(a.kb.Keys) {
			ui.Column(c).Grow(1).Center().Gap(t.Space(3)).Padding(t.Space(4)).Children(func() {
				ui.Text(c, locales.HintChooseKey()).TextColor(t.TextMuted)
				if !a.unlocked {
					a.lockNote(c)
					ui.PrimaryButton(c.Key("unlock"), locales.LockUnlock()).Disabled(a.busy || a.dev == nil).OnClick(a.toggleLock)
				}
			})
			return
		}
		key := a.kb.Keys[a.sel]
		ui.Column(c).Width(t.Rem(13)).Padding(t.Space(4)).Gap(t.Space(3)).Children(func() {
			a.keySummary(c, key)
			if key.HasMatrix || key.Encoder {
				a.slots(c, key)
			}
		})
		ui.Column(c).Grow(1).Children(func() {
			switch {
			case !key.HasMatrix && !key.Encoder:
				ui.Text(c, locales.NotInMatrix()).TextColor(t.TextMuted).Margin(t.Space(4))
			case !a.unlocked:
				ui.Column(c).Padding(t.Space(4)).Gap(t.Space(3)).Children(func() {
					a.lockNote(c)
					ui.PrimaryButton(c.Key("unlock"), locales.LockUnlock()).Disabled(a.busy || a.dev == nil).OnClick(a.toggleLock).AlignSelf(ui.Start)
				})
			case a.slotOf(key) == slotHold:
				a.holdPicker(c)
			default:
				a.picker(c)
			}
		})
	})
}

// editable is the chosen key when it can be assigned now.
func (a *App) editable() (protocol.Key, bool) {
	if a.kb == nil || a.unlocking || !a.unlocked || a.sel < 0 || a.sel >= len(a.kb.Keys) {
		return protocol.Key{}, false
	}
	key := a.kb.Keys[a.sel]
	return key, key.HasMatrix || key.Encoder
}

// The slots are what a key does on a tap and on a hold. A key with both is
// one LT or mod-tap keycode; the keyboard has no other place for them.
const (
	slotTap  = "tap"
	slotHold = "hold"
)

// slotOf is the slot being edited. A knob turns, so it has only a tap.
func (a *App) slotOf(key protocol.Key) string {
	if key.Encoder {
		return slotTap
	}
	return a.slot
}

func (a *App) slots(c *ui.Context, key protocol.Key) {
	if key.Encoder || !a.unlocked {
		return
	}
	tap, hold := a.tapHold()
	ui.Sidebar(c, &a.slot, func() {
		ui.SidebarItem(c, slotTap, nil, locales.SlotTap()).Children(func() {
			ui.Badge(c, a.tapLabel(tap))
		})
		ui.SidebarItem(c, slotHold, nil, locales.SlotHold()).Children(func() {
			ui.Badge(c, holdLabel(hold))
		})
	}).Label(locales.SlotsLabel()).Background(ui.RGBA(0, 0, 0, 0))
}

// tapHold is the selected key's code read as a tap and a hold.
func (a *App) tapHold() (tap, hold uint16) {
	code := a.current()
	if code == 0xFFFF {
		return 0, 0
	}
	return protocol.SplitTapHold(code)
}

func (a *App) tapLabel(tap uint16) string {
	if tap == 0 {
		return locales.None()
	}
	return buttonName(a.kb, tap)
}

func holdLabel(hold uint16) string {
	switch {
	case hold == 0:
		return locales.None()
	case hold >= 0x5220 && hold <= 0x523F:
		return locales.LayerN(int(hold - 0x5220))
	}
	mods := byte(hold >> 8)
	var b strings.Builder
	if mods&0x10 != 0 {
		b.WriteString(locales.RightPrefix())
	}
	for _, m := range []struct {
		bit  byte
		sign string
	}{{0x01, "⌃"}, {0x02, "⇧"}, {0x04, "⌥"}, {0x08, "⌘"}} {
		if mods&m.bit != 0 {
			b.WriteString(m.sign)
		}
	}
	return b.String()
}

// holdPicker offers what a hold can do: a layer while held, or modifiers.
func (a *App) holdPicker(c *ui.Context) {
	tap, hold := a.tapHold()
	open := tap <= 0xFF
	layers := 0
	if a.kb != nil {
		layers = a.kb.Layers
	}
	if layers > 16 {
		layers = 16
	}
	var layerHolds []uint16
	for layer := 0; layer < layers; layer++ {
		code, _ := protocol.MomentaryLayer(layer)
		layerHolds = append(layerHolds, code)
	}
	var modHolds []uint16
	for _, mods := range []byte{0x01, 0x02, 0x04, 0x08, 0x11, 0x12, 0x14, 0x18, 0x07, 0x0F} {
		modHolds = append(modHolds, protocol.ModHold(mods))
	}
	groups := []keyGroup{
		{codes: []uint16{0}},
		{name: locales.GroupLayer(), codes: layerHolds, short: layerNumber},
		{name: locales.GroupModifiers(), codes: modHolds},
	}
	var note string
	if !open {
		note = locales.NoteHoldBasicOnly()
	}
	choices(c, note, nil, groups, func(code uint16, short string) {
		a.choiceButton(c, fmt.Sprintf("hold-%04x", code), holdLabel(code), short, code == hold, code != 0 && !open, func() {
			a.assignTapHold(tap, code)
		})
	})
}

// choices scrolls groups of choice buttons under their headings, after a
// note and lead when there are.
func choices(c *ui.Context, note string, lead func(), groups []keyGroup, button func(code uint16, short string)) {
	t := c.Theme()
	ui.Scroll(c).Grow(1).Padding(t.Space(2), t.Space(4), t.Space(4), t.Space(4)).Gap(t.Space(2)).Children(func() {
		if note != "" {
			ui.Text(c, note).TextColor(t.TextMuted)
		}
		if lead != nil {
			lead()
		}
		// Headings sit beside their choices, so a pane of groups fits the
		// panel without scrolling.
		for _, group := range groups {
			ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Start).Children(func() {
				// As tall as a choice, so the heading centers on the first row.
				ui.Column(c).Width(t.Rem(5.5)).Height(pickUnit(c)).Shrink(0).Justify(ui.Center).Children(func() {
					ui.Text(c, group.name).FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
				})
				ui.Row(c).Grow(1).Wrap().Gap(t.Space(1)).Children(func() {
					for _, code := range group.codes {
						short := ""
						if group.short != nil {
							short = group.short(code)
						}
						button(code, short)
					}
				})
			})
		}
	})
}

// assignTapHold writes tap and hold as the one keycode that carries both.
func (a *App) assignTapHold(tap, hold uint16) {
	code, ok := protocol.JoinTapHold(tap, hold)
	if !ok {
		a.setFault(fault{kind: faultHoldBasic})
		return
	}
	a.assign(code)
}

func (a *App) keySummary(c *ui.Context, key protocol.Key) {
	t := c.Theme()
	code, _ := keyCode(a.kb, key, a.layer)
	name, _ := capLabels(a.kb, key, a.layer)
	name = oneLine(name)
	if name == "" {
		name = "No"
	}
	ui.Text(c, name).FontSize(t.Rem(1.6)).Bold()
	if key.Encoder {
		turn := locales.TurnCounterclockwise()
		if key.EncDir == 1 {
			turn = locales.TurnClockwise()
		}
		ui.Text(c, locales.EncoderSummary(key.EncoderIndex+1, turn, fmt.Sprintf("%04X", code))).FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
		return
	}
	ui.Text(c, locales.KeySummary(a.layer, key.Row, key.Col, fmt.Sprintf("%04X", code))).FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
}

func (a *App) lockNote(c *ui.Context) {
	if a.unlocked || a.unlocking {
		return
	}
	ui.Text(c, locales.LockNote()).TextColor(c.Theme().TextMuted)
}

func (a *App) current() uint16 {
	if a.kb == nil || a.sel < 0 || a.sel >= len(a.kb.Keys) {
		return 0xFFFF
	}
	key := a.kb.Keys[a.sel]
	code, ok := keyCode(a.kb, key, a.layer)
	if !ok {
		return 0xFFFF
	}
	return code
}

func choosable(key protocol.Key) bool {
	return key.W >= 0 && (key.HasMatrix || key.Encoder)
}

// tapButton offers code for the tap. While the key has a hold, only a basic
// key can join it.
func (a *App) tapButton(c *ui.Context, code uint16, short string) ui.Element {
	tap, hold := a.tapHold()
	return a.choiceButton(c, fmt.Sprintf("kc-%04x", code), buttonName(a.kb, code), short, code == tap && a.current() != 0xFFFF, hold != 0 && code > 0xFF, func() {
		a.assignTapHold(code, hold)
	})
}

// choiceButton offers one choice named label, showing short instead when
// it is set.
func (a *App) choiceButton(c *ui.Context, key, label, short string, chosen, off bool, pick func()) ui.Element {
	text := label
	if short != "" {
		text = short
	}
	var b ui.Element
	if chosen {
		b = ui.PrimaryButton(c.Key(key), text)
	} else {
		b = ui.Button(c.Key(key), text)
	}
	if short != "" {
		b.Label(label).Tooltip(label)
	}
	// Choices keep their width in a wrapping row and are keycaps the size
	// of the basic board's, so every page of choices reads at one scale.
	u := pickUnit(c)
	return b.Padding(0, c.Theme().Space(2)).MinWidth(u).Height(u).FontSize(u * 0.36).Shrink(0).Disabled(off || a.busy || a.unlocking || !a.unlocked).OnClick(pick)
}

type keyGroup struct {
	name  string
	codes []uint16
	// short, when set, is the text a choice shows, as a layer's number
	// under a heading that names the action; the full name stays its label.
	short func(code uint16) string
}

// layerNumber is the layer a layer keycode names, as short text.
func layerNumber(code uint16) string {
	return strconv.Itoa(int(code & 0x1F))
}

type keyKind int

const (
	kindBasic keyKind = iota
	kindOther
	kindLayer
	kindMacro
	kindCustom
)

func (a *App) keyKinds() []keyKind {
	kinds := []keyKind{kindBasic, kindOther, kindLayer}
	if a.kb != nil && a.kb.Macros > 0 {
		kinds = append(kinds, kindMacro)
	}
	if a.kb != nil && len(a.kb.Custom) > 0 {
		kinds = append(kinds, kindCustom)
	}
	return kinds
}

func kindLabel(kind keyKind) string {
	switch kind {
	case kindOther:
		return locales.KindOther()
	case kindLayer:
		return locales.KindLayer()
	case kindMacro:
		return locales.KindMacro()
	case kindCustom:
		return locales.KindCustom()
	default:
		return locales.KindBasic()
	}
}

func (a *App) picker(c *ui.Context) {
	t := c.Theme()
	kinds := a.keyKinds()
	if a.pick < 0 || a.pick >= len(kinds) {
		a.pick = 0
	}
	labels := make([]string, len(kinds))
	for i, kind := range kinds {
		labels[i] = kindLabel(kind)
	}
	// Segmented switches which keys are offered. Tabs would be the same pages with more chrome.
	ui.Segmented(c, &a.pick, labels...).Label(locales.KeyKind()).AlignSelf(ui.Start).Margin(t.Space(4), t.Space(4), 0, t.Space(4))
	var groups []keyGroup
	switch kinds[a.pick] {
	case kindLayer:
		groups = layerPalette(a.kb)
	case kindMacro:
		groups = macroPalette(a.kb)
	case kindCustom:
		groups = customPalette(a.kb)
	case kindOther:
		groups = palette()
	}
	var note string
	if _, hold := a.tapHold(); hold != 0 && kinds[a.pick] != kindBasic && kinds[a.pick] != kindOther {
		note = locales.NoteHoldLimitsTap()
	}
	var lead func()
	if kinds[a.pick] == kindBasic {
		lead = func() { a.basicBoard(c) }
	}
	if kinds[a.pick] == kindMacro {
		a.macroLead(c)
	}
	choices(c, note, lead, groups, func(code uint16, short string) { a.tapButton(c, code, short) })
}

// pickKey is one key of the basic board, w units wide; code room is space.
type pickKey struct {
	code uint16
	w    float64
}

const room = 0xFFFF

// basicRows is a tenkeyless ANSI board in the Mac's order, ⌃ ⌥ ⌘, so a
// basic key is found where it sits on a keyboard. Print Screen, Scroll
// Lock and Pause, which a Mac does not use, are in the palette instead.
var basicRows = [][]pickKey{
	{{0x29, 1}, {room, 1}, {0x3A, 1}, {0x3B, 1}, {0x3C, 1}, {0x3D, 1}, {room, 0.5}, {0x3E, 1}, {0x3F, 1}, {0x40, 1}, {0x41, 1}, {room, 0.5}, {0x42, 1}, {0x43, 1}, {0x44, 1}, {0x45, 1}},
	{{0x35, 1}, {0x1E, 1}, {0x1F, 1}, {0x20, 1}, {0x21, 1}, {0x22, 1}, {0x23, 1}, {0x24, 1}, {0x25, 1}, {0x26, 1}, {0x27, 1}, {0x2D, 1}, {0x2E, 1}, {0x2A, 2}, {room, 0.25}, {0x49, 1}, {0x4A, 1}, {0x4B, 1}},
	{{0x2B, 1.5}, {0x14, 1}, {0x1A, 1}, {0x08, 1}, {0x15, 1}, {0x17, 1}, {0x1C, 1}, {0x18, 1}, {0x0C, 1}, {0x12, 1}, {0x13, 1}, {0x2F, 1}, {0x30, 1}, {0x31, 1.5}, {room, 0.25}, {0x4C, 1}, {0x4D, 1}, {0x4E, 1}},
	{{0x39, 1.75}, {0x04, 1}, {0x16, 1}, {0x07, 1}, {0x09, 1}, {0x0A, 1}, {0x0B, 1}, {0x0D, 1}, {0x0E, 1}, {0x0F, 1}, {0x33, 1}, {0x34, 1}, {0x28, 2.25}},
	{{0xE1, 2.25}, {0x1D, 1}, {0x1B, 1}, {0x06, 1}, {0x19, 1}, {0x05, 1}, {0x11, 1}, {0x10, 1}, {0x36, 1}, {0x37, 1}, {0x38, 1}, {0xE5, 2.75}, {room, 1.25}, {0x52, 1}},
	{{0xE0, 1.25}, {0xE2, 1.25}, {0xE3, 1.25}, {0x2C, 6.25}, {0xE7, 1.25}, {0xE6, 1.25}, {0x65, 1.25}, {0xE4, 1.25}, {room, 0.25}, {0x50, 1}, {0x51, 1}, {0x4F, 1}},
}

// pickUnit is the 1u of the basic board in the panel. It grows with the
// window, so the board and the choices keep their proportion, and stays
// within the panel's width beside the slots.
func pickUnit(c *ui.Context) float32 {
	t := c.Theme()
	w, h := c.Size()
	u := h * 0.05
	// The panel's margins and padding, and the slot column.
	if fit := (w - t.Space(16) - t.Rem(13)) / 18.5; fit < u {
		u = fit
	}
	return max(t.Rem(1.75), min(u, t.Rem(3.25)))
}

func pickSpacing(c *ui.Context) float32 {
	return c.Theme().Space(1) / 2
}

func (a *App) basicBoard(c *ui.Context) {
	unit := pickUnit(c)
	spacing := pickSpacing(c)
	ui.Column(c).Gap(spacing).Role(ui.RoleGroup).Label(locales.BasicKeys()).Children(func() {
		for _, row := range basicRows {
			ui.Row(c).Gap(spacing).Children(func() {
				for _, key := range row {
					width := float32(key.w)*unit - spacing
					if key.code == room {
						ui.Box(c).Width(width).Shrink(0)
						continue
					}
					b := a.tapButton(c, key.code, "").Width(width).MinWidth(width).Height(unit).Padding(0)
					// A symbol reads at full size, a word smaller, as on keycaps.
					switch n := utf8.RuneCountInString(buttonName(a.kb, key.code)); {
					case n > 3:
						b.FontSize(unit * 0.3)
					case n > 1:
						b.FontSize(unit * 0.34)
					default:
						b.FontSize(unit * 0.42)
					}
				}
			})
		}
	})
}

func layerPalette(kb *protocol.Keyboard) []keyGroup {
	n := 0
	if kb != nil {
		n = kb.Layers
	}
	if n > 32 {
		n = 32
	}
	type op struct {
		name  string
		limit int
		make  func(int) (uint16, bool)
	}
	ops := []op{
		{locales.LayerToggle(), 32, protocol.ToggleLayer},
		{locales.LayerTo(), 32, protocol.ToLayer},
		{locales.LayerTapToggle(), 32, protocol.TapToggleLayer},
		{locales.LayerOneShot(), 32, protocol.OneShotLayer},
		{locales.LayerDefault(), 32, protocol.DefaultLayer},
	}
	groups := make([]keyGroup, 0, len(ops))
	for _, item := range ops {
		groups = append(groups, keyGroup{name: item.name, codes: layerCodes(n, item.limit, item.make), short: layerNumber})
	}
	return groups
}

func layerCodes(layers, limit int, code func(int) (uint16, bool)) []uint16 {
	if layers < 0 {
		layers = 0
	}
	if layers < limit {
		limit = layers
	}
	codes := make([]uint16, 0, limit)
	for layer := 0; layer < limit; layer++ {
		got, ok := code(layer)
		if ok {
			codes = append(codes, got)
		}
	}
	return codes
}

func macroPalette(kb *protocol.Keyboard) []keyGroup {
	n := 0
	if kb != nil {
		n = kb.Macros
	}
	if n > 128 {
		n = 128
	}
	codes := make([]uint16, 0, n)
	for i := 0; i < n; i++ {
		code, ok := protocol.MacroKey(i)
		if ok {
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		return nil
	}
	return []keyGroup{{name: locales.KindMacro(), codes: codes}}
}

func customPalette(kb *protocol.Keyboard) []keyGroup {
	if kb == nil || len(kb.Custom) == 0 {
		return nil
	}
	n := len(kb.Custom)
	if n > 64 {
		n = 64
	}
	codes := make([]uint16, n)
	for i := range codes {
		codes[i] = uint16(0x7E00 + i)
	}
	return []keyGroup{{name: locales.KindCustom(), codes: codes}}
}

// palette is the basic keys the basic board does not hold.
func palette() []keyGroup {
	return []keyGroup{
		{name: locales.GroupJapanese(), codes: []uint16{0x0091, 0x0090, 0x0089, 0x0087, 0x008B, 0x008A}},
		{name: locales.GroupMedia(), codes: []uint16{0x00A8, 0x00AA, 0x00A9, 0x00AC, 0x00AE, 0x00AB, 0x00BE, 0x00BD}},
		{name: locales.GroupMouse(), codes: []uint16{0x00CD, 0x00CE, 0x00CF, 0x00D0, 0x00D1, 0x00D2, 0x00D3}},
		{name: locales.GroupWheel(), codes: []uint16{0x00D9, 0x00DA, 0x00DB, 0x00DC}},
		{name: locales.KindOther(), codes: []uint16{0x0000, 0x0001, 0x0046, 0x0047, 0x0048}},
	}
}
