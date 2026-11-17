package view

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/halqme/govial/locales"
	"github.com/halqme/govial/protocol"
)

// The macro screen is the key panel: the same glass, the same left column,
// and the same insets. A macro's steps are keycaps in a choice row. A key
// step is changed on the basic board, not in a menu of names. An empty macro
// uses that same place, so choosing another slot does not move the screen.

func (a *App) toggleMacros() {
	if a.macrosOn {
		a.closeMacros()
		return
	}
	a.openMacros()
}

func (a *App) closeMacros() {
	a.macrosOn = false
	a.macros = nil
	a.macroMem = 0
	a.macroAct = -1
	a.macroSlotID = ""
	a.macroList = ui.ListState{}
}

func (a *App) openMacros() {
	if a.busy || a.dev == nil || a.kb == nil || a.kb.Macros == 0 || a.unlocking {
		return
	}
	slot := 0
	if code := a.current(); code >= 0x7700 && code <= 0x777F {
		slot = int(code - 0x7700)
	}
	a.busy = true
	a.clearFault()
	a.gen++
	gen := a.gen
	dev := a.dev
	vial := a.kb.VialProtocol
	go func() {
		macros, memory, err := protocol.LoadMacros(dev, vial)
		apply := func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if err != nil {
				a.setFault(explain(err))
				return
			}
			a.macros = flattenMacros(macros)
			a.macroMem = memory
			if slot < 0 || slot >= len(a.macros) {
				slot = 0
			}
			a.macroSlotID = strconv.Itoa(slot)
			a.macroAct = -1
			a.macroList = ui.ListState{}
			a.macrosOn = true
		}
		if a.win == nil {
			apply()
			return
		}
		a.win.Update(apply)
	}()
}

// macroLead sits on the macro page, above the keys that assign a macro, and
// opens the editor. The toolbar has the same command.
func (a *App) macroLead(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Margin(t.Space(3), t.Space(4), 0, t.Space(4)).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.MacroAssignHint()).TextColor(t.TextMuted)
		ui.Button(c.Key("edit-macros"), locales.EditMacros()).Disabled(a.busy || a.unlocking || a.dev == nil).OnClick(a.openMacros)
	})
}

func (a *App) macroScreen(c *ui.Context) {
	t := c.Theme()
	if a.macroSlotID == "" {
		a.macroSlotID = "0"
	}
	before := a.macroSlotID
	// The key panel's pane, filling the window instead of the band under the board.
	ui.Row(c).Grow(1).Margin(0, t.Space(4), t.Space(4), t.Space(4)).Radius(t.Rem(1.25)).Material(glass.Glass{Clarity: 0.3}).Border(1, t.Border).AlignItems(ui.Stretch).Role(ui.RoleGroup).Label(locales.EditMacros()).Children(func() {
		ui.Column(c).Width(t.Rem(13)).Padding(t.Space(4)).Children(func() {
			ui.Sidebar(c, &a.macroSlotID, func() {
				for i := range a.macros {
					n := i
					code, _ := protocol.MacroKey(n)
					item := ui.SidebarItem(c, strconv.Itoa(n), nil, protocol.KeyName(code))
					if len(a.macros[n]) == 0 && a.macroSlotID != strconv.Itoa(n) {
						item.TextColor(t.TextMuted)
					}
				}
			}).Label(locales.KindMacro()).Grow(1).Background(ui.RGBA(0, 0, 0, 0))
		})
		ui.Column(c).Grow(1).Children(func() {
			// The slots are built first, so a new one is known before its steps.
			if a.macroSlotID != before {
				a.macroAct = -1
				a.macroList = ui.ListState{}
			}
			a.macroBody(c)
		})
	})
}

func (a *App) macroTitle() string {
	n, err := strconv.Atoi(a.macroSlotID)
	if err != nil {
		return "M0"
	}
	code, ok := protocol.MacroKey(n)
	if !ok {
		return "M0"
	}
	return protocol.KeyName(code)
}

func (a *App) slotPreview() string {
	steps := a.steps()
	if steps == nil || len(*steps) == 0 {
		return ""
	}
	if len(*steps) == 1 {
		return stepValue(a.kb, (*steps)[0])
	}
	return locales.MacroStepCount(len(*steps))
}

