package main

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/halqme/govial/hid"
	"github.com/halqme/govial/protocol"
)

// keyUnit is the size of a 1u keycap in DIPs. It stays fixed so the
// layout's proportions do not follow the text size.
const keyUnit = 54.0

// keyboardIcon is shown where the system has no SF Symbol.
var keyboardIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M3 7h18v10H3z"/><path d="M6 10h2v2H6zm4 0h2v2h-2zm4 0h2v2h-2zm4 0h2v2h-2zM7 14h10v2H7z"/></svg>`))

var (
	lockIcon   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M7 10V7a5 5 0 0 1 10 0v3h1v11H6V10zm2 0h6V7a3 3 0 0 0-6 0z"/></svg>`))
	unlockIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M9 10V7a3 3 0 0 1 6 0h2a5 5 0 0 0-10 0v3H6v11h12V10z"/></svg>`))
	reloadIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M12 5V2l4 4-4 4V7a5 5 0 1 0 5 5h2a7 7 0 1 1-7-7z"/></svg>`))
)

type app struct {
	win         *mygo.Window
	gen         int
	busy        bool
	err         string
	flash       string
	windowTitle string
	infos       []hid.Info
	entry       uint64
	device      string
	dev         *hid.Device
	kb          *protocol.Keyboard
	layer       int
	sel         int
	hot         int
	unlocked    bool
	unlocking   bool
	remain      int
	hold        []protocol.KeyPos
	// unit and inset are the keycap scale and offset of the last drawn
	// board, so pointer hits match what is on screen.
	unit       float32
	inset      ui.Point
	pick       int
	slot       string
	layout     []int
	layoutKB   *protocol.Keyboard
	reloadItem *mygo.MenuItem
	lockItem   *mygo.MenuItem
	reloadOn   bool
	lockOn     bool
	lockLabel  string
}

func newApp() *app {
	return &app{sel: -1, hot: -1, slot: slotTap}
}

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	a.syncWindowTitle()
	a.syncMenu()
	if a.flash != "" {
		c.Toast(a.flash)
		a.flash = ""
	}
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.toolbar(c)
		// MyGo v0.9.0 has no banner. This notice stays while the failure does.
		if a.err != "" {
			ui.Row(c).Margin(0, t.Space(4), t.Space(2), t.Space(4)).Padding(t.Space(2), t.Space(3)).Radius(t.Radius).Background(t.Danger.Mix(t.Background, 0.85)).Children(func() {
				ui.Text(c, a.err).Role(ui.RoleAlert).TextColor(t.Danger)
			})
		}
		switch {
		case a.kb == nil && a.unlocking:
			a.unlockPane(c)
		case a.kb == nil:
			a.empty(c)
		default:
			a.workspace(c)
		}
	})
}

func (a *app) installMenus() {
	// Not a document app, so there is no File menu. View leaves out Reload:
	// that role reloads a page, and its shortcut would do nothing here.
	a.reloadItem = &mygo.MenuItem{
		ID:          "reload",
		Label:       "再読み込み",
		Accelerator: "CmdOrCtrl+R",
		Click:       func(*mygo.MenuItem, *mygo.Window) { a.refresh() },
	}
	a.lockItem = &mygo.MenuItem{
		ID:          "lock",
		Label:       "解除",
		Accelerator: "CmdOrCtrl+Shift+U",
		Disabled:    true,
		Click:       func(*mygo.MenuItem, *mygo.Window) { a.toggleLock() },
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Role: mygo.RoleEditMenu},
		{Label: "キーボード", Submenu: []*mygo.MenuItem{a.reloadItem, mygo.Separator(), a.lockItem}},
		{Role: mygo.RoleViewMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleResetZoom},
			{Role: mygo.RoleZoomIn},
			{Role: mygo.RoleZoomOut},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu},
		{Role: mygo.RoleHelp},
	}))
}

func (a *app) syncWindowTitle() {
	title := "Govial"
	if name := a.keyboardName(); name != "" {
		title = name
	}
	if a.win == nil || title == a.windowTitle {
		return
	}
	a.windowTitle = title
	a.win.SetTitle(title)
}

