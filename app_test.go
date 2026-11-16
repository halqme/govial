package main

import (
	"errors"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/halqme/govial/hid"
	"github.com/halqme/govial/protocol"
)

func init() {
	// Catalogs follow the process language. Pin Japanese so these
	// assertions do not change with the machine that runs them.
	i18n.SetLocaleFunc(func() string { return "ja" })
}

func TestEmptyStateOffersRefresh(t *testing.T) {
	a := newApp()
	tt := ui.NewTester(a.view, 900, 640)
	if !tt.HasText("Vial のキーボードが見つかりません") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "更新"); !ok {
		t.Fatal("refresh button missing")
	}
	if _, ok := tt.AccessByRole(ui.RoleToolbar, "操作"); !ok {
		t.Fatal("toolbar missing")
	}
}

func TestConnectionErrorStaysOnScreen(t *testing.T) {
	a := newApp()
	a.err = "接続できません"
	tt := ui.NewTester(a.view, 900, 640)
	if _, ok := tt.AccessByRole(ui.RoleAlert, "接続できません"); !ok {
		t.Fatal("error is not an alert")
	}
}

func TestLayerSwitchAndArrowSelection(t *testing.T) {
	a := newApp()
	a.unlocked = true
	a.kb = &protocol.Keyboard{
		Name:   "Board",
		Layers: 2,
		Keys: []protocol.Key{
			{X: 0, Y: 0, W: 1, H: 1, HasMatrix: true, Row: 0, Col: 0},
			{X: 2, Y: 0, W: 1, H: 1, HasMatrix: true, Row: 0, Col: 1},
		},
	}
	tt := ui.NewTester(a.view, 1000, 700)
	if err := tt.Click("1"); err != nil {
		t.Fatal(err)
	}
	if a.layer != 1 {
		t.Fatalf("layer %d", a.layer)
	}
	if !tt.HasText("キーを選んでください") {
		t.Fatalf("no hint with no key chosen: %q", tt.Texts())
	}
	board, ok := tt.AccessByRole(ui.RoleGroup, "キー配列")
	if !ok {
		t.Fatalf("board missing: %+v", tt.AccessAll())
	}
	tt.ClickAt(board.Bounds.X+2, board.Bounds.Y+2)
	tt.Key(0, ui.KeyRight)
	if a.sel != 0 {
		t.Fatalf("first arrow selected %d", a.sel)
	}
	tt.Key(0, ui.KeyRight)
	if a.sel != 1 {
		t.Fatalf("second arrow selected %d", a.sel)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "A"); !ok {
		t.Fatal("keycode palette missing A")
	}
	// The panel holds its room whether or not a key is chosen.
	if chosen, _ := tt.AccessByRole(ui.RoleGroup, "キー配列"); chosen.Bounds != board.Bounds {
		t.Fatalf("choosing a key moved the board from %+v to %+v", board.Bounds, chosen.Bounds)
	}
	tt.Key(0, ui.KeyEscape)
	if a.sel != -1 {
		t.Fatalf("escape left %d chosen", a.sel)
	}
	if !tt.HasText("キーを選んでください") {
		t.Fatal("hint missing after escape")
	}
}

func TestKnobAssignmentIsNotMatrix(t *testing.T) {
	a := newApp()
	a.unlocked = true
	a.kb = &protocol.Keyboard{
		Layers: 1,
		Keys: []protocol.Key{
			{X: 0, Y: 0, W: 1, H: 1, HasMatrix: true, Row: 0, Col: 1},
			{X: 8, Y: 1, W: 1, H: 1, Encoder: true, EncoderIndex: 0, EncDir: 1},
		},
	}
	a.sel = 1
	tt := ui.NewTester(a.view, 1000, 700)
	if tt.HasText("このキーはマトリクスにありません") {
		t.Fatal("knob is treated as a key with no assignment")
	}
	if !tt.HasText("ノブ 1 · 時計回り · 0000") {
		t.Fatalf("texts %q", tt.Texts())
	}
}

func TestLockedKeyboardHidesPalette(t *testing.T) {
	a := newApp()
	a.kb = &protocol.Keyboard{
		Layers: 1,
		Keys:   []protocol.Key{{X: 0, Y: 0, W: 1, H: 1, HasMatrix: true}},
	}
	a.sel = 0
	tt := ui.NewTester(a.view, 1000, 700)
	if !tt.HasText("ロック中") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "A"); ok {
		t.Fatal("palette is available while locked")
	}
}

