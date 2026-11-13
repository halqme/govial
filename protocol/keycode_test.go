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