func (a *app) syncMenu() {
	if a.reloadItem == nil || a.lockItem == nil {
		return
	}
	reloadOn := !a.busy
	if reloadOn != a.reloadOn {
		a.reloadOn = reloadOn
		a.reloadItem.SetEnabled(reloadOn)
	}
	label := a.lockCommand()
	if label != a.lockLabel {
		a.lockLabel = label
		a.lockItem.SetLabel(label)
	}
	lockOn := a.dev != nil && !a.busy
	if lockOn != a.lockOn {
		a.lockOn = lockOn
		a.lockItem.SetEnabled(lockOn)
	}
}

func (a *app) lockCommand() string {
	switch {
	case a.unlocking:
		return "解除中"
	case a.unlocked:
		return "ロック"
	default:
		return "解除"
	}
}

func (a *app) toggleLock() {
	if a.busy || a.dev == nil {
		return
	}
	if a.unlocked {
		a.lock()
		return
	}
	if !a.unlocking {
		a.beginUnlock()
	}
}

// toolbar is the window's title bar: the window controls, the keyboard,
// its layers in the middle, and the lock and reload at the end.
func (a *app) toolbar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Toolbar(c, func() {
		a.deviceSelect(c)
		ui.Spacer(c)
		a.layers(c)
		ui.Spacer(c)
		if a.busy {
			ui.Spinner(c).Label("通信中")
		}
		if a.dev != nil {
			a.lockButton(c)
		}
		iconButton(c, "reload", "arrow.clockwise", reloadIcon, "再読み込み").Disabled(a.busy).OnClick(a.refresh)
	}).Label("操作").Height(max(bar.Height, t.Rem(3.25))).Padding(0, bar.Right+t.Space(3), 0, bar.Left+t.Space(3)).DragWindow()
}

func (a *app) lockButton(c *ui.Context) {
	symbol, icon, tip := "lock.fill", lockIcon, "ロック中。押すと解除します"
	if a.unlocked || a.unlocking {
		symbol, icon, tip = "lock.open.fill", unlockIcon, "解除中。押すとロックします"
	}
	iconButton(c, "lock", symbol, icon, a.lockCommand()).Tooltip(tip).Disabled(a.busy).OnClick(a.toggleLock)
}

// iconButton is a title bar button that shows an SF Symbol, or fallback
// where the system has none, named by label.
func iconButton(c *ui.Context, key, symbol string, fallback *ui.SVG, label string) ui.Element {
	t := c.Theme()
	return ui.Button(c.Key(key), "").Children(func() {
		sfsymbols.Symbol(c, symbol).Size(t.Rem(1.05)).Color(t.Text).Fallback(fallback).Role(ui.RoleNone)
	}).Label(label).Tooltip(label)
}

func (a *app) deviceSelect(c *ui.Context) {
	if len(a.infos) < 2 {
		if name := a.keyboardName(); name != "" {
			ui.Text(c, name).Bold().Tooltip(a.kb.UIDString())
		}
		return
	}
	names := deviceNames(a.infos)
	for i, info := range a.infos {
		if info.Entry == a.entry {
			a.device = names[i]
			break
		}
	}
	if ui.Select(c, &a.device, names).Label("キーボード").Changed() {
		for i, name := range names {
			if name == a.device && a.infos[i].Entry != a.entry {
				a.entry = a.infos[i].Entry
				a.open(a.infos[i])
				return
			}
		}
	}
}

// keyboardName is the product name the device reports, which names the
// board itself, or else the name in its Vial definition, which is often a
// placeholder such as "HID Keyboard".
func (a *app) keyboardName() string {
	if a.kb == nil {
		return ""
	}
	for _, info := range a.infos {
		if info.Entry == a.entry {
			if name := strings.TrimSpace(info.Name); name != "" {
				return name
			}
		}
	}
	return a.kb.Name
}

