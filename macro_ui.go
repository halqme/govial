package main

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/halqme/govial/locales"
	"github.com/halqme/govial/protocol"
)

// The macro screen is a Split: a Sidebar of the slots, each with a Badge of
// how many steps it has, and a List of the chosen slot's steps, which Reorder
// rearranges while the keyboard is unlocked. Adding a step is a MenuButton in
// a Toolbar; the chosen step is a Form on a surface. MyGo v0.9.0 has each of
// these. Leaving the screen is a button, not BackButton, because the window
// has no page history.

var stepsIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M4 6h16v2H4zm0 5h16v2H4zm0 5h10v2H4z"/></svg>`))

func (a *app) toggleMacros() {
	if a.macrosOn {
		a.closeMacros()
		return
	}
	a.openMacros()
}

func (a *app) closeMacros() {
	a.macrosOn = false
	a.macros = nil
	a.macroMem = 0
	a.macroAct = -1
	a.macroSlotID = ""
	a.macroList = ui.ListState{}
}

func (a *app) openMacros() {
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
func (a *app) macroLead(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Margin(t.Space(3), t.Space(4), 0, t.Space(4)).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.MacroAssignHint()).TextColor(t.TextMuted)
		ui.Button(c.Key("edit-macros"), locales.EditMacros()).Disabled(a.busy || a.unlocking || a.dev == nil).OnClick(a.openMacros)
	})
}

func (a *app) macroScreen(c *ui.Context) {
	t := c.Theme()
	if a.macroSplit == 0 {
		a.macroSplit = t.Rem(14)
	}
	if a.macroSlotID == "" {
		a.macroSlotID = "0"
	}
	before := a.macroSlotID
	ui.Column(c).Grow(1).Children(func() {
		a.macroHeader(c)
		ui.Split(c, &a.macroSplit, func() {
			ui.Sidebar(c, &a.macroSlotID, func() {
				for i := range a.macros {
					code, _ := protocol.MacroKey(i)
					ui.SidebarItem(c, strconv.Itoa(i), nil, protocol.KeyName(code)).Children(func() {
						ui.Badge(c, a.slotBadge(i))
					})
				}
			}).Label(locales.KindMacro()).Fill()
		}, func() {
			// The sidebar is built first, so a new slot is known before its steps.
			if a.macroSlotID != before {
				a.macroAct = -1
				a.macroList = ui.ListState{}
			}
			a.macroEditor(c)
		}).Grow(1).SplitReset(t.Rem(14))
	})
}

func (a *app) macroHeader(c *ui.Context) {
	t := c.Theme()
	name := "M0"
	if n, err := strconv.Atoi(a.macroSlotID); err == nil {
		if code, ok := protocol.MacroKey(n); ok {
			name = protocol.KeyName(code)
		}
	}
	ui.Row(c).Padding(t.Space(3), t.Space(4), t.Space(2), t.Space(4)).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
		ui.Button(c.Key("macro-close"), locales.MacroClose()).OnClick(a.closeMacros)
		ui.Text(c, name).FontSize(t.Rem(1.6)).Bold()
		if preview := a.slotPreview(); preview != "" {
			ui.Text(c, preview).FontSize(t.Rem(0.85)).TextColor(t.TextMuted).SingleLine()
		}
		ui.Spacer(c)
		ui.PrimaryButton(c.Key("macro-save"), locales.MacroSave()).Disabled(!a.canEditMacros() || a.dev == nil).OnClick(a.saveMacros)
	})
}

func (a *app) slotBadge(i int) string {
	if i < 0 || i >= len(a.macros) || len(a.macros[i]) == 0 {
		return locales.MacroBlank()
	}
	return strconv.Itoa(len(a.macros[i]))
}

func (a *app) slotPreview() string {
	steps := a.steps()
	if steps == nil || len(*steps) == 0 {
		return ""
	}
	if len(*steps) == 1 {
		return stepValue(a.kb, (*steps)[0])
	}
	return locales.MacroStepCount(len(*steps))
}

