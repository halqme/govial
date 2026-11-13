package protocol

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ulikunitz/xz"
)

func TestKnobLegendIsNotAMatrixKey(t *testing.T) {
	keys, err := parseLayout([]byte(`[
		["0,0","0,1\n\n\n\n\n\n\n\n\ne"],
		["1,1\n\n\n\n\n\n\n\n\ne","0,0\n\n\n\n\n\n\n\n\ne","1,0\n\n\n\n\n\n\n\n\ne"]
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 5 {
		t.Fatalf("keys = %d", len(keys))
	}
	matrix := keys[0]
	if !matrix.HasMatrix || matrix.Encoder || matrix.Row != 0 || matrix.Col != 0 {
		t.Fatalf("matrix key %+v", matrix)
	}
	want := []struct{ index, dir int }{{0, 1}, {1, 1}, {0, 0}, {1, 0}}
	for i, spec := range want {
		key := keys[i+1]
		if key.HasMatrix || !key.Encoder || key.EncoderIndex != spec.index || key.EncDir != spec.dir {
			t.Fatalf("knob %d = matrix %v encoder %v index %d dir %d", i, key.HasMatrix, key.Encoder, key.EncoderIndex, key.EncDir)
		}
	}
}

func TestReadKnobAssignment(t *testing.T) {
	raw := []byte(`{"name":"Knob","matrix":{"rows":1,"cols":2},"layouts":{"keymap":[["0,0","0,1\n\n\n\n\n\n\n\n\ne"]]}}`)
	payload := xzPayload(t, raw)
	// Matrix column 1 is Q. The knob legend says 0,1, which is its direction, not that column.
	dev := &fakeKeyboard{
		payload: payload,
		layers:  1,
		codes:   []uint16{0x0004, 0x0014},
		enc:     [][2]uint16{{0x004E, 0x004B}},
	}
	kb, err := Read(dev)
	if err != nil {
		t.Fatal(err)
	}
	var knob Key
	var found bool
	for _, key := range kb.Keys {
		if key.Encoder {
			knob = key
			found = true
		}
	}
	if !found || knob.HasMatrix {
		t.Fatalf("knob %+v", knob)
	}
	code, ok := kb.EncoderCode(0, knob.EncoderIndex, knob.EncDir)
	if !ok || code != 0x004B {
		t.Fatalf("knob code %04X ok=%v", code, ok)
	}
	matrix, ok := kb.Code(0, 0, 1)
	if !ok || matrix != 0x0014 || matrix == code {
		t.Fatalf("matrix column used for the knob: %04X ok=%v", matrix, ok)
	}
	got, read, err := kb.WriteEncoder(dev, 0, 0, 1, 0x0028)
	if err != nil || !read || got != 0x0028 {
		t.Fatalf("write got %04X read %v err %v", got, read, err)
	}
	if dev.enc[0][1] != 0x0028 || dev.enc[0][0] != 0x004E {
		t.Fatalf("device knobs %+v", dev.enc)
	}
}

func xzPayload(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatal("definition is not json")
	}
	return buf.Bytes()
}
