package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/ulikunitz/xz"
)

// pythonXz is lzma.compress of the JSON below, which is the container Vial
// stores on the keyboard. Defined here so the test does not follow the Go
// compressor.
//
// {"name":"Test","matrix":{"rows":2,"cols":2},"layouts":{"keymap":[["0,0","0,1"],["1,0","1,1"]]}}
const pythonXz = "fd377a585a000004e6d6b4460200210116000000742fe5a3e0005e00545d003d8889c65436c3174fe4e6be7a4e39462cc239c67988ef3a203ac7952beaa900673c31c4e0f93139c352206c9d4c4ca033792005468113edf6a15fe482950b586f3a2e59b40cd93b93bf2bde0a1d06410ef4270000d673ad9d02d30fb70001705fb8806df41fb6f37d010000000004595a"

func TestReadKeyboard(t *testing.T) {
	payload, err := hex.DecodeString(pythonXz)
	if err != nil {
		t.Fatal(err)
	}
	codes := []uint16{0x0004, 0x0005, 0x0006, 0x0007, 0x001E, 0x001F, 0x0020, 0x0021}
	dev := &fakeKeyboard{payload: payload, layers: 2, codes: codes}
	kb, err := Read(dev)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if kb.Name != "Test" || kb.ViaProtocol != 9 || kb.VialProtocol != 6 {
		t.Fatalf("identity: %+v", kb)
	}
	if kb.UIDString() != "0807060504030201" {
		t.Fatalf("uid %s", kb.UIDString())
	}
	if kb.Rows != 2 || kb.Cols != 2 || kb.Layers != 2 || len(kb.Keys) != 4 {
		t.Fatalf("shape rows=%d cols=%d layers=%d keys=%d", kb.Rows, kb.Cols, kb.Layers, len(kb.Keys))
	}
	if code, ok := kb.Code(0, 0, 1); !ok || code != 0x0005 || KeyName(code) != "B" {
		t.Fatalf("layer 0 col 1 = %04X %s ok=%v", code, KeyName(code), ok)
	}
	if code, ok := kb.Code(1, 1, 0); !ok || code != 0x0020 || KeyName(code) != "3" {
		t.Fatalf("layer 1 row 1 = %04X %s ok=%v", code, KeyName(code), ok)
	}
	if _, ok := kb.Code(2, 0, 0); ok {
		t.Fatal("layer 2 should be absent")
	}
	if KeyName(0x0001) != "▽" || KeyName(0x1234) != "RS(')" {
		t.Fatalf("names %q %q", KeyName(0x0001), KeyName(0x1234))
	}

	var blocks []int
	for _, req := range dev.reqs {
		if req[0] == cmdVialPrefix && req[1] == vialGetDefinition {
			blocks = append(blocks, int(binary.LittleEndian.Uint32(req[2:6])))
		}
	}
	if len(blocks) != 5 || blocks[0] != 0 || blocks[4] != 4 {
		t.Fatalf("definition blocks %v", blocks)
	}
	off := dev.reqs[len(dev.reqs)-1]
	if off[0] != cmdKeymapGetBuffer {
		t.Fatalf("last request %x", off[0])
	}
}

func TestMacroCountComesFromTheKeyboard(t *testing.T) {
	payload, err := hex.DecodeString(pythonXz)
	if err != nil {
		t.Fatal(err)
	}
	codes := make([]uint16, 8)
	dev := &fakeKeyboard{payload: payload, layers: 2, codes: codes, macros: 4}
	kb, err := Read(dev)
	if err != nil {
		t.Fatal(err)
	}
	if kb.Macros != 4 {
		t.Fatalf("macros %d", kb.Macros)
	}
	dev = &fakeKeyboard{payload: payload, layers: 2, codes: codes, macros: 200}
	kb, err = Read(dev)
	if err != nil {
		t.Fatal(err)
	}
	if kb.Macros != 128 {
		t.Fatalf("assignable macros %d", kb.Macros)
	}
	dev = &fakeKeyboard{payload: payload, layers: 2, codes: codes, macros: -1}
	kb, err = Read(dev)
	if err != nil {
		t.Fatal(err)
	}
	if kb.Macros != 0 {
		t.Fatalf("unanswered macros %d", kb.Macros)
	}
	if _, ok := kb.Code(0, 0, 0); !ok {
		t.Fatal("keymap missing when the keyboard does not answer the macro command")
	}
}