func deviceNames(infos []hid.Info) []string {
	names := make([]string, len(infos))
	seen := map[string]int{}
	for i, info := range infos {
		name := strings.TrimSpace(info.Name)
		if name == "" {
			name = "キーボード"
		}
		seen[name]++
		if seen[name] > 1 {
			name = fmt.Sprintf("%s (%d)", name, seen[name])
		}
		names[i] = name
	}
	return names
}

func (a *app) empty(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(t.Space(2)).Children(func() {
		icon := sfsymbols.Symbol(c, "keyboard").Size(t.Rem(2.4)).Color(t.TextMuted).Fallback(keyboardIcon)
		icon.Role(ui.RoleNone)
		ui.Text(c, "Vial のキーボードが見つかりません")
		ui.PrimaryButton(c, "更新").Disabled(a.busy).OnClick(a.refresh)
	})
}

func (a *app) unlockPane(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(t.Space(2)).Children(func() {
		a.unlockStatus(c)
	})
}

func (a *app) unlockStatus(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "キーを押し続けてください").Bold()
	ui.Text(c, a.holdText()).FontSize(t.Rem(1.5)).Bold()
	ui.Progress(c, a.unlockDone()).Label("解除の進行")
	ui.Text(c, fmt.Sprintf("残り %d。やめるにはキーボードを抜き差しします", a.remain)).TextColor(t.TextMuted)
}

func (a *app) unlockDone() float64 {
	const total = 50
	left := a.remain
	if left < 0 {
		left = 0
	}
	if left > total {
		left = total
	}
	return float64(total-left) / float64(total)
}

func (a *app) layers(c *ui.Context) {
	if a.kb == nil || a.kb.Layers < 1 {
		return
	}
	if a.layer < 0 || a.layer >= a.kb.Layers {
		a.layer = 0
	}
	labels := make([]string, a.kb.Layers)
	for i := range labels {
		labels[i] = strconv.Itoa(i)
	}
	ui.Segmented(c, &a.layer, labels...).Label("レイヤー")
}

// workspace is the board over the whole window, with the panel for the
// chosen key floating under it. The board fits the room the panel leaves.
func (a *app) workspace(c *ui.Context) {
	ui.Column(c).Grow(1).Children(func() {
		a.layoutOptions(c)
		a.board(c)
		a.panel(c)
	})
}

func (a *app) layoutOptions(c *ui.Context) {
	opts := a.kb.Layouts()
	if len(opts) == 0 {
		return
	}
	if a.layoutKB != a.kb || len(a.layout) != len(opts) {
		a.layout = a.kb.Choices()
		a.layoutKB = a.kb
	}
	t := c.Theme()
	ui.Row(c).Padding(0, t.Space(4), t.Space(2), t.Space(4)).Gap(t.Space(3)).Justify(ui.Center).Children(func() {
		for i, opt := range opts {
			if len(opt.Choices) < 2 {
				continue
			}
			name := opt.Name
			if name == "" {
				name = "配列"
			}
			if !ui.Segmented(c, &a.layout[i], opt.Choices...).Label(name).Changed() {
				continue
			}
			if a.busy {
				a.layout = a.kb.Choices()
				continue
			}
			prev := a.kb.Choices()
			if !a.kb.SetChoice(i, a.layout[i]) {
				a.layout = prev
				continue
			}
			a.sel = -1
			a.saveLayout(prev)
		}
	})
}

func (a *app) placedKeys() []protocol.Key {
	if a.kb == nil {
		return nil
	}
	out := make([]protocol.Key, len(a.kb.Keys))
	for i, key := range a.kb.Keys {
		if !a.kb.Shows(key) {
			out[i].W = -1
			continue
		}
		out[i] = a.kb.Place(key)
	}
	return out
}