func TestLayerAndMacroKeysAreOffered(t *testing.T) {
	a := newApp()
	a.unlocked = true
	a.kb = &protocol.Keyboard{
		Layers: 3,
		Macros: 2,
		Custom: []string{"Foo"},
		Keys:   []protocol.Key{{X: 0, Y: 0, W: 1, H: 1, HasMatrix: true}},
	}
	a.sel = 0
	tt := ui.NewTester(a.view, 1000, 700)
	if _, ok := tt.AccessByRole(ui.RoleButton, "MO(0)"); ok {
		t.Fatal("layer keys are mixed into the basic palette")
	}
	if err := tt.Click("レイヤー操作"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"TG(2)", "TO(0)", "TT(0)", "OSL(0)", "DF(0)"} {
		if _, ok := tt.AccessByRole(ui.RoleButton, label); !ok {
			t.Fatalf("%s missing", label)
		}
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "TG(3)"); ok {
		t.Fatal("layer key past the last layer")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "MO(0)"); ok {
		t.Fatal("MO is a hold, but the tap offers it")
	}
	if err := tt.Click("マクロ"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("まだ編集できません") {
		t.Fatal("macro note missing")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "M0"); !ok {
		t.Fatal("M0 missing")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "M1"); !ok {
		t.Fatal("M1 missing")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "M2"); ok {
		t.Fatal("macro past the count")
	}
	if err := tt.Click("独自"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Foo"); !ok {
		t.Fatal("custom key name missing")
	}
}

func TestHoldSlotOffersLayersAndModifiers(t *testing.T) {
	a := newApp()
	a.unlocked = true
	a.kb = &protocol.Keyboard{
		Layers: 20,
		Keys:   []protocol.Key{{X: 0, Y: 0, W: 1, H: 1, HasMatrix: true}},
	}
	a.sel = 0
	tt := ui.NewTester(a.view, 1000, 700)
	if _, ok := tt.AccessByRole(ui.RoleButton, "L1"); ok {
		t.Fatal("hold choices show while the tap slot is chosen")
	}
	if err := tt.Click("長押し"); err != nil {
		t.Fatal(err)
	}
	if a.slot != slotHold {
		t.Fatalf("slot %q", a.slot)
	}
	for _, label := range []string{"なし", "L0", "L15", "⌃", "右⌘", "⌃⇧⌥⌘"} {
		if _, ok := tt.AccessByRole(ui.RoleButton, label); !ok {
			t.Fatalf("%s missing: %q", label, tt.Texts())
		}
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "L16"); ok {
		t.Fatal("LT reaches layer 15, but layer 16 is offered")
	}
}

func TestKnobHasNoHoldSlot(t *testing.T) {
	a := newApp()
	a.unlocked = true
	a.slot = slotHold
	a.kb = &protocol.Keyboard{
		Layers: 1,
		Keys:   []protocol.Key{{X: 0, Y: 0, W: 1, H: 1, Encoder: true}},
	}
	a.sel = 0
	tt := ui.NewTester(a.view, 1000, 700)
	if tt.HasText("長押し") {
		t.Fatal("a knob offers a hold")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "A"); !ok {
		t.Fatal("a knob does not offer its keys")
	}
}

func TestLockButtonStartsLock(t *testing.T) {
	a := newApp()
	a.unlocked = true
	a.dev = &hid.Device{}
	a.kb = &protocol.Keyboard{Layers: 1, Keys: []protocol.Key{{X: 0, Y: 0, W: 1, H: 1, HasMatrix: true}}}
	tt := ui.NewTester(a.view, 1000, 700)
	if _, ok := tt.AccessByRole(ui.RoleButton, "ロック"); !ok {
		t.Fatalf("lock button missing: %q", tt.Texts())
	}
	if err := tt.Click("ロック"); err != nil {
		t.Fatal(err)
	}
	if !a.busy {
		t.Fatal("lock button did not start locking")
	}
}

func TestLockFollowsKeyboardWhenReplyIsMissed(t *testing.T) {
	a := newApp()
	a.unlocked = true
	a.finishLock(errors.New("keyboard did not answer"), protocol.Unlock{}, nil)
	if a.unlocked || a.unlocking || a.err != "" || a.flash != "ロックしました" {
		t.Fatalf("unlocked=%v unlocking=%v err=%q flash=%q", a.unlocked, a.unlocking, a.err, a.flash)
	}
}

func TestLockStaysOpenWhenKeyboardRefuses(t *testing.T) {
	a := newApp()
	a.unlocked = true
	a.finishLock(nil, protocol.Unlock{Open: true}, nil)
	if !a.unlocked || a.flash != "" || a.err != "ロックできませんでした" {
		t.Fatalf("unlocked=%v flash=%q err=%q", a.unlocked, a.flash, a.err)
	}
}

func TestLockCommandWithoutKeyboard(t *testing.T) {
	a := newApp()
	a.installMenus()
	a.syncMenu()
	if a.lockItem.IsEnabled() {
		t.Fatal("lock command enabled with no keyboard")
	}
	if a.lockLabel != "解除" {
		t.Fatalf("lock label %q", a.lockLabel)
	}
	if !a.reloadItem.IsEnabled() {
		t.Fatal("reload command disabled while idle")
	}
}

func TestDeviceNameWinsOverDefinition(t *testing.T) {
	a := newApp()
	a.kb = &protocol.Keyboard{Name: "HID Keyboard", Layers: 1}
	a.infos = []hid.Info{{Name: "Cornix", Entry: 7}}
	a.entry = 7
	if got := a.keyboardName(); got != "Cornix" {
		t.Fatalf("name %q", got)
	}
	a.infos = []hid.Info{{Name: " ", Entry: 7}}
	if got := a.keyboardName(); got != "HID Keyboard" {
		t.Fatalf("blank device name gave %q", got)
	}
}

func TestBasicKeysSitOnABoard(t *testing.T) {
	a := newApp()
	a.unlocked = true
	a.kb = &protocol.Keyboard{Layers: 1, Keys: []protocol.Key{{X: 0, Y: 0, W: 1, H: 1, HasMatrix: true}}}
	a.sel = 0
	tt := ui.NewTester(a.view, 1000, 700)
	if _, ok := tt.AccessByRole(ui.RoleGroup, "基本のキー"); !ok {
		t.Fatal("basic keys are not laid out as a board")
	}
	for _, label := range []string{"Esc", "F12", "A", "⌘", "右⌘", "Space", "→"} {
		if _, ok := tt.AccessByRole(ui.RoleButton, label); !ok {
			t.Fatalf("%s missing", label)
		}
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "英数"); ok {
		t.Fatal("keys off the board share its pane")
	}
	if err := tt.Click("その他"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"No", "▽", "英数", "かな", "Mute", "MB1", "PScr"} {
		if _, ok := tt.AccessByRole(ui.RoleButton, label); !ok {
			t.Fatalf("%s missing from その他", label)
		}
	}
}

func TestLoadingDoesNotSayMissing(t *testing.T) {
	a := newApp()
	a.busy = true
	tt := ui.NewTester(a.view, 900, 640)
	if tt.HasText("Vial のキーボードが見つかりません") {
		t.Fatal("reading the keyboard says there is none")
	}
	if !tt.HasText("読み込み中") {
		t.Fatalf("texts %q", tt.Texts())
	}
}

func TestCustomNamesReadOnOneLine(t *testing.T) {
	a := newApp()
	a.unlocked = true
	a.kb = &protocol.Keyboard{
		Layers: 1,
		Custom: []string{"Next\nBT"},
		Keys:   []protocol.Key{{X: 0, Y: 0, W: 1, H: 1, HasMatrix: true}},
	}
	a.sel = 0
	tt := ui.NewTester(a.view, 1000, 700)
	if err := tt.Click("独自"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Next BT"); !ok {
		t.Fatalf("custom key not on one line: %q", tt.Texts())
	}
}

func showLocale(t *testing.T, tag string) {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return tag })
	t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return "ja" }) })
}

