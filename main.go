// Command govial shows and edits the keymap of a connected Vial keyboard.
// Copyright (C) 2026 HAL. SPDX-License-Identifier: GPL-2.0-or-later
// Run it with `go tool mygo dev` or `go run .`.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	a := newApp()
	a.installMenus()
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:  "Govial",
			Width:  1100,
			Height: 720,
			// A tenkeyless board (18.25u x 6.5u) fits at 1u = 44 DIPs, with the
			// assignment pane below it.
			MinWidth:  840,
			MinHeight: 640,
			StateKey:  "main",
			Content:   ui.View(a.view),
		})
		a.refresh()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
