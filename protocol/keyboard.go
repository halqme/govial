package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ulikunitz/xz"
)

// Exchanger sends one padded 32-byte report and returns the device report.
type Exchanger interface {
	Exchange(report []byte) ([]byte, error)
}

// Keyboard is the layout and keymap read from a Vial device.
type Keyboard struct {
	Name         string
	ViaProtocol  int
	VialProtocol uint32
	UID          [8]byte
	Rows, Cols   int
	Layers       int
	Keys         []Key
	Custom       []string
	codes        []uint16
	encN         int
	enc          []uint16
	layouts      []LayoutOption
	choices      []int
	pivots       map[[2]int][2]float64
}

// Label is the keycap text, including a short name from the keyboard
// definition when the code is one of its custom keycodes.
func (k *Keyboard) Label(code uint16) string {
	if k != nil {
		i := int(code) - 0x7E00
		if i >= 0 && i < len(k.Custom) && k.Custom[i] != "" {
			return k.Custom[i]
		}
	}
	return KeyName(code)
}

// UIDString is the keyboard id as Vial's integer, hex.
func (k *Keyboard) UIDString() string {
	return fmt.Sprintf("%016X", binary.LittleEndian.Uint64(k.UID[:]))
}

// Code returns the keycode stored for a matrix position.
func (k *Keyboard) Code(layer, row, col int) (uint16, bool) {
	if !k.contains(layer, row, col) {
		return 0, false
	}
	return k.codes[(layer*k.Rows+row)*k.Cols+col], true
}

// Store replaces the cached keycode. It does not send anything to the keyboard.
func (k *Keyboard) Store(layer, row, col int, code uint16) bool {
	if !k.contains(layer, row, col) {
		return false
	}
	k.codes[(layer*k.Rows+row)*k.Cols+col] = code
	return true
}

// EncoderCode returns the keycode for one knob direction on a layer.
// Direction 0 is counter-clockwise and 1 is clockwise.
func (k *Keyboard) EncoderCode(layer, index, dir int) (uint16, bool) {
	i, ok := k.encAt(layer, index, dir)
	if !ok {
		return 0, false
	}
	return k.enc[i], true
}

// StoreEncoder replaces the cached knob keycode. It does not send anything.
func (k *Keyboard) StoreEncoder(layer, index, dir int, code uint16) bool {
	i, ok := k.encAt(layer, index, dir)
	if !ok {
		return false
	}
	k.enc[i] = code
	return true
}

func (k *Keyboard) encAt(layer, index, dir int) (int, bool) {
	if k == nil || k.enc == nil || layer < 0 || layer >= k.Layers || index < 0 || index >= k.encN || (dir != 0 && dir != 1) {
		return 0, false
	}
	return (layer*k.encN+index)*2 + dir, true
}

func encoderCount(keys []Key) int {
	n := 0
	for _, key := range keys {
		if key.Encoder && key.EncoderIndex+1 > n {
			n = key.EncoderIndex + 1
		}
	}
	return n
}

func (k *Keyboard) contains(layer, row, col int) bool {
	return k != nil && layer >= 0 && row >= 0 && col >= 0 && layer < k.Layers && row < k.Rows && col < k.Cols && k.codes != nil
}

// KeyPos is one matrix position.
type KeyPos struct {
	Row int
	Col int
}

// Unlock is the keyboard's write lock. Keys are the positions to hold.
type Unlock struct {
	Open    bool
	Pending bool
	Keys    []KeyPos
}

// Security reads whether the keyboard is unlocked.
func Security(ex Exchanger) (Unlock, error) {
	resp, err := call(ex, unlockStatus())
	if err != nil {
		return Unlock{}, err
	}
	open, pending, keys, err := parseUnlockStatus(resp)
	if err != nil {
		return Unlock{}, err
	}
	return Unlock{Open: open, Pending: pending, Keys: keys}, nil
}

// BeginUnlock starts the hold-to-unlock countdown. Until it finishes, the
// keyboard ignores keymap reads and writes. Replugging is the only cancel.
func BeginUnlock(ex Exchanger) error {
	_, err := call(ex, unlockStart())
	return err
}

// PollUnlock reports the countdown. Left stays at 50 until every unlock key
// is held, then drops once per successful poll. Open becomes true at zero.
func PollUnlock(ex Exchanger) (open bool, left int, err error) {
	resp, err := call(ex, unlockPoll())
	if err != nil {
		return false, 0, err
	}
	return parseUnlockPoll(resp)
}