func TestEnglishShowsTheSourceCatalog(t *testing.T) {
	showLocale(t, "en")
	a := newApp()
	tt := ui.NewTester(a.view, 900, 640)
	if !tt.HasText("No Vial keyboard found") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Refresh"); !ok {
		t.Fatal("refresh button missing")
	}
	if _, ok := tt.AccessByRole(ui.RoleToolbar, "Controls"); !ok {
		t.Fatal("toolbar missing")
	}
}

func TestFailureFollowsTheLanguage(t *testing.T) {
	a := newApp()
	a.unlocked = true
	a.finishLock(nil, protocol.Unlock{Open: true}, nil)
	if a.banner() != "ロックできませんでした" {
		t.Fatalf("ja %q", a.banner())
	}
	showLocale(t, "en")
	if a.banner() != "Could not lock" {
		t.Fatalf("en %q", a.banner())
	}
}

func TestLanguageMenuFollowsTheLanguage(t *testing.T) {
	a := newApp()
	a.installMenus()
	menu := mygo.App.Menu()
	if menu == nil {
		t.Fatal("menu missing")
	}
	label := func(id string) string {
		t.Helper()
		item := menu.ItemByID(id)
		if item == nil {
			t.Fatalf("%s missing", id)
		}
		return item.Label
	}
	if label("language") != "言語" || label("lang-system") != "システム" {
		t.Fatalf("language %q system %q", label("language"), label("lang-system"))
	}
	if label("lang-en") != "English" || label("lang-ja") != "日本語" {
		t.Fatalf("en %q ja %q", label("lang-en"), label("lang-ja"))
	}
	showLocale(t, "en")
	a.syncMenu()
	if label("language") != "Language" || label("lang-system") != "System" || label("keyboard") != "Keyboard" {
		t.Fatalf("language %q system %q keyboard %q", label("language"), label("lang-system"), label("keyboard"))
	}
}