// macroBody is the panel's right column. The name and the step count sit with
// the buttons, and the steps start below them. The list is only the left column.
func (a *App) macroBody(c *ui.Context) {
	t := c.Theme()
	edit := a.canEditMacros()
	ui.Column(c).Grow(1).Padding(t.Space(4)).Gap(t.Space(3)).Children(func() {
		ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(t.Space(1)).Children(func() {
				ui.Text(c, a.macroTitle()).FontSize(t.Rem(1.6)).Bold()
				// The second line stays when the macro is empty, so this row does not jump.
				preview := a.slotPreview()
				if preview == "" {
					preview = "\u00a0"
				}
				ui.Text(c, preview).FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
			})
			ui.Spacer(c)
			a.addMenu(c, edit)
			ui.Button(c.Key("macro-remove"), locales.MacroRemove()).Disabled(!edit || !a.stepChosen()).OnClick(a.removeStep)
			ui.PrimaryButton(c.Key("macro-save"), locales.MacroSave()).Disabled(!edit || a.dev == nil).OnClick(a.saveMacros)
		})
		steps := a.steps()
		ui.Scroll(c).Grow(1).Gap(t.Space(2)).Children(func() {
			if steps == nil || len(*steps) == 0 {
				ui.Text(c, locales.MacroEmpty()).TextColor(t.TextMuted)
				return
			}
			a.stepSequence(c, *steps, edit)
			if a.stepChosen() {
				a.stepEditor(c, &(*steps)[a.macroAct], edit)
			}
		})
	})
}

// stepSequence is the macro as a row of keycaps, headed like a group of choices.
func (a *App) stepSequence(c *ui.Context, steps protocol.Macro, edit bool) {
	ui.Row(c).Wrap().Gap(pickSpacing(c)).Children(func() {
		for i := range steps {
			a.stepCap(c, i, edit)
		}
	})
}

func (a *App) stepCap(c *ui.Context, i int, edit bool) {
	t := c.Theme()
	act := (*a.steps())[i]
	ui.Column(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, stepKind(act.Kind)).FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
		face := stepValue(a.kb, act)
		tip := a.stepLabel(i)
		short := ""
		if tip != face {
			short = face
		}
		a.choiceButton(c, fmt.Sprintf("step-%d", i), tip, short, i == a.macroAct, !edit, func() {
			a.macroAct = i
		})
	})
}

func (a *App) stepEditor(c *ui.Context, act *protocol.Action, edit bool) {
	t := c.Theme()
	switch act.Kind {
	case protocol.ActionText:
		ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Width(t.Rem(5.5)).Height(pickUnit(c)).Shrink(0).Justify(ui.Center).Children(func() {
				ui.Text(c, locales.MacroCharacters()).FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
			})
			text := act.Text
			if ui.TextInput(c, &text).Label(locales.MacroCharacters()).Grow(1).Disabled(!edit).Changed() {
				act.Text = text
			}
		})
	case protocol.ActionDelay:
		ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Width(t.Rem(5.5)).Height(pickUnit(c)).Shrink(0).Justify(ui.Center).Children(func() {
				ui.Text(c, locales.MacroMilliseconds()).FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
			})
			ms := float64(act.Delay)
			if ui.NumberInput(c, &ms, 0, float64(protocol.MaxMacroDelay), 1).Label(locales.MacroMilliseconds()).Disabled(!edit).Changed() {
				act.Delay = int(ms)
			}
		})
	default:
		if len(act.Keys) == 0 {
			return
		}
		ui.Column(c).Gap(t.Space(2)).Children(func() {
			if note := holdNote(act.Kind); note != "" {
				ui.Text(c, note).FontSize(t.Rem(0.85)).TextColor(t.TextMuted)
			}
			a.macroKeyBoard(c, act.Keys[0])
			a.stepKeyGroups(c, act.Keys[0])
		})
	}
}

// macroKeyBoard is the basic board, choosing the selected step's key instead
// of assigning the key on the layout.
func (a *App) macroKeyBoard(c *ui.Context, chosen uint16) {
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
					b := a.stepKeyCap(c, key.code, chosen, "").Width(width).MinWidth(width).Height(unit).Padding(0)
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

func (a *App) stepKeyGroups(c *ui.Context, chosen uint16) {
	t := c.Theme()
	groups := palette()
	if a.kb != nil && len(a.kb.Custom) > 0 {
		groups = append(groups, customPalette(a.kb)...)
	}
	for _, group := range groups {
		ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Width(t.Rem(5.5)).Height(pickUnit(c)).Shrink(0).Justify(ui.Center).Children(func() {
				ui.Text(c, group.name).FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
			})
			ui.Row(c).Grow(1).Wrap().Gap(t.Space(1)).Children(func() {
				for _, code := range group.codes {
					a.stepKeyCap(c, code, chosen, "")
				}
			})
		})
	}
}

func (a *App) stepKeyCap(c *ui.Context, code, chosen uint16, short string) ui.Element {
	off := a.kb != nil && a.kb.VialProtocol < 2 && code > 0xFF
	return a.choiceButton(c, fmt.Sprintf("mk-%04x", code), buttonName(a.kb, code), short, code == chosen, off, func() {
		a.setStepKey(code)
	})
}

