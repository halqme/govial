package main

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
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
	boardShare  float32
	layout      []int
	layoutKB    *protocol.Keyboard
	reloadItem  *mygo.MenuItem
	lockItem    *mygo.MenuItem
	reloadOn    bool
	lockOn      bool
	lockLabel   string
}

func newApp() *app {
	return &app{sel: -1, hot: -1, boardShare: 64}
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
		// MyGo v0.9.0 has no banner. This text stays while the failure does.
		if a.err != "" {
			ui.Text(c, a.err).Role(ui.RoleAlert).TextColor(t.Danger).Margin(t.Space(2), t.Space(4), 0, t.Space(4))
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
	if a.kb != nil && a.kb.Name != "" {
		title = a.kb.Name
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

func (a *app) toolbar(c *ui.Context) {
	ui.Toolbar(c, func() {
		if a.busy {
			ui.Spinner(c).Label("通信中")
		}
		ui.Button(c.Key("refresh"), "更新").Disabled(a.busy).OnClick(a.refresh)
		if a.dev != nil {
			a.lockButton(c)
		}
		ui.Spacer(c)
		a.deviceSelect(c)
	}).Label("操作")
}

func (a *app) lockButton(c *ui.Context) {
	var b ui.Element
	if !a.unlocked && !a.unlocking {
		b = ui.PrimaryButton(c.Key("lock"), a.lockCommand())
	} else {
		b = ui.Button(c.Key("lock"), a.lockCommand())
	}
	b.Disabled(a.busy).OnClick(a.toggleLock)
}

func (a *app) deviceSelect(c *ui.Context) {
	if len(a.infos) < 2 {
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

func (a *app) workspace(c *ui.Context) {
	ui.SplitVerticalPercent(c, &a.boardShare, func() { a.boardPane(c) }, func() { a.inspector(c) }).Grow(1).SplitReset(64)
}

func (a *app) boardPane(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(t.Space(3), t.Space(4), t.Space(2), t.Space(4)).Justify(ui.Center).Children(func() {
			a.layers(c)
		})
		a.layoutOptions(c)
		a.board(c)
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
	ui.ScrollBoth(c).Grow(1).Children(func() {
		ui.Column(c).MinWidthPercent(100).MinHeightPercent(100).Center().Children(func() {
			// Painted, not a grid of buttons: keycaps rotate and overlap,
			// which no collection widget lays out. MyGo v0.9.0.
			box := ui.Box(c).Width(width + pad*2).Height(height + pad*2).Focusable().Label("キー配列")
			switch {
			case box.Shortcut(0, ui.KeyLeft):
				a.moveSelection(-1, 0)
			case box.Shortcut(0, ui.KeyRight):
				a.moveSelection(1, 0)
			case box.Shortcut(0, ui.KeyUp):
				a.moveSelection(0, -1)
			case box.Shortcut(0, ui.KeyDown):
				a.moveSelection(0, 1)
			}
			a.hot = -1
			if px, py, ok := box.PointerPosition(); ok {
				for i := len(keys) - 1; i >= 0; i-- {
					if keys[i].W < 0 {
						continue
					}
					if keys[i].Contains((float64(px)-float64(pad))/keyUnit, (float64(py)-float64(pad))/keyUnit) {
						a.hot = i
						break
					}
				}
			}
			if a.hot >= 0 && (box.Clicked() || box.RightClicked()) {
				a.sel = a.hot
			}
			box.ContextMenu(func(m *ui.Menu) {
				if a.sel >= 0 && a.sel < len(keys) {
					name := keyLabel(a.kb, keys[a.sel], a.layer)
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
				origin := r
				origin.X += pad
				origin.Y += pad
				for i, key := range keys {
					if key.W < 0 {
						continue
					}
					a.drawKey(c, p, origin, i, key)
				}
			})
		})
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

func (a *app) drawKey(c *ui.Context, p *ui.Painter, r ui.Rect, i int, key protocol.Key) {
	t := c.Theme()
	ox := r.X + float32(key.RotX)*keyUnit
	oy := r.Y + float32(key.RotY)*keyUnit
	p.Transform(ui.Rotation(float32(key.Rot)), ox, oy, func() {
		const gap = float32(3.5)
		const lip = float32(4)
		x := r.X + float32(key.X)*keyUnit + gap
		y := r.Y + float32(key.Y)*keyUnit + gap
		w := float32(key.W)*keyUnit - gap*2
		h := float32(key.H)*keyUnit - gap*2
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
		label := keyLabel(a.kb, key, a.layer)
		if label == "" {
			return
		}
		size := t.FontSize
		if size <= 0 {
			size = 13
		}
		span := ui.Span{Text: label, Weight: 600, Size: size, Color: labelColor}
		tw, th := p.MeasureText(0, span)
		limit := w - 10
		for size > 8 && tw > limit {
			size--
			span.Size = size
			tw, th = p.MeasureText(0, span)
		}
		p.RichText(x+(w-tw)/2, y+(faceH-th)/2, 0, span)
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
				name := a.kb.Label(code)
				if name == "" {
					name = "No"
				}
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

func (a *app) inspector(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c).Fill().Background(t.Surface).Padding(t.Space(4)).Gap(t.Space(3)).Children(func() {
		if a.unlocking {
			ui.Text(c, "解除").TextColor(t.TextMuted)
			a.unlockStatus(c)
			return
		}
		if a.sel < 0 || a.sel >= len(a.kb.Keys) {
			ui.Text(c, "キーを選ぶと割り当てが出ます").TextColor(t.TextMuted)
			a.lockNote(c)
			a.inspectorFoot(c)
			return
		}
		key := a.kb.Keys[a.sel]
		if !key.HasMatrix && !key.Encoder {
			ui.Text(c, "このキーはマトリクスにありません").TextColor(t.TextMuted)
			a.inspectorFoot(c)
			return
		}
		a.keySummary(c, key)
		if !a.unlocked {
			a.lockNote(c)
			a.inspectorFoot(c)
			return
		}
		ui.Divider(c)
		for _, group := range palette() {
			ui.Text(c, group.name).TextColor(t.TextMuted)
			ui.Row(c).Wrap().Gap(t.Space(1)).Children(func() {
				for _, code := range group.codes {
					a.codeButton(c, code)
				}
			})
		}
		a.inspectorFoot(c)
	})
}

func (a *app) keySummary(c *ui.Context, key protocol.Key) {
	t := c.Theme()
	code, _ := keyCode(a.kb, key, a.layer)
	name := a.kb.Label(code)
	if name == "" {
		name = "No"
	}
	ui.Text(c, fmt.Sprintf("レイヤー %d", a.layer)).TextColor(t.TextMuted)
	ui.Text(c, name).FontSize(t.Rem(1.6)).Bold()
	if key.Encoder {
		turn := "反時計回り"
		if key.EncDir == 1 {
			turn = "時計回り"
		}
		ui.Text(c, fmt.Sprintf("ノブ %d · %s · %04X", key.EncoderIndex+1, turn, code)).TextColor(t.TextMuted)
		return
	}
	ui.Text(c, fmt.Sprintf("%d,%d · %04X", key.Row, key.Col, code)).TextColor(t.TextMuted)
}

func (a *app) lockNote(c *ui.Context) {
	if a.unlocked || a.unlocking {
		return
	}
	ui.Text(c, "ロック中です。割り当てるには「解除」を押して、表示されたキーを押し続けてください").TextColor(c.Theme().TextMuted)
}

func (a *app) inspectorFoot(c *ui.Context) {
	if a.kb == nil {
		return
	}
	ui.Text(c, a.kb.UIDString()).FontSize(c.Theme().Rem(0.85)).TextColor(c.Theme().TextMuted)
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

func (a *app) codeButton(c *ui.Context, code uint16) {
	label := pickerLabel(code)
	key := fmt.Sprintf("kc-%04x", code)
	var b ui.Element
	if code == a.current() {
		b = ui.PrimaryButton(c.Key(key), label)
	} else {
		b = ui.Button(c.Key(key), label)
	}
	b.Padding(c.Theme().Space(1), c.Theme().Space(2)).Disabled(a.busy || a.unlocking || !a.unlocked).OnClick(func() { a.assign(code) })
}

type keyGroup struct {
	name  string
	codes []uint16
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
		{name: "修飾", codes: []uint16{0x0050, 0x0051, 0x0052, 0x004F, 0x00E0, 0x00E1, 0x00E2, 0x00E3}},
		{name: "文字", codes: letters},
		{name: "数字", codes: digits},
	}
}

func pickerLabel(code uint16) string {
	switch code {
	case 0x0000:
		return "No"
	case 0x0028:
		return "⏎"
	case 0x002A:
		return "⌫"
	case 0x002C:
		return "␣"
	case 0x004C:
		return "⌦"
	case 0x004F:
		return "→"
	case 0x0050:
		return "←"
	case 0x0051:
		return "↓"
	case 0x0052:
		return "↑"
	case 0x00E0:
		return "⌃"
	case 0x00E1:
		return "⇧"
	case 0x00E2:
		return "⌥"
	case 0x00E3:
		return "⌘"
	default:
		return protocol.KeyName(code)
	}
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

func keyLabel(kb *protocol.Keyboard, key protocol.Key, layer int) string {
	if key.Encoder {
		code, ok := kb.EncoderCode(layer, key.EncoderIndex, key.EncDir)
		name := "No"
		if ok {
			if label := kb.Label(code); label != "" {
				name = label
			}
		}
		turn := "↺"
		if key.EncDir == 1 {
			turn = "↻"
		}
		return turn + name
	}
	if !key.HasMatrix {
		if len(key.Labels) > 0 {
			return key.Labels[0]
		}
		return ""
	}
	code, ok := kb.Code(layer, key.Row, key.Col)
	if !ok {
		return ""
	}
	return kb.Label(code)
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
	return float32(w * keyUnit), float32(h * keyUnit)
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
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if err != nil {
				a.err = explain(err)
				return
			}
			a.unlocked = false
			a.flash = "ロックしました"
		})
	}()
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
