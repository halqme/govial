package protocol

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestMacrosRoundTrip(t *testing.T) {
	// Hi, tap A then B, wait 300ms, hold a keycode whose low byte is 0,
	// release M1, tap the keycode that QMK stores as 0xFFFF, then é as one byte.
	first := []byte{
		'H', 'i',
		0x01, 0x01, 0x04,
		0x01, 0x01, 0x05,
		0x01, 0x04, 45 + 1, 1 + 1, // 300 = 45 + 1*255
		0x01, 0x06, 0x02, 0xFF, // down 0x0200, remapped
		0x01, 0x07, 0x01, 0x77, // up 0x7701
		0x01, 0x05, 0xFF, 0xFF, // tap 0xFF00, remapped
		0xE9,
	}
	stored := append(append(first, 0), 0) // second macro is empty
	pad := bytes.Repeat([]byte{0xFF}, 8)
	dev := &fakeKeyboard{
		macros:   2,
		macroMem: len(stored) + len(pad),
		macroBuf: append(append([]byte(nil), stored...), pad...),
	}
	got, memory, err := LoadMacros(dev, 6)
	if err != nil {
		t.Fatal(err)
	}
	if memory != dev.macroMem {
		t.Fatalf("memory %d", memory)
	}
	want := []Macro{{
		{Kind: ActionText, Text: "Hi"},
		{Kind: ActionTap, Keys: []uint16{0x0004, 0x0005}},
		{Kind: ActionDelay, Delay: 300},
		{Kind: ActionDown, Keys: []uint16{0x0200}},
		{Kind: ActionUp, Keys: []uint16{0x7701}},
		{Kind: ActionTap, Keys: []uint16{0xFF00}},
		{Kind: ActionText, Text: "é"},
	}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded %#v", got)
	}

	got[0][1].Keys = []uint16{0x0004}
	if err := SaveMacros(dev, 6, got); err != nil {
		t.Fatal(err)
	}
	again, _, err := LoadMacros(dev, 6)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, got) {
		t.Fatalf("saved %#v", again)
	}
	if !bytes.Equal(dev.macroBuf[len(dev.macroBuf)-len(pad):], pad) {
		t.Fatal("save overwrote the unused tail")
	}
}

func TestMacroBufferSpansReports(t *testing.T) {
	text := string(bytes.Repeat([]byte{'a'}, 40))
	dev := &fakeKeyboard{macros: 1, macroMem: 64, macroBuf: make([]byte, 64)}
	macros := []Macro{{{Kind: ActionText, Text: text}}}
	if err := SaveMacros(dev, 6, macros); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadMacros(dev, 6)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, macros) {
		t.Fatalf("across reports %#v", got)
	}
}

