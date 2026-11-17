package view

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/halqme/govial/locales"
)

func (a *App) InstallMenus() {
	// Not a document app, so there is no File menu. View leaves out Reload:
	// that role reloads a page, and its shortcut would do nothing here.
	a.reloadItem = &mygo.MenuItem{
		ID:          "reload",
		Label:       locales.Reload(),
		Accelerator: "CmdOrCtrl+R",
		Click:       func(*mygo.MenuItem, *mygo.Window) { a.Refresh() },
	}
	a.lockItem = &mygo.MenuItem{
		ID:          "lock",
		Label:       a.lockCommand(),
		Accelerator: "CmdOrCtrl+Shift+U",
		Disabled:    true,
		Click:       func(*mygo.MenuItem, *mygo.Window) { a.toggleLock() },
	}
	a.macroItem = &mygo.MenuItem{
		ID:       "macros",
		Label:    locales.EditMacros(),
		Type:     mygo.MenuItemCheckbox,
		Disabled: true,
		Click:    func(*mygo.MenuItem, *mygo.Window) { a.toggleMacros() },
	}
	a.keyboardItem = &mygo.MenuItem{
		ID:      "keyboard",
		Label:   locales.Keyboard(),
		Submenu: []*mygo.MenuItem{a.reloadItem, mygo.Separator(), a.lockItem, mygo.Separator(), a.macroItem},
	}
	a.systemItem = &mygo.MenuItem{
		ID:    "lang-system",
		Label: locales.LanguageSystem(),
		Type:  mygo.MenuItemRadio,
		Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.SetLocale("") },
	}
	a.langItems = map[string]*mygo.MenuItem{}
	langItems := []*mygo.MenuItem{a.systemItem}
	for _, tag := range mygo.App.Languages() {
		lang := tag
		item := &mygo.MenuItem{
			ID:    "lang-" + lang,
			Label: i18n.Autonym(lang),
			Type:  mygo.MenuItemRadio,
			Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.SetLocale(lang) },
		}
		a.langItems[lang] = item
		langItems = append(langItems, item)
	}
	a.languageItem = &mygo.MenuItem{
		ID:      "language",
		Label:   locales.Language(),
		Submenu: langItems,
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Role: mygo.RoleEditMenu},
		a.keyboardItem,
		{Role: mygo.RoleViewMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleResetZoom},
			{Role: mygo.RoleZoomIn},
			{Role: mygo.RoleZoomOut},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu},
		a.languageItem,
		{Role: mygo.RoleHelp},
	}))
}

func (a *App) syncWindowTitle() {
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

func (a *App) SyncMenu() {
	if a.reloadItem == nil || a.lockItem == nil {
		return
	}
	a.setLabel(&a.reloadLabel, a.reloadItem, locales.Reload())
	a.setLabel(&a.keyboardLabel, a.keyboardItem, locales.Keyboard())
	a.setLabel(&a.languageLabel, a.languageItem, locales.Language())
	a.setLabel(&a.systemLabel, a.systemItem, locales.LanguageSystem())
	reloadOn := !a.busy
	if reloadOn != a.reloadOn {
		a.reloadOn = reloadOn
		a.reloadItem.SetEnabled(reloadOn)
	}
	a.setLabel(&a.lockLabel, a.lockItem, a.lockCommand())
	lockOn := a.dev != nil && !a.busy
	if lockOn != a.lockOn {
		a.lockOn = lockOn
		a.lockItem.SetEnabled(lockOn)
	}
	if a.macroItem == nil {
		return
	}
	a.setLabel(&a.macroLabel, a.macroItem, locales.EditMacros())
	macroOn := a.dev != nil && a.kb != nil && a.kb.Macros > 0 && !a.busy && !a.unlocking
	if macroOn != a.macroEnabled {
		a.macroEnabled = macroOn
		a.macroItem.SetEnabled(macroOn)
	}
	if a.macrosOn != a.macroChecked {
		a.macroChecked = a.macrosOn
		a.macroItem.SetChecked(a.macrosOn)
	}
}

func (a *App) setLabel(cached *string, item *mygo.MenuItem, label string) {
	if item == nil || label == *cached {
		return
	}
	*cached = label
	item.SetLabel(label)
}

// SyncLanguage checks the language the user chose. The names of the
// languages are their own, so only the menu's own words follow the language
// the app shows.
func (a *App) SyncLanguage() {
	if a.systemItem == nil {
		return
	}
	chosen := mygo.App.ChosenLocale()
	if a.chosenKnown && chosen == a.chosen {
		return
	}
	a.chosenKnown = true
	a.chosen = chosen
	a.systemItem.SetChecked(chosen == "")
	for tag, item := range a.langItems {
		item.SetChecked(chosen == tag)
	}
}
