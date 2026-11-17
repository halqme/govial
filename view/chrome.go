package view

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/halqme/govial/hid"
	"github.com/halqme/govial/locales"
	"github.com/halqme/govial/protocol"
)

func (a *App) lockCommand() string {
	switch {
	case a.unlocking:
		return locales.LockUnlocking()
	case a.unlocked:
		return locales.LockLock()
	default:
		return locales.LockUnlock()
	}
}

func (a *App) toggleLock() {
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
func (a *App) toolbar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Toolbar(c, func() {
		a.deviceSelect(c)
		ui.Spacer(c)
		if !a.macrosOn {
			a.layers(c)
		}
		ui.Spacer(c)
		// With no board, searching shows the wait in the middle instead.
		if a.busy && a.kb != nil {
			ui.Spinner(c).Label(locales.StatusBusy())
		}
		if a.kb != nil && a.kb.Macros > 0 {
			a.macroButton(c)
		}
		if a.dev != nil {
			a.lockButton(c)
		}
		iconButton(c, "reload", "arrow.clockwise", reloadIcon, locales.Reload()).Disabled(a.busy).OnClick(a.Refresh)
	}).Label(locales.ToolbarLabel()).Height(max(bar.Height, t.Rem(3.25))).Padding(0, bar.Right+t.Space(3), 0, bar.Left+t.Space(3)).DragWindow()
}

// macroButton opens the editor from the window, next to the lock. The
// menu has the same command; this is the one in sight.
func (a *App) macroButton(c *ui.Context) {
	label := locales.EditMacros()
	var b ui.Element
	if a.macrosOn {
		b = ui.PrimaryButton(c.Key("macro-mode"), label)
	} else {
		b = ui.Button(c.Key("macro-mode"), label)
	}
	// Closing needs no device. Opening does, so a missing keyboard only blocks the way in.
	b.Disabled(a.busy || a.unlocking || (a.dev == nil && !a.macrosOn)).OnClick(a.toggleMacros)
}

func (a *App) lockButton(c *ui.Context) {
	symbol, icon := "lock.fill", lockIcon
	if a.unlocked || a.unlocking {
		symbol, icon = "lock.open.fill", unlockIcon
	}
	iconButton(c, "lock", symbol, icon, a.lockCommand()).Disabled(a.busy).OnClick(a.toggleLock)
}

// iconButton is a title bar button that shows an SF Symbol, or fallback
// where the system has none, named by label.
func iconButton(c *ui.Context, key, symbol string, fallback *ui.SVG, label string) ui.Element {
	t := c.Theme()
	return ui.Button(c.Key(key), "").Children(func() {
		sfsymbols.Symbol(c, symbol).Size(t.Rem(1.05)).Color(t.Text).Fallback(fallback).Role(ui.RoleNone)
	}).Label(label).Tooltip(label)
}

func (a *App) deviceSelect(c *ui.Context) {
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
	if ui.Select(c, &a.device, names).Label(locales.Keyboard()).Changed() {
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
func (a *App) keyboardName() string {
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
			name = locales.Keyboard()
		}
		seen[name]++
		if seen[name] > 1 {
			name = locales.DeviceDuplicate(name, seen[name])
		}
		names[i] = name
	}
	return names
}

func (a *App) empty(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(t.Space(2)).Children(func() {
		icon := sfsymbols.Symbol(c, "keyboard").Size(t.Rem(2.4)).Color(t.TextMuted).Fallback(keyboardIcon)
		icon.Role(ui.RoleNone)
		ui.Text(c, locales.EmptyTitle())
		ui.PrimaryButton(c, locales.Refresh()).Disabled(a.busy).OnClick(a.Refresh)
	})
}

// searching stands in for the board while the keyboard is looked for and
// read, so a slow first read does not say there is no keyboard.
func (a *App) searching(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(t.Space(2)).Children(func() {
		ui.Spinner(c).Label(locales.Loading())
		ui.Text(c, locales.Loading()).TextColor(t.TextMuted)
	})
}

func (a *App) unlockPane(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(t.Space(2)).Children(func() {
		a.unlockStatus(c)
	})
}

func (a *App) unlockStatus(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, locales.UnlockHold()).Bold()
	ui.Text(c, a.holdText()).FontSize(t.Rem(1.5)).Bold()
	ui.Progress(c, a.unlockDone()).Label(locales.UnlockProgress())
	ui.Text(c, locales.UnlockRemain(a.remain)).TextColor(t.TextMuted)
}

func (a *App) unlockDone() float64 {
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

func (a *App) layers(c *ui.Context) {
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
	ui.Segmented(c, &a.layer, labels...).Label(locales.GroupLayer())
}

// workspace is the board over the whole window, with the panel for the
// chosen key floating under it. The board fits the room the panel leaves.
func (a *App) workspace(c *ui.Context) {
	ui.Column(c).Grow(1).Children(func() {
		a.layoutOptions(c)
		a.board(c)
		a.panel(c)
	})
}

func (a *App) layoutOptions(c *ui.Context) {
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
				name = locales.LayoutOption()
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

func (a *App) placedKeys() []protocol.Key {
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
