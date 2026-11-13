package protocol

import "testing"

func TestKeyName(t *testing.T) {
	cases := []struct {
		code uint16
		name string
	}{
		{0x0000, ""},
		{0x0001, "▽"},
		{0x0004, "A"},
		{0x004A, "Home"},
		{0x00E1, "LSft"},
		{0x0104, "C(A)"},        // LCTL(KC_A)
		{0x0700, "MEH"},         // MEH(KC_NO)
		{0x0F00, "HYPR"},        // HYPR(KC_NO)
		{0x1234, "RS(')"},       // RSFT(KC_QUOTE)
		{0x2129, "C/Esc"},       // LCTL_T(KC_ESCAPE)
		{0x422C, "LT(2,Space)"}, // LT(2, KC_SPACE)
		{0x5200, "TO(0)"},
		{0x5221, "MO(1)"},
		{0x5262, "TG(2)"},
		{0x52A2, "OSM(S)"},
		{0x52C1, "TT(1)"},
		{0x5703, "TD(3)"},
		{0x7701, "M1"},
		{0x7C00, "Boot"},
		{0x7E00, "USER00"},
		{0x6FFF, "6FFF"},
	}
	for _, tc := range cases {
		if got := KeyName(tc.code); got != tc.name {
			t.Errorf("%04X: got %q, want %q", tc.code, got, tc.name)
		}
	}
}

func TestLayerAndMacroEncodings(t *testing.T) {
	if got, ok := MomentaryLayer(1); !ok || got != 0x5221 || KeyName(got) != "MO(1)" {
		t.Fatalf("MO %04X %v %q", got, ok, KeyName(got))
	}
	if _, ok := MomentaryLayer(-1); ok {
		t.Fatal("layer -1")
	}
	if _, ok := MomentaryLayer(32); ok {
		t.Fatal("layer 32")
	}
	if got, ok := ToggleLayer(2); !ok || got != 0x5262 || KeyName(got) != "TG(2)" {
		t.Fatalf("TG %04X %q", got, KeyName(got))
	}
	if got, ok := ToLayer(0); !ok || got != 0x5200 || KeyName(got) != "TO(0)" {
		t.Fatalf("TO %04X %q", got, KeyName(got))
	}
	if got, ok := TapToggleLayer(1); !ok || got != 0x52C1 || KeyName(got) != "TT(1)" {
		t.Fatalf("TT %04X %q", got, KeyName(got))
	}
	if got, ok := OneShotLayer(2); !ok || got != 0x5282 || KeyName(got) != "OSL(2)" {
		t.Fatalf("OSL %04X %q", got, KeyName(got))
	}
	if got, ok := DefaultLayer(1); !ok || got != 0x5241 || KeyName(got) != "DF(1)" {
		t.Fatalf("DF %04X %q", got, KeyName(got))
	}
	if got, ok := LayerTap(2, 0x2C); !ok || got != 0x422C || KeyName(got) != "LT(2,Space)" {
		t.Fatalf("LT %04X %q", got, KeyName(got))
	}
	if _, ok := LayerTap(16, 0x2C); ok {
		t.Fatal("LT layer 16")
	}
	if _, ok := LayerTap(0, 0x100); ok {
		t.Fatal("LT tap is not a basic key")
	}
	if got, ok := MacroKey(1); !ok || got != 0x7701 || KeyName(got) != "M1" {
		t.Fatalf("macro %04X %q", got, KeyName(got))
	}
	if got, ok := MacroKey(127); !ok || got != 0x777F || KeyName(got) != "M127" {
		t.Fatalf("last macro %04X %q", got, KeyName(got))
	}
	if _, ok := MacroKey(128); ok {
		t.Fatal("macro 128")
	}
}

func TestCustomKeyLabel(t *testing.T) {
	kb, err := keyboardFromJSON([]byte(`{
		"name": "K",
		"matrix": {"rows": 1, "cols": 1},
		"layouts": {"keymap": [["0,0"]]},
		"customKeycodes": [{"name": "FOO", "shortName": "Foo"}, {"name": "BAR"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := kb.Label(0x7E00); got != "Foo" {
		t.Fatalf("custom 0: %q", got)
	}
	if got := kb.Label(0x7E01); got != "BAR" {
		t.Fatalf("custom 1: %q", got)
	}
	if got := kb.Label(0x7E02); got != "USER02" {
		t.Fatalf("unset custom: %q", got)
	}
}