// LockKeyboard locks the keyboard again.
func LockKeyboard(ex Exchanger) error {
	_, err := call(ex, lockKeyboard())
	return err
}

// WriteKey stores code at a matrix position and reads that position back.
// It does not change the cached keymap. read is false when the keyboard did
// not confirm a value, including when the position is outside the keymap.
func (k *Keyboard) WriteKey(ex Exchanger, layer, row, col int, code uint16) (got uint16, read bool, err error) {
	if !k.contains(layer, row, col) {
		return 0, false, fmt.Errorf("key %d %d,%d is outside the keymap", layer, row, col)
	}
	req, err := setKeycode(layer, row, col, code)
	if err != nil {
		return 0, false, err
	}
	if _, err = call(ex, req); err != nil {
		return 0, false, err
	}
	got, err = readKeycode(ex, layer, row, col)
	if err != nil || got != code {
		// The first reply is sometimes a different report. Confirm once more.
		again, againErr := readKeycode(ex, layer, row, col)
		if againErr == nil {
			got, err = again, nil
		}
	}
	if err != nil {
		return 0, false, err
	}
	if got != code {
		return got, true, &Kept{Got: got}
	}
	return got, true, nil
}

// WriteEncoder stores code on one knob direction and reads it back.
// It does not change the cached encoder map. read is false when the keyboard
// did not confirm a value.
func (k *Keyboard) WriteEncoder(ex Exchanger, layer, index, dir int, code uint16) (got uint16, read bool, err error) {
	if _, ok := k.encAt(layer, index, dir); !ok {
		return 0, false, fmt.Errorf("encoder %d %d,%d is outside the keymap", layer, index, dir)
	}
	req, err := setEncoder(layer, index, dir, code)
	if err != nil {
		return 0, false, err
	}
	if _, err = call(ex, req); err != nil {
		return 0, false, err
	}
	ccw, cw, err := readEncoder(ex, layer, index)
	if err != nil {
		return 0, false, err
	}
	got = ccw
	if dir == 1 {
		got = cw
	}
	if got != code {
		return got, true, &Kept{Got: got}
	}
	return got, true, nil
}

// Kept is a write the keyboard answered but did not store.
type Kept struct {
	Got uint16
}

func (e *Kept) Error() string {
	if e == nil {
		return "keyboard kept the previous key"
	}
	return fmt.Sprintf("keyboard kept %s (%04X)", keyLabel(e.Got), e.Got)
}

func readKeycode(ex Exchanger, layer, row, col int) (uint16, error) {
	req, err := getKeycode(layer, row, col)
	if err != nil {
		return 0, err
	}
	resp, err := call(ex, req)
	if err != nil {
		return 0, err
	}
	if len(resp) == 0 || resp[0] != cmdGetKeycode {
		return 0, fmt.Errorf("keycode reply is not a read")
	}
	return parseKeycode(resp)
}

func keyLabel(code uint16) string {
	if code == 0 {
		return "No"
	}
	return KeyName(code)
}

