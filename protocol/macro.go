package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// vialAdvancedMacros is the Vial protocol that stores a macro as QMK
// send-string bytes. Older keyboards store one-byte taps and text only.
const vialAdvancedMacros = 2

const (
	// A macro action begins with QMK's send-string prefix. A short key is one
	// byte; an extended key is a little-endian keycode.
	macroPrefix = 0x01
	actTap      = 0x01
	actDown     = 0x02
	actUp       = 0x03
	actDelay    = 0x04
	actTapExt   = 0x05
	actDownExt  = 0x06
	actUpExt    = 0x07

	// MaxMacroDelay is the longest wait a macro can store. A delay is
	// (d%255)+1 and (d/255)+1, so that is 254*255+254 milliseconds.
	MaxMacroDelay = 254*255 + 254
)

// ActionKind is what one step of a macro does.
type ActionKind int

const (
	ActionText ActionKind = iota
	ActionTap
	ActionDown
	ActionUp
	ActionDelay
)

// Action is one step of a macro.
// Text is the characters typed. Each character is one byte from 2 through 255:
// 0 ends the macro and 1 starts a key action. Bytes 128 through 255 are one
// character each, not UTF-8.
// Keys are the keycodes of a tap, a press, or a release. Consecutive key
// actions of the same kind are one action, because the keyboard stores them
// the same either way.
// Delay is a wait in milliseconds, from 0 through 65024.
type Action struct {
	Kind  ActionKind
	Text  string
	Keys  []uint16
	Delay int
}

// Macro is one slot. An empty macro does nothing.
type Macro []Action

// TooBigError is a macro that does not fit in the keyboard's buffer.
// Nothing was written.
type TooBigError struct {
	Need int
	Have int
}

func (e *TooBigError) Error() string {
	return fmt.Sprintf("macros need %d bytes, keyboard has %d", e.Need, e.Have)
}

// LoadMacros reads every macro. memory is the keyboard's buffer size.
// A keyboard with no slots returns no macros. vialProtocol selects the byte
// format: 2 and later include presses, releases, waits, and keycodes above 255.
func LoadMacros(ex Exchanger, vialProtocol uint32) (macros []Macro, memory int, err error) {
	count, err := readMacroCount(ex)
	if err != nil || count == 0 {
		return nil, 0, err
	}
	resp, err := call(ex, getMacroBufferSize())
	if err != nil {
		return nil, 0, err
	}
	memory, err = parseMacroBufferSize(resp)
	if err != nil {
		return nil, 0, err
	}
	if memory == 0 {
		return make([]Macro, count), 0, nil
	}
	buf, err := readMacroBytes(ex, memory)
	if err != nil {
		return nil, 0, err
	}
	return decodeMacros(buf, count, vialProtocol >= vialAdvancedMacros), memory, nil
}

// SaveMacros writes every macro. It sends nothing when the actions cannot be
// stored: the count differs from the keyboard, a step does not fit the
// keyboard's format, or the bytes do not fit its buffer. The keyboard keeps
// whatever was already there.
func SaveMacros(ex Exchanger, vialProtocol uint32, macros []Macro) error {
	data, err := encodeMacros(macros, vialProtocol >= vialAdvancedMacros)
	if err != nil {
		return err
	}
	count, err := readMacroCount(ex)
	if err != nil {
		return err
	}
	if len(macros) != count {
		return fmt.Errorf("keyboard has %d macros, not %d", count, len(macros))
	}
	if count == 0 {
		return nil
	}
	resp, err := call(ex, getMacroBufferSize())
	if err != nil {
		return err
	}
	memory, err := parseMacroBufferSize(resp)
	if err != nil {
		return err
	}
	if len(data) > memory {
		return &TooBigError{Need: len(data), Have: memory}
	}
	for offset := 0; offset < len(data); {
		n := len(data) - offset
		if n > Chunk {
			n = Chunk
		}
		req, err := setMacroBuffer(offset, data[offset:offset+n])
		if err != nil {
			return err
		}
		if _, err = call(ex, req); err != nil {
			return err
		}
		offset += n
	}
	return nil
}

func readMacroBytes(ex Exchanger, memory int) ([]byte, error) {
	buf := make([]byte, 0, memory)
	for offset := 0; offset < memory; {
		n := memory - offset
		if n > Chunk {
			n = Chunk
		}
		req, err := macroBufferRequest(offset, n)
		if err != nil {
			return nil, err
		}
		resp, err := call(ex, req)
		if err != nil {
			return nil, err
		}
		part, err := macroBufferChunk(resp, n)
		if err != nil {
			return nil, err
		}
		buf = append(buf, part...)
		offset += n
	}
	return buf, nil
}

func decodeMacros(buf []byte, count int, advanced bool) []Macro {
	parts := bytes.Split(buf, []byte{0})
	for len(parts) < count {
		parts = append(parts, nil)
	}
	parts = parts[:count]
	macros := make([]Macro, count)
	for i, part := range parts {
		macros[i] = decodeActions(part, advanced)
	}
	return macros
}

