// Package view is the window. It draws the board, the assignment panel, and
// the macro editor, and keeps the keyboard connection those screens use.
package view

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/halqme/govial/hid"
	"github.com/halqme/govial/protocol"
)

// SetWindow is the window this app draws into.
func (a *App) SetWindow(w *mygo.Window) { a.win = w }

// keyUnit is the largest 1u keycap in DIPs, which a wide window reaches.
// It is fixed so the layout's proportions do not follow the text size.
const keyUnit = 72.0

// keyboardIcon is shown where the system has no SF Symbol.
var keyboardIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M3 7h18v10H3z"/><path d="M6 10h2v2H6zm4 0h2v2h-2zm4 0h2v2h-2zm4 0h2v2h-2zM7 14h10v2H7z"/></svg>`))

var (
	lockIcon   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M7 10V7a5 5 0 0 1 10 0v3h1v11H6V10zm2 0h6V7a3 3 0 0 0-6 0z"/></svg>`))
	unlockIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M9 10V7a3 3 0 0 1 6 0h2a5 5 0 0 0-10 0v3H6v11h12V10z"/></svg>`))
	reloadIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M12 5V2l4 4-4 4V7a5 5 0 1 0 5 5h2a7 7 0 1 1-7-7z"/></svg>`))
)

type App struct {
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
	unit          float32
	inset         ui.Point
	pick          int
	slot          string
	macrosOn      bool
	macroSlotID   string
	macroAct      int
	macros        []protocol.Macro
	macroMem      int
	macroList     ui.ListState
	macroItem     *mygo.MenuItem
	macroLabel    string
	macroEnabled  bool
	macroChecked  bool
	layout        []int
	layoutKB      *protocol.Keyboard
	reloadItem    *mygo.MenuItem
	lockItem      *mygo.MenuItem
	keyboardItem  *mygo.MenuItem
	languageItem  *mygo.MenuItem
	systemItem    *mygo.MenuItem
	langItems     map[string]*mygo.MenuItem
	reloadOn      bool
	lockOn        bool
	lockLabel     string
	reloadLabel   string
	keyboardLabel string
	languageLabel string
	systemLabel   string
	chosen        string
	chosenKnown   bool
	fault         fault
}

func New() *App {
	return &App{sel: -1, hot: -1, slot: slotTap, macroAct: -1}
}

func (a *App) View(c *ui.Context) {
	t := c.Theme()
	a.syncWindowTitle()
	a.SyncMenu()
	if a.flash != "" {
		c.Toast(a.flash)
		a.flash = ""
	}
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.toolbar(c)
		// MyGo v0.9.0 has no banner. This notice stays while the failure does.
		if msg := a.banner(); msg != "" {
			ui.Row(c).Margin(0, t.Space(4), t.Space(2), t.Space(4)).Padding(t.Space(2), t.Space(3)).Radius(t.Radius).Background(t.Danger.Mix(t.Background, 0.85)).Children(func() {
				ui.Text(c, msg).Role(ui.RoleAlert).TextColor(t.Danger)
			})
		}
		switch {
		case a.kb == nil && a.unlocking:
			a.unlockPane(c)
		case a.kb == nil && a.busy:
			a.searching(c)
		case a.kb == nil:
			a.empty(c)
		default:
			if a.macrosOn {
				a.macroScreen(c)
			} else {
				a.workspace(c)
			}
		}
	})
}