func (a *app) board(c *ui.Context) {
	if a.sel >= 0 && (a.sel >= len(a.kb.Keys) || !a.kb.Shows(a.kb.Keys[a.sel])) {
		a.sel = -1
	}
	keys := a.placedKeys()
	width, height := boardSize(keys)
	// pad is the inset around the keycaps, inside the painted canvas.
	const pad = float32(18)
	// Painted, not a grid of buttons: keycaps rotate and overlap,
	// which no collection widget lays out. MyGo v0.9.0.
	box := ui.Box(c).Grow(1).Focusable().Role(ui.RoleGroup).Label("キー配列")
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
				name = "キー"
			}
			m.Item(name).Disabled(true)
			m.Separator()
		}
		if m.Item("再読み込み").Disabled(a.busy).Chosen() {
			a.refresh()
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

func (a *app) moveSelection(dx, dy int) {
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

func (a *app) drawKey(c *ui.Context, p *ui.Painter, r ui.Rect, unit float32, i int, key protocol.Key) {
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
		// Two sizes, so labels line up across the board; a label that still
		// does not fit shrinks on its own as a last resort.
		size := unit * 0.3
		if utf8.RuneCountInString(main) > 3 {
			size *= 0.78
		}
		span := ui.Span{Text: main, Weight: 600, Size: size, Color: labelColor}
		tw, th := p.MeasureText(0, span)
		for span.Size > 7 && tw > w-unit*0.16 {
			span.Size--
			tw, th = p.MeasureText(0, span)
		}
		mainH := faceH
		if sub != "" {
			mainH = faceH * 0.7
			small := ui.Span{Text: sub, Weight: 500, Size: unit * 0.2, Color: t.TextMuted}
			sw, sh := p.MeasureText(0, small)
			p.RichText(x+(w-sw)/2, y+faceH-sh-unit*0.06, 0, small)
		}
		if main != "" {
			p.RichText(x+(w-tw)/2, y+(mainH-th)/2, 0, span)
		}
	})
}

