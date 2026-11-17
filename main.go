// Command govial shows and edits the keymap of a connected Vial keyboard.
// Copyright (C) 2026 HAL. SPDX-License-Identifier: GPL-2.0-or-later
// Run it with `go tool mygo dev` or `go run .`.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/halqme/govial/view"
)

func main() {
	a := view.New()
	a.InstallMenus()
	// Menu labels are set again from the view. The check marks name the
	// language the app chose, which is only known on the main thread, so
	// they are set once the app is ready and again when the language changes.
	mygo.App.OnLocaleChanged(func(string) {
		a.SyncMenu()
		a.SyncLanguage()
	})
	mygo.App.WhenReady(func() {
		a.SyncLanguage()
		a.SetWindow(mygo.NewWindow(mygo.WindowOptions{
			Title:         "Govial",
			TitleBarStyle: mygo.TitleBarHiddenInset,
			Width:         1100,
			Height:        720,
			// A tenkeyless board (18.25u x 6.5u) fits at 1u = 44 DIPs, with the
			// assignment pane below it.
			MinWidth:  840,
			MinHeight: 640,
			StateKey:  "main",
			Content:   ui.View(a.View),
		}))
		a.Refresh()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
