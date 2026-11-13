package protocol

import (
	"bytes"
	"testing"
)

func TestLayoutOptionUsesSelectedArrangement(t *testing.T) {
	keys, err := parseLayout([]byte(`[["0,0",{"x":2},"1,0\n\n\n0,0"],[{"x":30},"1,1\n\n\n0,1"]]`))
	if err != nil {
		t.Fatal(err)
	}
	kb := &Keyboard{
		Keys:    keys,
		layouts: []LayoutOption{{Name: "配列", Choices: []string{"手前", "奥"}}},
		choices: []int{0},
	}

	var shown []Key
	var far bool
	for _, key := range kb.Keys {
		if !kb.Shows(key) {
			if key.X >= 30 {
				far = true
			}
			continue
		}
		shown = append(shown, kb.Place(key))
	}
	if !far {
		t.Fatal("the unused arrangement was not in the definition")
	}
	for _, key := range shown {
		if key.X >= 30 {
			t.Fatalf("unused arrangement is still on the board at x=%v", key.X)
		}
	}
	if len(shown) != 2 {
		t.Fatalf("shown keys = %d, want the base key and the default arrangement", len(shown))
	}

	if !kb.SetChoice(0, 1) {
		t.Fatal("could not select the other arrangement")
	}
	var moved Key
	var found bool
	for _, key := range kb.Keys {
		if !kb.Shows(key) || !key.HasOption {
			continue
		}
		moved = kb.Place(key)
		found = true
	}
	if !found {
		t.Fatal("selected arrangement is not shown")
	}
	if moved.X != 3 || moved.Y != 0 {
		t.Fatalf("selected arrangement at (%v,%v), want it on the default arrangement", moved.X, moved.Y)
	}
	for _, key := range kb.Keys {
		if key.HasOption && key.Choice == 0 && kb.Shows(key) {
			t.Fatal("default arrangement stays visible after another is selected")
		}
	}
}

func TestLayoutBitsRoundTrip(t *testing.T) {
	labels := parseLayoutOptions([]byte(`[["Trackball","Left","Right"],"RGB"]`))
	if len(labels) != 2 || labels[0].Name != "Trackball" || len(labels[0].Choices) != 2 || len(labels[1].Choices) != 2 {
		t.Fatalf("labels %+v", labels)
	}
	kb := &Keyboard{layouts: labels}
	kb.choices = []int{1, 0}
	if got := kb.LayoutBits(); got != 2 {
		t.Fatalf("bits = %d, want 2", got)
	}
	kb.UseLayoutBits(2)
	if got := kb.Choices(); len(got) != 2 || got[0] != 1 || got[1] != 0 {
		t.Fatalf("choices %v", got)
	}
	kb.SetChoice(1, 1)
	kb.UseLayoutBits(kb.LayoutBits())
	if got := kb.Choices(); got[0] != 1 || got[1] != 1 {
		t.Fatalf("second option choices %v", got)
	}
}

func TestLayoutOptionWire(t *testing.T) {
	msg := setLayoutOptions(6)
	if !bytes.Equal(msg[:6], []byte{cmdSetKeyboardValue, keyboardLayoutOptions, 0, 0, 0, 6}) {
		t.Fatalf("set %x", msg[:6])
	}
	resp := make([]byte, 32)
	resp[0], resp[1], resp[5] = cmdGetKeyboardValue, keyboardLayoutOptions, 6
	got, err := parseLayoutOptionBits(resp)
	if err != nil || got != 6 {
		t.Fatalf("parse %d %v", got, err)
	}
}