func (a *app) holdText() string {
	if len(a.hold) == 0 {
		return "解除キー"
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
	return strings.Join(names, " と ")
}

func (a *app) holding(key protocol.Key) bool {
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

// panel is the floating pane for the chosen key: its slots and their
// choices, or why it cannot be assigned yet. It shows while unlocking too.
func (a *app) panel(c *ui.Context) {
	chosen := a.kb != nil && a.sel >= 0 && a.sel < len(a.kb.Keys)
	if !chosen && !a.unlocking {
		return
	}
	t := c.Theme()
	panel := ui.Row(c.Key("panel"))
	if _, ok := a.editable(); ok {
		// Fixed while editing, so switching slots does not move the board.
		panel.Height(t.Rem(15))
	}
	panel.Margin(0, t.Space(4), t.Space(4), t.Space(4)).Radius(t.Rem(1.25)).Material(glass.Glass{Clarity: 0.3}).AlignItems(ui.Stretch).Role(ui.RoleGroup).Label("割り当て").Children(func() {
		if a.unlocking {
			ui.Column(c).Grow(1).Center().Gap(t.Space(2)).Padding(t.Space(4)).Children(func() {
				a.unlockStatus(c)
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
				ui.Text(c, "このキーはマトリクスにありません").TextColor(t.TextMuted).Margin(t.Space(4))
			case !a.unlocked:
				ui.Column(c).Padding(t.Space(4)).Gap(t.Space(3)).Children(func() {
					a.lockNote(c)
					ui.PrimaryButton(c.Key("unlock"), "解除").Disabled(a.busy || a.dev == nil).OnClick(a.toggleLock).AlignSelf(ui.Start)
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
func (a *app) editable() (protocol.Key, bool) {
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
func (a *app) slotOf(key protocol.Key) string {
	if key.Encoder {
		return slotTap
	}
	return a.slot
}

func (a *app) slots(c *ui.Context, key protocol.Key) {
	if key.Encoder || !a.unlocked {
		return
	}
	tap, hold := a.tapHold()
	ui.Sidebar(c, &a.slot, func() {
		ui.SidebarItem(c, slotTap, nil, "タップ").Children(func() {
			ui.Badge(c, a.tapLabel(tap))
		})
		ui.SidebarItem(c, slotHold, nil, "長押し").Children(func() {
			ui.Badge(c, holdLabel(hold))
		})
	}).Label("動作").Background(ui.RGBA(0, 0, 0, 0))
}

// tapHold is the selected key's code read as a tap and a hold.
func (a *app) tapHold() (tap, hold uint16) {
	code := a.current()
	if code == 0xFFFF {
		return 0, 0
	}
	return protocol.SplitTapHold(code)
}

func (a *app) tapLabel(tap uint16) string {
	if tap == 0 {
		return "なし"
	}
	return buttonName(a.kb, tap)
}

func holdLabel(hold uint16) string {
	switch {
	case hold == 0:
		return "なし"
	case hold >= 0x5220 && hold <= 0x523F:
		return fmt.Sprintf("レイヤー %d", hold-0x5220)
	}
	mods := byte(hold >> 8)
	var b strings.Builder
	if mods&0x10 != 0 {
		b.WriteString("右")
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
func (a *app) holdPicker(c *ui.Context) {
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
		{name: "押している間のレイヤー", codes: layerHolds},
		{name: "修飾", codes: modHolds},
	}
	var note string
	if !open {
		note = "長押しを付けられるのは、修飾のない基本キーだけです"
	}
	choices(c, note, groups, func(code uint16) {
		a.choiceButton(c, fmt.Sprintf("hold-%04x", code), holdLabel(code), code == hold, code != 0 && !open, func() {
			a.assignTapHold(tap, code)
		})
	})
}

// choices scrolls groups of choice buttons under their headings, after a
// note when there is one.
func choices(c *ui.Context, note string, groups []keyGroup, button func(code uint16)) {
	t := c.Theme()
	ui.Scroll(c).Grow(1).Padding(t.Space(3), t.Space(4), t.Space(4), t.Space(4)).Gap(t.Space(2)).Children(func() {
		if note != "" {
			ui.Text(c, note).TextColor(t.TextMuted)
		}
		for _, group := range groups {
			if group.name != "" {
				ui.Text(c, group.name).FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
			}
			ui.Row(c).Wrap().Gap(t.Space(1)).Children(func() {
				for _, code := range group.codes {
					button(code)
				}
			})
		}
	})
}

// assignTapHold writes tap and hold as the one keycode that carries both.
func (a *app) assignTapHold(tap, hold uint16) {
	code, ok := protocol.JoinTapHold(tap, hold)
	if !ok {
		a.err = "長押しを付けられるのは、修飾のない基本キーだけです"
		return
	}
	a.assign(code)
}

func (a *app) keySummary(c *ui.Context, key protocol.Key) {
	t := c.Theme()
	code, _ := keyCode(a.kb, key, a.layer)
	name, _ := capLabels(a.kb, key, a.layer)
	if name == "" {
		name = "No"
	}
	ui.Text(c, name).FontSize(t.Rem(1.6)).Bold()
	if key.Encoder {
		turn := "反時計回り"
		if key.EncDir == 1 {
			turn = "時計回り"
		}
		ui.Text(c, fmt.Sprintf("ノブ %d · %s · %04X", key.EncoderIndex+1, turn, code)).FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
		return
	}
	ui.Text(c, fmt.Sprintf("レイヤー %d · %d,%d · %04X", a.layer, key.Row, key.Col, code)).FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
}

func (a *app) lockNote(c *ui.Context) {
	if a.unlocked || a.unlocking {
		return
	}
	ui.Text(c, "ロック中です。割り当てるには「解除」を押して、表示されたキーを押し続けてください").TextColor(c.Theme().TextMuted)
}

func (a *app) current() uint16 {
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
func (a *app) tapButton(c *ui.Context, code uint16) {
	tap, hold := a.tapHold()
	a.choiceButton(c, fmt.Sprintf("kc-%04x", code), buttonName(a.kb, code), code == tap && a.current() != 0xFFFF, hold != 0 && code > 0xFF, func() {
		a.assignTapHold(code, hold)
	})
}

func (a *app) choiceButton(c *ui.Context, key, label string, chosen, off bool, pick func()) {
	var b ui.Element
	if chosen {
		b = ui.PrimaryButton(c.Key(key), label)
	} else {
		b = ui.Button(c.Key(key), label)
	}
	// Choices keep their width in a wrapping row and line up as keycaps.
	b.Padding(c.Theme().Space(1), c.Theme().Space(2)).MinWidth(c.Theme().Rem(2.4)).Shrink(0).Disabled(off || a.busy || a.unlocking || !a.unlocked).OnClick(pick)
}

type keyGroup struct {
	name  string
	codes []uint16
}

type keyKind int

const (
	kindBasic keyKind = iota
	kindLayer
	kindMacro
	kindCustom
)

func (a *app) keyKinds() []keyKind {
	kinds := []keyKind{kindBasic, kindLayer}
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
	case kindLayer:
		return "レイヤー操作"
	case kindMacro:
		return "マクロ"
	case kindCustom:
		return "独自"
	default:
		return "基本"
	}
}

func (a *app) picker(c *ui.Context) {
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
	ui.Segmented(c, &a.pick, labels...).Label("キーの種類").AlignSelf(ui.Start).Margin(t.Space(4), t.Space(4), 0, t.Space(4))
	var groups []keyGroup
	switch kinds[a.pick] {
	case kindLayer:
		groups = layerPalette(a.kb)
	case kindMacro:
		groups = macroPalette(a.kb)
	case kindCustom:
		groups = customPalette(a.kb)
	default:
		groups = palette()
	}
	var note string
	switch _, hold := a.tapHold(); {
	case hold != 0 && kinds[a.pick] != kindBasic:
		note = "長押しがあるあいだは、基本キーだけを選べます"
	case kinds[a.pick] == kindMacro:
		note = "割り当てだけです。マクロの中身はまだ編集できません"
	}
	choices(c, note, groups, func(code uint16) { a.tapButton(c, code) })
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
		{"切り替え", 32, protocol.ToggleLayer},
		{"移動", 32, protocol.ToLayer},
		{"タップで切り替え", 32, protocol.TapToggleLayer},
		{"一度だけ", 32, protocol.OneShotLayer},
		{"既定", 32, protocol.DefaultLayer},
	}
	groups := make([]keyGroup, 0, len(ops))
	for _, item := range ops {
		groups = append(groups, keyGroup{name: item.name, codes: layerCodes(n, item.limit, item.make)})
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
	return []keyGroup{{name: "マクロ", codes: codes}}
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
	return []keyGroup{{name: "独自", codes: codes}}
}

func palette() []keyGroup {
	letters := make([]uint16, 26)
	for i := range letters {
		letters[i] = uint16(0x04 + i)
	}
	digits := make([]uint16, 10)
	for i := range digits {
		digits[i] = uint16(0x1E + i)
	}
	return []keyGroup{
		{name: "基本", codes: []uint16{0x0001, 0x0000, 0x0029, 0x002B, 0x002C, 0x0028, 0x002A, 0x004C}},
		{name: "矢印", codes: []uint16{0x0050, 0x0051, 0x0052, 0x004F}},
		{name: "修飾", codes: []uint16{0x00E0, 0x00E1, 0x00E2, 0x00E3}},
		{name: "文字", codes: letters},
		{name: "数字", codes: digits},
	}
}

// capName is how a keycode reads on a keycap and in the choices: the
// keyboard's own name for its custom keys, the Mac's symbol where a key has
// one, and QMK's short name otherwise. KC_NO reads as nothing.
func capName(kb *protocol.Keyboard, code uint16) string {
	if sym, ok := capSymbols[code]; ok {
		return sym
	}
	return kb.Label(code)
}

// buttonName is capName for a control, where KC_NO needs a name.
func buttonName(kb *protocol.Keyboard, code uint16) string {
	if name := capName(kb, code); name != "" {
		return name
	}
	return "No"
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
	0x00E4: "右⌃",
	0x00E5: "右⇧",
	0x00E6: "右⌥",
	0x00E7: "右⌘",
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
		return shortHold(hold), "押す間"
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

func (a *app) refresh() {
	if a.busy {
		return
	}
	a.busy = true
	a.err = ""
	a.gen++
	gen := a.gen
	entry := a.entry
	go func() {
		infos, err := hid.List()
		if err != nil {
			a.finish(gen, nil, nil, nil, protocol.Unlock{}, err, entry, false)
			return
		}
		if len(infos) == 0 {
			a.finish(gen, infos, nil, nil, protocol.Unlock{}, nil, 0, true)
			return
		}
		info := infos[0]
		for _, candidate := range infos {
			if candidate.Entry == entry {
				info = candidate
				break
			}
		}
		a.load(gen, infos, info)
	}()
}

func (a *app) open(info hid.Info) {
	if a.busy {
		return
	}
	a.busy = true
	a.err = ""
	a.gen++
	gen := a.gen
	go func() {
		a.load(gen, nil, info)
	}()
}

func (a *app) load(gen int, infos []hid.Info, info hid.Info) {
	dev, err := hid.Open(info)
	if err != nil {
		a.finish(gen, infos, nil, nil, protocol.Unlock{}, err, info.Entry, true)
		return
	}
	kb, err := protocol.Read(dev)
	lock, lockErr := protocol.Security(dev)
	if err != nil {
		if lockErr == nil && lock.Pending {
			a.finish(gen, infos, dev, nil, lock, nil, info.Entry, true)
			return
		}
		dev.Close()
		a.finish(gen, infos, nil, nil, protocol.Unlock{}, err, info.Entry, true)
		return
	}
	if lockErr != nil {
		dev.Close()
		a.finish(gen, infos, nil, nil, protocol.Unlock{}, lockErr, info.Entry, true)
		return
	}
	a.finish(gen, infos, dev, kb, lock, nil, info.Entry, true)
}

func (a *app) finish(gen int, infos []hid.Info, dev *hid.Device, kb *protocol.Keyboard, lock protocol.Unlock, err error, entry uint64, loaded bool) {
	apply := func() {
		if a.gen != gen {
			if dev != nil {
				dev.Close()
			}
			return
		}
		if infos != nil {
			a.infos = infos
		}
		if loaded {
			if a.dev != nil {
				a.dev.Close()
			}
			a.dev = dev
			a.kb = kb
			a.entry = entry
			a.layer = 0
			a.sel = -1
			a.unlocked = lock.Open
			a.hold = lock.Keys
			a.unlocking = lock.Pending
			a.remain = 50
		}
		a.busy = false
		if err != nil {
			a.err = explain(err)
			a.unlocking = false
			return
		}
		if a.unlocking {
			a.busy = true
			a.schedulePoll(gen)
		}
	}
	if a.win == nil {
		apply()
		return
	}
	a.win.Update(apply)
}

func (a *app) saveLayout(prev []int) {
	if a.dev == nil || a.kb == nil {
		return
	}
	a.busy = true
	a.err = ""
	a.gen++
	gen := a.gen
	dev := a.dev
	kb := a.kb
	bits := kb.LayoutBits()
	go func() {
		err := protocol.WriteLayoutOptions(dev, bits)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if err != nil && a.kb == kb {
				for i, choice := range prev {
					kb.SetChoice(i, choice)
				}
				a.layout = kb.Choices()
				a.err = explain(err)
			}
		})
	}()
}

func (a *app) assign(code uint16) {
	if a.busy || a.kb == nil || a.unlocking || a.sel < 0 || a.sel >= len(a.kb.Keys) {
		return
	}
	if !a.unlocked {
		a.err = "ロック中です。解除してから割り当ててください"
		return
	}
	if a.dev == nil {
		return
	}
	key := a.kb.Keys[a.sel]
	if key.Encoder {
		a.assignEncoder(key, code)
		return
	}
	if !key.HasMatrix {
		return
	}
	a.busy = true
	a.err = ""
	a.gen++
	gen := a.gen
	dev := a.dev
	kb := a.kb
	layer, row, col := a.layer, key.Row, key.Col
	go func() {
		got, read, err := kb.WriteKey(dev, layer, row, col, code)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if read && a.kb == kb {
				a.kb.Store(layer, row, col, got)
			}
			if err != nil {
				a.err = explain(err)
			}
		})
	}()
}

func (a *app) assignEncoder(key protocol.Key, code uint16) {
	a.busy = true
	a.err = ""
	a.gen++
	gen := a.gen
	dev := a.dev
	kb := a.kb
	layer, index, dir := a.layer, key.EncoderIndex, key.EncDir
	go func() {
		got, read, err := kb.WriteEncoder(dev, layer, index, dir, code)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if read && a.kb == kb {
				a.kb.StoreEncoder(layer, index, dir, got)
			}
			if err != nil {
				a.err = explain(err)
			}
		})
	}()
}

func (a *app) beginUnlock() {
	if a.busy || a.dev == nil || a.unlocked {
		return
	}
	a.busy = true
	a.err = ""
	a.gen++
	gen := a.gen
	dev := a.dev
	go func() {
		err := protocol.BeginUnlock(dev)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen {
				return
			}
			if err != nil {
				a.busy = false
				a.err = explain(err)
				return
			}
			a.unlocking = true
			a.remain = 50
			a.schedulePoll(gen)
		})
	}()
}

func (a *app) lock() {
	if a.busy || a.dev == nil || !a.unlocked {
		return
	}
	a.busy = true
	a.err = ""
	a.gen++
	gen := a.gen
	dev := a.dev
	go func() {
		err := protocol.LockKeyboard(dev)
		// The keyboard can lock and still drop the reply. The status
		// read is what refresh uses, so a locked status wins.
		status, statusErr := protocol.Security(dev)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			a.finishLock(err, status, statusErr)
		})
	}()
}

// finishLock applies a lock attempt. A locked status wins even when the
// lock command itself reported an error.
func (a *app) finishLock(cmdErr error, status protocol.Unlock, statusErr error) {
	if statusErr == nil {
		a.unlocked = status.Open
		a.unlocking = status.Pending
		if len(status.Keys) > 0 {
			a.hold = status.Keys
		}
		if !status.Open && !status.Pending {
			a.err = ""
			a.flash = "ロックしました"
			return
		}
		if cmdErr != nil {
			a.err = explain(cmdErr)
			return
		}
		a.err = "ロックできませんでした"
		return
	}
	if cmdErr != nil {
		a.err = explain(cmdErr)
		return
	}
	a.unlocked = false
	a.unlocking = false
	a.err = ""
	a.flash = "ロックしました"
}

func (a *app) schedulePoll(gen int) {
	dev := a.dev
	go func() {
		time.Sleep(200 * time.Millisecond)
		open, left, err := protocol.PollUnlock(dev)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen || !a.unlocking {
				return
			}
			if err != nil {
				a.err = explain(err)
				a.busy = false
				a.unlocking = false
				return
			}
			a.remain = left
			if !open {
				a.schedulePoll(gen)
				return
			}
			a.unlocked = true
			a.unlocking = false
			a.busy = false
			a.err = ""
			a.flash = "解除しました"
			if a.kb == nil {
				a.refresh()
			}
		})
	}()
}

func explain(err error) string {
	if err == nil {
		return ""
	}
	var kept *protocol.Kept
	if errors.As(err, &kept) {
		name := protocol.KeyName(kept.Got)
		if name == "" {
			name = "No"
		}
		return fmt.Sprintf("書き込めませんでした。キーボードは %s のままです", name)
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "did not answer"):
		return "キーボードが応答しませんでした"
	case strings.Contains(msg, "not connected"), strings.Contains(msg, "is closed"):
		return "キーボードが接続されていません"
	case strings.Contains(msg, "outside the keymap"):
		return "このキーの位置には書き込めません"
	case strings.Contains(msg, "write report"):
		return "キーボードへの送信に失敗しました"
	default:
		return msg
	}
}