func (a *App) setStepKey(code uint16) {
	if !a.canEditMacros() || !a.stepChosen() {
		return
	}
	steps := a.steps()
	(*steps)[a.macroAct].Keys = []uint16{code}
}

func (a *App) addMenu(c *ui.Context, edit bool) {
	ui.MenuButton(c, locales.MacroAdd(), func(m *ui.Menu) {
		if m.Item(locales.MacroText()).Chosen() {
			a.addStep(protocol.ActionText)
		}
		if m.Item(locales.MacroTap()).Chosen() {
			a.addStep(protocol.ActionTap)
		}
		if m.Item(locales.MacroDown()).Chosen() {
			a.addStep(protocol.ActionDown)
		}
		if m.Item(locales.MacroUp()).Chosen() {
			a.addStep(protocol.ActionUp)
		}
		if a.kb != nil && a.kb.VialProtocol >= 2 && m.Item(locales.MacroWait()).Chosen() {
			a.addStep(protocol.ActionDelay)
		}
	}).Disabled(!edit)
}

func (a *App) canEditMacros() bool {
	return a.macrosOn && a.unlocked && !a.busy && !a.unlocking
}

func (a *App) steps() *protocol.Macro {
	n, err := strconv.Atoi(a.macroSlotID)
	if err != nil || n < 0 || n >= len(a.macros) {
		return nil
	}
	return &a.macros[n]
}

func (a *App) stepChosen() bool {
	steps := a.steps()
	return steps != nil && a.macroAct >= 0 && a.macroAct < len(*steps)
}

func (a *App) stepLabel(i int) string {
	steps := a.steps()
	if steps == nil || i < 0 || i >= len(*steps) {
		return ""
	}
	act := (*steps)[i]
	switch act.Kind {
	case protocol.ActionText:
		if act.Text == "" {
			return locales.MacroBlank()
		}
		return act.Text
	case protocol.ActionTap:
		return keyList(a.kb, act.Keys)
	case protocol.ActionDown:
		return locales.StepPress(keyList(a.kb, act.Keys))
	case protocol.ActionUp:
		return locales.StepRelease(keyList(a.kb, act.Keys))
	case protocol.ActionDelay:
		return locales.StepWait(act.Delay)
	default:
		return ""
	}
}

func keyList(kb *protocol.Keyboard, keys []uint16) string {
	names := make([]string, len(keys))
	for i, code := range keys {
		names[i] = buttonName(kb, code)
	}
	return joinNames(names)
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return locales.MacroBlank()
	}
	out := names[0]
	for _, name := range names[1:] {
		out += " " + name
	}
	return out
}

func holdNote(kind protocol.ActionKind) string {
	switch kind {
	case protocol.ActionDown:
		return locales.MacroDownNote()
	case protocol.ActionUp:
		return locales.MacroUpNote()
	default:
		return ""
	}
}

func stepKind(kind protocol.ActionKind) string {
	switch kind {
	case protocol.ActionTap:
		return locales.MacroTap()
	case protocol.ActionDown:
		return locales.MacroDown()
	case protocol.ActionUp:
		return locales.MacroUp()
	case protocol.ActionDelay:
		return locales.MacroWait()
	default:
		return locales.MacroText()
	}
}

func stepValue(kb *protocol.Keyboard, act protocol.Action) string {
	switch act.Kind {
	case protocol.ActionText:
		if act.Text == "" {
			return locales.MacroBlank()
		}
		return act.Text
	case protocol.ActionDelay:
		return locales.StepWait(act.Delay)
	default:
		return keyList(kb, act.Keys)
	}
}

func (a *App) addStep(kind protocol.ActionKind) {
	if !a.canEditMacros() {
		return
	}
	steps := a.steps()
	if steps == nil {
		return
	}
	var act protocol.Action
	switch kind {
	case protocol.ActionText:
		act = protocol.Action{Kind: kind, Text: "a"}
	case protocol.ActionDelay:
		act = protocol.Action{Kind: kind, Delay: 100}
	default:
		act = protocol.Action{Kind: kind, Keys: []uint16{0x04}}
	}
	*steps = append(*steps, act)
	a.macroAct = len(*steps) - 1
}

func (a *App) removeStep() {
	if !a.canEditMacros() || !a.stepChosen() {
		return
	}
	steps := a.steps()
	i := a.macroAct
	*steps = append((*steps)[:i], (*steps)[i+1:]...)
	if a.macroAct >= len(*steps) {
		a.macroAct = len(*steps) - 1
	}
}

func (a *App) reorderSteps(rows []int, to int) {
	if !a.canEditMacros() || len(rows) != 1 {
		return
	}
	steps := a.steps()
	if steps == nil {
		return
	}
	next, at := moveAction(*steps, rows[0], to)
	*steps = next
	a.macroAct = at
}