func decodeActions(data []byte, advanced bool) []Action {
	var out []Action
	var text []byte
	var keys []uint16
	var keyKind byte
	flush := func() {
		if len(text) > 0 {
			out = append(out, Action{Kind: ActionText, Text: latin1(text)})
			text = nil
		}
		if len(keys) == 0 {
			return
		}
		kind := ActionTap
		switch keyKind {
		case actDown:
			kind = ActionDown
		case actUp:
			kind = ActionUp
		}
		out = append(out, Action{Kind: kind, Keys: keys})
		keys = nil
	}
	addKey := func(kind byte, code uint16) {
		if len(text) > 0 || (len(keys) > 0 && keyKind != kind) {
			flush()
		}
		keyKind = kind
		keys = append(keys, code)
	}
loop:
	for i := 0; i < len(data); {
		if advanced && data[i] == macroPrefix {
			if i+1 >= len(data) {
				break loop
			}
			switch data[i+1] {
			case actTap, actDown, actUp:
				if i+2 >= len(data) {
					break loop
				}
				addKey(data[i+1], uint16(data[i+2]))
				i += 3
			case actTapExt, actDownExt, actUpExt:
				if i+3 >= len(data) {
					break loop
				}
				code := binary.LittleEndian.Uint16(data[i+2 : i+4])
				if code > 0xFF00 {
					code = (code & 0xFF) << 8
				}
				// 5, 6 and 7 are the two-byte forms of tap, down and up.
				addKey(data[i+1]-(actTapExt-actTap), code)
				i += 4
			case actDelay:
				if i+3 >= len(data) {
					break loop
				}
				flush()
				delay := int(data[i+2]) - 1 + (int(data[i+3])-1)*255
				out = append(out, Action{Kind: ActionDelay, Delay: delay})
				i += 4
			default:
				i += 2
			}
			continue
		}
		if !advanced && (data[i] == actTap || data[i] == actDown || data[i] == actUp) {
			if i+1 >= len(data) {
				break
			}
			addKey(data[i], uint16(data[i+1]))
			i += 2
			continue
		}
		if len(keys) > 0 {
			flush()
		}
		text = append(text, data[i])
		i++
	}
	flush()
	return out
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func encodeMacros(macros []Macro, advanced bool) ([]byte, error) {
	parts := make([][]byte, len(macros))
	for i, macro := range macros {
		part, err := encodeActions(macro, advanced)
		if err != nil {
			return nil, fmt.Errorf("macro %d: %w", i, err)
		}
		parts[i] = part
	}
	return append(bytes.Join(parts, []byte{0}), 0), nil
}

func encodeActions(actions []Action, advanced bool) ([]byte, error) {
	var out []byte
	for i, action := range actions {
		switch action.Kind {
		case ActionText:
			if len(action.Keys) != 0 || action.Delay != 0 {
				return nil, fmt.Errorf("macro text action %d is not only text", i)
			}
			raw, err := textBytes(action.Text)
			if err != nil {
				return nil, err
			}
			out = append(out, raw...)
		case ActionTap, ActionDown, ActionUp:
			if action.Text != "" || action.Delay != 0 || len(action.Keys) == 0 {
				return nil, fmt.Errorf("macro key action %d is empty", i)
			}
			for _, code := range action.Keys {
				part, err := encodeKey(action.Kind, code, advanced)
				if err != nil {
					return nil, err
				}
				out = append(out, part...)
			}
		case ActionDelay:
			if action.Text != "" || len(action.Keys) != 0 {
				return nil, fmt.Errorf("macro delay action %d is not only a wait", i)
			}
			if !advanced {
				return nil, fmt.Errorf("a delay needs the newer macro format")
			}
			if action.Delay < 0 || action.Delay > MaxMacroDelay {
				return nil, fmt.Errorf("macro delay %d is outside 0 through %d", action.Delay, MaxMacroDelay)
			}
			out = append(out, macroPrefix, actDelay, byte(action.Delay%255+1), byte(action.Delay/255+1))
		default:
			return nil, fmt.Errorf("macro action %d is unknown", i)
		}
	}
	return out, nil
}

func textBytes(text string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("macro text is empty")
	}
	out := make([]byte, 0, len(text))
	for _, r := range text {
		if r < 2 || r > 255 {
			return nil, fmt.Errorf("macro text must be bytes 2 through 255")
		}
		out = append(out, byte(r))
	}
	return out, nil
}

func encodeKey(kind ActionKind, code uint16, advanced bool) ([]byte, error) {
	short, ext := byte(actTap), byte(actTapExt)
	switch kind {
	case ActionDown:
		short, ext = actDown, actDownExt
	case ActionUp:
		short, ext = actUp, actUpExt
	}
	if !advanced {
		if code > 0xFF {
			return nil, fmt.Errorf("macro keycode %04X needs the newer macro format", code)
		}
		return []byte{short, byte(code)}, nil
	}
	if code < 256 {
		return []byte{macroPrefix, short, byte(code)}, nil
	}
	wire := code
	if code%256 == 0 {
		wire = 0xFF00 | (code >> 8)
	}
	out := []byte{macroPrefix, ext, 0, 0}
	binary.LittleEndian.PutUint16(out[2:], wire)
	return out, nil
}