// Read loads the definition and keymap.
func Read(ex Exchanger) (*Keyboard, error) {
	viaResp, err := call(ex, getProtocolVersion())
	if err != nil {
		return nil, err
	}
	via, err := parseViaProtocol(viaResp)
	if err != nil {
		return nil, err
	}
	if via != 9 {
		return nil, fmt.Errorf("unsupported VIA protocol %d", via)
	}

	idResp, err := call(ex, getKeyboardID())
	if err != nil {
		return nil, err
	}
	vial, uid, err := parseKeyboardID(idResp)
	if err != nil {
		return nil, err
	}
	if vial > 6 {
		return nil, fmt.Errorf("unsupported Vial protocol %d", vial)
	}
	if uid == [8]byte{} {
		return nil, fmt.Errorf("device did not return a Vial keyboard id")
	}

	sizeResp, err := call(ex, getDefinitionSize())
	if err != nil {
		return nil, err
	}
	size, err := parseDefinitionSize(sizeResp)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, fmt.Errorf("keyboard definition is empty")
	}
	comp := make([]byte, 0, size)
	remain := size
	for block := 0; remain > 0; block++ {
		req, err := definitionRequest(block)
		if err != nil {
			return nil, err
		}
		resp, err := call(ex, req)
		if err != nil {
			return nil, err
		}
		part, err := definitionChunk(resp, remain)
		if err != nil {
			return nil, err
		}
		comp = append(comp, part...)
		remain -= ReportLen
		if remain < 0 {
			remain = 0
		}
	}

	raw, err := decodeXZ(comp)
	if err != nil {
		return nil, err
	}
	kb, err := keyboardFromJSON(raw)
	if err != nil {
		return nil, err
	}
	kb.ViaProtocol = via
	kb.VialProtocol = vial
	kb.UID = uid
	if len(kb.layouts) > 0 || kb.hasOptionalKeys() {
		if bits, err := ReadLayoutOptions(ex); err == nil {
			kb.UseLayoutBits(bits)
		}
	}

	layerResp, err := call(ex, getLayerCount())
	if err != nil {
		return nil, err
	}
	layers, err := parseLayerCount(layerResp)
	if err != nil {
		return nil, err
	}
	kb.Layers = layers

	codes, err := readKeymap(ex, layers, kb.Rows, kb.Cols)
	if err != nil {
		return nil, err
	}
	for _, key := range kb.Keys {
		if !key.HasMatrix {
			continue
		}
		if key.Row >= kb.Rows || key.Col >= kb.Cols {
			return nil, fmt.Errorf("key %d,%d is outside the %dx%d matrix", key.Row, key.Col, kb.Rows, kb.Cols)
		}
	}
	kb.codes = codes
	kb.encN = encoderCount(kb.Keys)
	if kb.encN > 0 {
		enc, err := readEncoders(ex, layers, kb.encN)
		if err != nil {
			return nil, err
		}
		kb.enc = enc
	}
	return kb, nil
}

func readKeymap(ex Exchanger, layers, rows, cols int) ([]uint16, error) {
	size := layers * rows * cols * 2
	if size <= 0 {
		return nil, fmt.Errorf("keymap size %d", size)
	}
	buf := make([]byte, 0, size)
	for offset := 0; offset < size; {
		n := size - offset
		if n > Chunk {
			n = Chunk
		}
		req, err := keymapRequest(offset, n)
		if err != nil {
			return nil, err
		}
		resp, err := call(ex, req)
		if err != nil {
			return nil, err
		}
		part, err := keymapChunk(resp, n)
		if err != nil {
			return nil, err
		}
		buf = append(buf, part...)
		offset += n
	}
	codes := make([]uint16, layers*rows*cols)
	for i := range codes {
		codes[i] = binary.BigEndian.Uint16(buf[i*2:])
	}
	return codes, nil
}

func call(ex Exchanger, msg []byte) ([]byte, error) {
	report, err := pad(msg)
	if err != nil {
		return nil, err
	}
	resp, err := ex.Exchange(report)
	if err != nil {
		return nil, err
	}
	return reportOf(resp)
}

func reportOf(resp []byte) ([]byte, error) {
	return report(resp)
}

func decodeXZ(comp []byte) ([]byte, error) {
	r, err := xz.NewReader(bytes.NewReader(comp))
	if err != nil {
		return nil, fmt.Errorf("definition: %w", err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("definition: %w", err)
	}
	return raw, nil
}

type definition struct {
	Name   string `json:"name"`
	Matrix struct {
		Rows int `json:"rows"`
		Cols int `json:"cols"`
	} `json:"matrix"`
	Layouts struct {
		Keymap json.RawMessage `json:"keymap"`
		Labels json.RawMessage `json:"labels"`
	} `json:"layouts"`
	Custom []struct {
		Name      string `json:"name"`
		ShortName string `json:"shortName"`
	} `json:"customKeycodes"`
}

func keyboardFromJSON(raw []byte) (*Keyboard, error) {
	var def definition
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("definition: %w", err)
	}
	if def.Matrix.Rows < 1 || def.Matrix.Cols < 1 {
		return nil, fmt.Errorf("matrix is %dx%d", def.Matrix.Rows, def.Matrix.Cols)
	}
	keys, err := parseLayout(def.Layouts.Keymap)
	if err != nil {
		return nil, err
	}
	kb := &Keyboard{Name: def.Name, Rows: def.Matrix.Rows, Cols: def.Matrix.Cols, Keys: keys, layouts: parseLayoutOptions(def.Layouts.Labels)}
	kb.choices = make([]int, len(kb.layouts))
	for _, custom := range def.Custom {
		name := custom.ShortName
		if name == "" {
			name = custom.Name
		}
		kb.Custom = append(kb.Custom, name)
	}
	return kb, nil
}