func moveAction(actions []protocol.Action, from, to int) ([]protocol.Action, int) {
	if from < 0 || from >= len(actions) || to < 0 || to > len(actions) || to == from || to == from+1 {
		return actions, from
	}
	item := actions[from]
	rest := append(append([]protocol.Action{}, actions[:from]...), actions[from+1:]...)
	if to > from {
		to--
	}
	out := make([]protocol.Action, 0, len(actions))
	out = append(out, rest[:to]...)
	out = append(out, item)
	out = append(out, rest[to:]...)
	return out, to
}

func (a *App) saveMacros() {
	if a.busy || !a.macrosOn || a.kb == nil {
		return
	}
	if !a.unlocked || a.unlocking {
		a.setFault(fault{kind: faultLockedAssign})
		return
	}
	if problem := draftProblem(a.macros, a.kb.VialProtocol >= 2); problem.kind != faultNone {
		a.setFault(problem)
		return
	}
	if a.dev == nil {
		return
	}
	a.busy = true
	a.clearFault()
	a.gen++
	gen := a.gen
	dev := a.dev
	vial := a.kb.VialProtocol
	macros := cloneMacros(a.macros)
	go func() {
		err := protocol.SaveMacros(dev, vial, macros)
		var got []protocol.Macro
		var memory int
		if err == nil {
			got, memory, err = protocol.LoadMacros(dev, vial)
		}
		apply := func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if err != nil {
				a.setFault(explain(err))
				return
			}
			a.macros = flattenMacros(got)
			a.macroMem = memory
			if n, conv := strconv.Atoi(a.macroSlotID); conv != nil || n < 0 || n >= len(a.macros) {
				a.macroSlotID = "0"
			}
			if steps := a.steps(); steps == nil || a.macroAct >= len(*steps) {
				a.macroAct = -1
			}
			a.flash = locales.FlashMacro()
		}
		if a.win == nil {
			apply()
			return
		}
		a.win.Update(apply)
	}()
}

func draftProblem(macros []protocol.Macro, advanced bool) fault {
	for _, macro := range macros {
		for _, act := range macro {
			switch act.Kind {
			case protocol.ActionText:
				if !macroTextOK(act.Text) {
					return fault{kind: faultMacroText}
				}
			case protocol.ActionDelay:
				if !advanced || act.Delay < 0 || act.Delay > protocol.MaxMacroDelay {
					return fault{kind: faultMacroDelay}
				}
			case protocol.ActionTap, protocol.ActionDown, protocol.ActionUp:
				if len(act.Keys) == 0 {
					return fault{kind: faultMacroWide}
				}
				for _, code := range act.Keys {
					if !advanced && code > 0xFF {
						return fault{kind: faultMacroWide}
					}
				}
			default:
				return fault{kind: faultMacroText}
			}
		}
	}
	return fault{}
}

func macroTextOK(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < 2 || r > 255 {
			return false
		}
	}
	return true
}

func flattenMacros(macros []protocol.Macro) []protocol.Macro {
	if macros == nil {
		return nil
	}
	out := make([]protocol.Macro, len(macros))
	for i, macro := range macros {
		for _, act := range macro {
			if len(act.Keys) <= 1 {
				out[i] = append(out[i], act)
				continue
			}
			for _, code := range act.Keys {
				out[i] = append(out[i], protocol.Action{Kind: act.Kind, Keys: []uint16{code}})
			}
		}
	}
	return out
}

func cloneMacros(macros []protocol.Macro) []protocol.Macro {
	out := make([]protocol.Macro, len(macros))
	for i, macro := range macros {
		out[i] = append(protocol.Macro(nil), macro...)
		for j := range out[i] {
			out[i][j].Keys = append([]uint16(nil), macro[j].Keys...)
		}
	}
	return out
}

func keyOptions(kb *protocol.Keyboard, current uint16) (names []string, codes map[string]uint16) {
	codes = map[string]uint16{}
	add := func(code uint16) {
		if kb != nil && kb.VialProtocol < 2 && code > 0xFF && code != current {
			return
		}
		name := buttonName(kb, code)
		if name == "" {
			name = fmt.Sprintf("%04X", code)
		}
		if _, ok := codes[name]; ok {
			return
		}
		codes[name] = code
		names = append(names, name)
	}
	add(current)
	for _, row := range basicRows {
		for _, key := range row {
			if key.code != room {
				add(key.code)
			}
		}
	}
	for _, group := range palette() {
		for _, code := range group.codes {
			add(code)
		}
	}
	if kb != nil {
		for i := range kb.Custom {
			add(uint16(0x7E00 + i))
		}
	}
	return names, codes
}