func TestReadRejectsBadDevice(t *testing.T) {
	payload, err := hex.DecodeString(pythonXz)
	if err != nil {
		t.Fatal(err)
	}
	dev := &fakeKeyboard{payload: payload, layers: 2, via: 1, codes: make([]uint16, 8)}
	kb, err := Read(dev)
	if err == nil || kb != nil {
		t.Fatal("expected unsupported VIA protocol")
	}
	dev = &fakeKeyboard{payload: payload, layers: 2, vial: 99, codes: make([]uint16, 8)}
	if kb, err = Read(dev); err == nil || kb != nil {
		t.Fatal("expected unsupported Vial protocol")
	}
	dev = &fakeKeyboard{fail: true}
	if kb, err = Read(dev); err == nil || kb != nil {
		t.Fatal("expected exchange failure")
	}
}

func TestLayoutOutsideMatrix(t *testing.T) {
	var buf bytes.Buffer
	w, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`{"name":"Bad","matrix":{"rows":1,"cols":1},"layouts":{"keymap":[["5,0"]]}}`)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	dev := &fakeKeyboard{payload: buf.Bytes(), layers: 1, codes: []uint16{0}}
	kb, err := Read(dev)
	if err == nil || kb != nil {
		t.Fatal("key 5,0 is outside the matrix and must not load")
	}
}