func (a *app) macroEditor(c *ui.Context) {
	t := c.Theme()
	edit := a.canEditMacros()
	ui.Column(c).Fill().Padding(t.Space(2), t.Space(4), t.Space(4), t.Space(2)).Gap(t.Space(3)).Children(func() {
		if !a.unlocked {
			ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
				a.lockNote(c)
				ui.PrimaryButton(c.Key("macro-unlock"), locales.LockUnlock()).Disabled(a.busy || a.dev == nil).OnClick(a.toggleLock)
			})
		}
		steps := a.steps()
		if steps == nil || len(*steps) == 0 {
			a.macroEmpty(c, edit)
			return
		}
		ui.Toolbar(c, func() {
			a.addMenu(c, edit)
			ui.Button(c.Key("macro-remove"), locales.MacroRemove()).Disabled(!edit || !a.stepChosen()).OnClick(a.removeStep)
		}).Label(locales.MacroSteps())
		a.macroList.Selected = &a.macroAct
		a.macroList.Label = a.stepLabel
		if edit {
			a.macroList.Reorder = a.reorderSteps
		} else {
			a.macroList.Reorder = nil
		}
		ui.List(c, &a.macroList, len(*steps), func(i int) {
			act := (*steps)[i]
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Badge(c, stepKind(act.Kind))
				ui.Text(c, stepValue(a.kb, act)).SingleLine()
			})
		}).Grow(1).Label(locales.MacroSteps())
		if a.stepChosen() {
			a.stepForm(c, &(*steps)[a.macroAct], edit)
		}
	})
}

func (a *app) macroEmpty(c *ui.Context, edit bool) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(t.Space(2)).Children(func() {
		icon := sfsymbols.Symbol(c, "list.bullet").Size(t.Rem(2.4)).Color(t.TextMuted).Fallback(stepsIcon)
		icon.Role(ui.RoleNone)
		ui.Text(c, locales.MacroEmpty()).TextColor(t.TextMuted)
		a.addMenu(c, edit)
	})
}

func (a *app) addMenu(c *ui.Context, edit bool) {
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

func (a *app) stepForm(c *ui.Context, act *protocol.Action, edit bool) {
	t := c.Theme()
	ui.Column(c).Padding(t.Space(3)).Gap(t.Space(2)).Background(t.Surface).Radius(t.Radius).Border(1, t.Border).Children(func() {
		ui.Text(c, stepKind(act.Kind)).FontSize(t.Rem(0.8)).TextColor(t.TextMuted)
		switch act.Kind {
		case protocol.ActionTap, protocol.ActionDown, protocol.ActionUp:
			if len(act.Keys) > 0 {
				ui.Text(c, buttonName(a.kb, act.Keys[0])).FontSize(t.Rem(1.6)).Bold()
			}
		case protocol.ActionDelay:
			ui.Text(c, locales.StepWait(act.Delay)).FontSize(t.Rem(1.6)).Bold()
		}
		ui.Form(c, func() {
			switch act.Kind {
			case protocol.ActionText:
				ui.Field(c, locales.MacroCharacters(), func() {
					text := act.Text
					if ui.TextArea(c, &text).MinHeight(t.Rem(4)).Disabled(!edit).Changed() {
						act.Text = text
					}
				})
			case protocol.ActionDelay:
				ui.Field(c, locales.MacroMilliseconds(), func() {
					ms := float64(act.Delay)
					if ui.NumberInput(c, &ms, 0, float64(protocol.MaxMacroDelay), 1).Disabled(!edit).Changed() {
						act.Delay = int(ms)
					}
				})
			default:
				if len(act.Keys) == 0 {
					return
				}
				ui.Field(c, locales.MacroKey(), func() {
					names, codes := keyOptions(a.kb, act.Keys[0])
					choice := buttonName(a.kb, act.Keys[0])
					if ui.Combobox(c, &choice, names).Disabled(!edit).Changed() {
						if code, ok := codes[choice]; ok {
							act.Keys = []uint16{code}
						}
					}
				})
			}
		})
	})
}

func (a *app) canEditMacros() bool {
	return a.macrosOn && a.unlocked && !a.busy && !a.unlocking
}

func (a *app) steps() *protocol.Macro {
	n, err := strconv.Atoi(a.macroSlotID)
	if err != nil || n < 0 || n >= len(a.macros) {
		return nil
	}
	return &a.macros[n]
}

func (a *app) stepChosen() bool {
	steps := a.steps()
	return steps != nil && a.macroAct >= 0 && a.macroAct < len(*steps)
}

func (a *app) stepLabel(i int) string {
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

func (a *app) addStep(kind protocol.ActionKind) {
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

func (a *app) removeStep() {
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

func (a *app) reorderSteps(rows []int, to int) {
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

func (a *app) saveMacros() {
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