func TestOldMacrosHaveNoDelayOrWideKeys(t *testing.T) {
	stored := []byte{'H', 'i', 0x01, 0x04, 0x02, 0x05, 0}
	dev := &fakeKeyboard{macros: 1, macroMem: 32, macroBuf: append([]byte(nil), stored...)}
	got, _, err := LoadMacros(dev, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []Macro{{{Kind: ActionText, Text: "Hi"}, {Kind: ActionTap, Keys: []uint16{0x04}}, {Kind: ActionDown, Keys: []uint16{0x05}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("old format %#v", got)
	}
	before := append([]byte(nil), dev.macroBuf...)
	dev.reqs = nil
	if err := SaveMacros(dev, 1, []Macro{{{Kind: ActionDelay, Delay: 10}}}); err == nil {
		t.Fatal("old keyboard accepted a delay")
	}
	if err := SaveMacros(dev, 1, []Macro{{{Kind: ActionTap, Keys: []uint16{0x0100}}}}); err == nil {
		t.Fatal("old keyboard accepted a wide keycode")
	}
	if !bytes.Equal(dev.macroBuf, before) || hasSet(dev) {
		t.Fatal("rejected macro was written")
	}
	if err := SaveMacros(dev, 1, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dev.macroBuf[:len(stored)], stored) {
		t.Fatalf("old bytes %x", dev.macroBuf[:len(stored)])
	}
}

func TestMacrosThatDoNotFitAreNotWritten(t *testing.T) {
	dev := &fakeKeyboard{macros: 1, macroMem: 4, macroBuf: []byte{0x11, 0x22, 0x33, 0x44}}
	before := append([]byte(nil), dev.macroBuf...)
	err := SaveMacros(dev, 6, []Macro{{{Kind: ActionText, Text: "hello"}}})
	var big *TooBigError
	if !errors.As(err, &big) || big.Need <= big.Have {
		t.Fatalf("fit error %v", err)
	}
	if !bytes.Equal(dev.macroBuf, before) || hasSet(dev) {
		t.Fatal("oversized macro was written")
	}

	dev.reqs = nil
	err = SaveMacros(dev, 6, []Macro{{}, {}})
	if err == nil {
		t.Fatal("count mismatch was accepted")
	}
	if !bytes.Equal(dev.macroBuf, before) || hasSet(dev) {
		t.Fatal("count mismatch was written")
	}
}

func TestMacroDelayLimits(t *testing.T) {
	dev := &fakeKeyboard{macros: 1, macroMem: 16, macroBuf: make([]byte, 16)}
	for _, delay := range []int{0, 65024} {
		macros := []Macro{{{Kind: ActionDelay, Delay: delay}}}
		if err := SaveMacros(dev, 2, macros); err != nil {
			t.Fatal(delay, err)
		}
		got, _, err := LoadMacros(dev, 2)
		if err != nil || !reflect.DeepEqual(got, macros) {
			t.Fatalf("delay %d %#v %v", delay, got, err)
		}
	}
	dev.reqs = nil
	before := append([]byte(nil), dev.macroBuf...)
	if err := SaveMacros(dev, 2, []Macro{{{Kind: ActionDelay, Delay: 65025}}}); err == nil {
		t.Fatal("delay 65025")
	}
	if err := SaveMacros(dev, 2, []Macro{{{Kind: ActionText, Text: "a\x00b"}}}); err == nil {
		t.Fatal("text containing a NUL")
	}
	if err := SaveMacros(dev, 2, []Macro{{{Kind: ActionText, Text: "a\x01b"}}}); err == nil {
		t.Fatal("text containing the action prefix")
	}
	if hasSet(dev) || !bytes.Equal(dev.macroBuf, before) {
		t.Fatal("invalid macro was written")
	}
}

func TestBrokenMacroTailIsKept(t *testing.T) {
	// An unknown action is skipped. A key action cut off at the end is dropped.
	stored := []byte{0x01, 0xFF, 'H', 'i', 0x01, 0x05, 0x01, 0, 0}
	dev := &fakeKeyboard{macros: 2, macroMem: len(stored), macroBuf: stored}
	got, _, err := LoadMacros(dev, 6)
	if err != nil {
		t.Fatal(err)
	}
	want := []Macro{{{Kind: ActionText, Text: "Hi"}}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("broken %#v", got)
	}
}

func TestNoMacroSlots(t *testing.T) {
	dev := &fakeKeyboard{macros: -1, macroMem: 32, macroBuf: []byte{1, 2, 3}}
	macros, memory, err := LoadMacros(dev, 6)
	if err != nil || macros != nil || memory != 0 {
		t.Fatalf("unanswered %v %d %v", macros, memory, err)
	}
	dev.macros = 2
	dev.macroMem = 0
	macros, memory, err = LoadMacros(dev, 6)
	if err != nil || memory != 0 || len(macros) != 2 || len(macros[0]) != 0 || len(macros[1]) != 0 {
		t.Fatalf("no memory %#v %d %v", macros, memory, err)
	}
}

func hasSet(dev *fakeKeyboard) bool {
	for _, req := range dev.reqs {
		if req[0] == cmdMacroSetBuffer {
			return true
		}
	}
	return false
}