func TestKLEPlacement(t *testing.T) {
	keys, err := parseLayout([]byte(`[[{"w":2.25},"0,0",{"x":0.5},"0,2"],[{"y":0.25,"h":1.5},"1,0"]]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("keys %d", len(keys))
	}
	if keys[0].W != 2.25 || keys[0].X != 0 || keys[0].Row != 0 || keys[0].Col != 0 {
		t.Fatalf("first %+v", keys[0])
	}
	if keys[1].X != 2.75 || keys[1].Col != 2 {
		t.Fatalf("second %+v", keys[1])
	}
	if keys[2].X != 0 || keys[2].Y != 1.25 || keys[2].H != 1.5 || keys[2].Row != 1 {
		t.Fatalf("third %+v", keys[2])
	}

	rotated, err := parseLayout([]byte(`[[{"r":90,"rx":1,"ry":2},"0,0"]]`))
	if err != nil {
		t.Fatal(err)
	}
	key := rotated[0]
	if key.Rot != 90 || key.RotX != 1 || key.RotY != 2 || key.X != 1 || key.Y != 2 {
		t.Fatalf("rotated %+v", key)
	}
	// Local center (1.5, 2.5) lands at (0.5, 2.5) after a clockwise quarter turn.
	if !key.Contains(0.5, 2.5) {
		t.Fatal("rotated center should hit")
	}
	if key.Contains(1.5, 2.5) {
		t.Fatal("unrotated center should miss after a quarter turn")
	}
	plain := Key{X: 0, Y: 0, W: 1, H: 1}
	if !plain.Contains(0.5, 0.5) || plain.Contains(1.5, 0.5) {
		t.Fatal("axis-aligned hit box")
	}
}

type fakeKeyboard struct {
	payload   []byte
	layers    int
	rows      int
	cols      int
	via       int
	vial      uint32
	codes     []uint16
	fail      bool
	ignoreSet bool
	holding   bool
	unlocked  bool
	pending   bool
	counter   int
	hold      []KeyPos
	reqs      [][]byte
	enc       [][2]uint16
	macros    int
}

func (f *fakeKeyboard) Exchange(msg []byte) ([]byte, error) {
	if f.fail {
		return nil, errors.New("unplugged")
	}
	if len(msg) < 32 {
		return nil, errors.New("report must be 32 bytes")
	}
	f.reqs = append(f.reqs, append([]byte(nil), msg[:6]...))
	resp := make([]byte, 32)
	via := f.via
	if via == 0 {
		via = 9
	}
	vial := f.vial
	if vial == 0 {
		vial = 6
	}
	switch msg[0] {
	case cmdGetProtocolVersion:
		binary.BigEndian.PutUint16(resp[1:3], uint16(via))
	case cmdGetLayerCount:
		resp[1] = byte(f.layers)
	case cmdMacroGetCount:
		if f.macros >= 0 {
			resp[0] = cmdMacroGetCount
			resp[1] = byte(f.macros)
		}
	case cmdGetKeycode:
		code, ok := f.codeAt(int(msg[1]), int(msg[2]), int(msg[3]))
		if !ok {
			return nil, errors.New("keycode range")
		}
		copy(resp, msg[:4])
		resp[4] = byte(code >> 8)
		resp[5] = byte(code)
	case cmdSetKeycode:
		if !f.ignoreSet {
			if err := f.setCode(int(msg[1]), int(msg[2]), int(msg[3]), binary.BigEndian.Uint16(msg[4:6])); err != nil {
				return nil, err
			}
		}
		copy(resp, msg[:6])
	case cmdKeymapGetBuffer:
		off := int(msg[1])<<8 | int(msg[2])
		n := int(msg[3])
		raw := make([]byte, len(f.codes)*2)
		for i, code := range f.codes {
			binary.BigEndian.PutUint16(raw[i*2:], code)
		}
		if off < 0 || n < 0 || off+n > len(raw) {
			return nil, errors.New("keymap range")
		}
		copy(resp[4:], raw[off:off+n])
	case cmdVialPrefix:
		switch msg[1] {
		case vialGetKeyboardID:
			binary.LittleEndian.PutUint32(resp[0:4], vial)
			copy(resp[4:12], []byte{1, 2, 3, 4, 5, 6, 7, 8})
		case vialGetSize:
			binary.LittleEndian.PutUint32(resp[0:4], uint32(len(f.payload)))
		case vialGetEncoder:
			layer, idx := int(msg[2]), int(msg[3])
			n := 0
			if f.layers > 0 {
				n = len(f.enc) / f.layers
			}
			off := layer*n + idx
			if layer < 0 || idx < 0 || n == 0 || off < 0 || off >= len(f.enc) {
				return nil, errors.New("encoder range")
			}
			binary.BigEndian.PutUint16(resp[0:2], f.enc[off][0])
			binary.BigEndian.PutUint16(resp[2:4], f.enc[off][1])
		case vialSetEncoder:
			layer, idx, dir := int(msg[2]), int(msg[3]), int(msg[4])
			n := 0
			if f.layers > 0 {
				n = len(f.enc) / f.layers
			}
			off := layer*n + idx
			if layer < 0 || idx < 0 || (dir != 0 && dir != 1) || n == 0 || off < 0 || off >= len(f.enc) {
				return nil, errors.New("encoder range")
			}
			f.enc[off][dir] = binary.BigEndian.Uint16(msg[5:7])
		case vialGetDefinition:
			block := int(binary.LittleEndian.Uint32(msg[2:6]))
			start := block * ReportLen
			if start >= len(f.payload) {
				return nil, errors.New("definition block")
			}
			copy(resp, f.payload[start:])
		case vialUnlockStatus:
			f.unlockResponse(resp)
		case vialUnlockStart:
			f.pending = true
			f.counter = 50
		case vialUnlockPoll:
			if f.pending {
				if f.holding && f.counter > 0 {
					f.counter--
				} else {
					f.counter = 50
				}
				if f.counter == 0 {
					f.pending = false
					f.unlocked = true
				}
			}
			f.unlockResponse(resp)
			resp[2] = byte(f.counter)
		case vialLock:
			f.unlocked = false
		default:
			return nil, errors.New("unknown vial command")
		}
	default:
		return nil, errors.New("unknown command")
	}
	return resp, nil
}

func (f *fakeKeyboard) shape() (rows, cols int) {
	rows, cols = f.rows, f.cols
	if rows == 0 {
		rows = 2
	}
	if cols == 0 {
		cols = 2
	}
	return rows, cols
}

func (f *fakeKeyboard) codeAt(layer, row, col int) (uint16, bool) {
	rows, cols := f.shape()
	if layer < 0 || row < 0 || col < 0 || row >= rows || col >= cols {
		return 0, false
	}
	i := (layer*rows+row)*cols + col
	if i >= len(f.codes) {
		return 0, false
	}
	return f.codes[i], true
}

func (f *fakeKeyboard) setCode(layer, row, col int, code uint16) error {
	rows, cols := f.shape()
	i := (layer*rows+row)*cols + col
	if layer < 0 || row < 0 || col < 0 || row >= rows || col >= cols || i >= len(f.codes) {
		return errors.New("keycode range")
	}
	f.codes[i] = code
	return nil
}

func (f *fakeKeyboard) unlockResponse(resp []byte) {
	if f.unlocked {
		resp[0] = 1
	}
	if f.pending {
		resp[1] = 1
	}
	for i := 0; i < 15; i++ {
		resp[2+i*2] = 0xFF
		resp[3+i*2] = 0xFF
	}
	for i, key := range f.hold {
		if i >= 15 {
			break
		}
		resp[2+i*2] = byte(key.Row)
		resp[3+i*2] = byte(key.Col)
	}
}

func TestWriteKeyRoundTrip(t *testing.T) {
	payload, err := hex.DecodeString(pythonXz)
	if err != nil {
		t.Fatal(err)
	}
	codes := []uint16{0x0004, 0x0005, 0x0006, 0x0007, 0x001E, 0x001F, 0x0020, 0x0021}
	dev := &fakeKeyboard{
		payload: payload,
		layers:  2,
		hold:    []KeyPos{{Row: 0, Col: 1}, {Row: 1, Col: 0}},
		codes:   append([]uint16(nil), codes...),
	}
	kb, err := Read(dev)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := Security(dev)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Open || lock.Pending || len(lock.Keys) != 2 || lock.Keys[0] != (KeyPos{Row: 0, Col: 1}) || lock.Keys[1] != (KeyPos{Row: 1, Col: 0}) {
		t.Fatalf("lock %+v", lock)
	}
	if err := BeginUnlock(dev); err != nil {
		t.Fatal(err)
	}
	open, left, err := PollUnlock(dev)
	if err != nil || open || left != 50 {
		t.Fatalf("not holding: open=%v left=%d err=%v", open, left, err)
	}
	dev.holding = true
	for range 50 {
		open, left, err = PollUnlock(dev)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !open || left != 0 {
		t.Fatalf("unlocked open=%v left=%d", open, left)
	}
	if err := LockKeyboard(dev); err != nil {
		t.Fatal(err)
	}
	lock, err = Security(dev)
	if err != nil || lock.Open || lock.Pending {
		t.Fatalf("relocked %+v %v", lock, err)
	}

	before := len(dev.reqs)
	if _, read, err := kb.WriteKey(dev, 9, 0, 0, 0x0029); err == nil || read || len(dev.reqs) != before {
		t.Fatalf("out of range err=%v read=%v reqs %d -> %d", err, read, before, len(dev.reqs))
	}

	got, read, err := kb.WriteKey(dev, 1, 1, 0, 0x0029)
	if err != nil || !read || got != 0x0029 {
		t.Fatalf("write got=%04X read=%v err=%v", got, read, err)
	}
	var set []byte
	for _, req := range dev.reqs[before:] {
		if req[0] == cmdSetKeycode {
			set = req
		}
	}
	if len(set) < 6 || set[0] != cmdSetKeycode || set[1] != 1 || set[2] != 1 || set[3] != 0 || set[4] != 0x00 || set[5] != 0x29 {
		t.Fatalf("set report %x", set)
	}
	if code, ok := kb.Code(1, 1, 0); !ok || code != 0x0020 {
		t.Fatalf("cache changed to %04X", code)
	}
	if !kb.Store(1, 1, 0, got) {
		t.Fatal("store")
	}
	if code, ok := kb.Code(1, 1, 0); !ok || code != 0x0029 || KeyName(code) != "Esc" {
		t.Fatalf("stored %04X %s", code, KeyName(code))
	}

	dev.ignoreSet = true
	got, read, err = kb.WriteKey(dev, 0, 0, 0, 0x0000)
	if err == nil || !read || got != 0x0004 {
		t.Fatalf("rejected write got=%04X read=%v err=%v", got, read, err)
	}
	if code, _ := kb.Code(0, 0, 0); code != 0x0004 {
		t.Fatalf("rejected write changed cache to %04X", code)
	}
}

type scriptExchange struct {
	replies [][]byte
}

func (s *scriptExchange) Exchange(report []byte) ([]byte, error) {
	if len(s.replies) == 0 {
		return nil, errors.New("no reply")
	}
	reply := s.replies[0]
	s.replies = s.replies[1:]
	return reply, nil
}

func TestWriteKeyConfirmsAfterAnotherReport(t *testing.T) {
	kb := &Keyboard{Layers: 1, Rows: 1, Cols: 1, codes: []uint16{0x0004}}
	ex := &scriptExchange{replies: [][]byte{
		{cmdSetKeycode, 0, 0, 0, 0x00, 0x05},
		{0x00},
		{cmdGetKeycode, 0, 0, 0, 0x00, 0x05},
	}}
	got, read, err := kb.WriteKey(ex, 0, 0, 0, 0x0005)
	if err != nil || !read || got != 0x0005 {
		t.Fatalf("got=%04X read=%v err=%v", got, read, err)
	}
	if code, _ := kb.Code(0, 0, 0); code != 0x0004 {
		t.Fatalf("cache changed to %04X", code)
	}
}
