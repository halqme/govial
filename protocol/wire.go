// Package protocol speaks the Vial raw-HID report format.
// A report is 32 bytes. Multi-byte integers are encoded as the keyboard
// firmware encodes them: VIA keymap fields are big-endian, and the Vial
// keyboard id and definition block index are little-endian.
package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const ReportLen = 32

const (
	cmdGetProtocolVersion = 0x01
	cmdGetKeyboardValue   = 0x02
	cmdSetKeyboardValue   = 0x03
	cmdGetKeycode         = 0x04
	cmdSetKeycode         = 0x05
	cmdMacroGetCount      = 0x0C
	cmdGetLayerCount      = 0x11
	cmdKeymapGetBuffer    = 0x12
	cmdVialPrefix         = 0xFE

	vialGetKeyboardID = 0x00
	vialGetSize       = 0x01
	vialGetDefinition = 0x02
	vialGetEncoder    = 0x03
	vialSetEncoder    = 0x04
	vialUnlockStatus  = 0x05
	vialUnlockStart   = 0x06
	vialUnlockPoll    = 0x07
	vialLock          = 0x08
)

// Chunk is how many keymap bytes one get-buffer report carries.
const Chunk = 28

// ErrShort is returned when a device response is not a full report.
var ErrShort = errors.New("short vial report")

func pad(msg []byte) ([]byte, error) {
	if len(msg) > ReportLen {
		return nil, fmt.Errorf("vial report is %d bytes, limit is %d", len(msg), ReportLen)
	}
	out := make([]byte, ReportLen)
	copy(out, msg)
	return out, nil
}

func report(resp []byte) ([]byte, error) {
	if len(resp) == 0 {
		return nil, ErrShort
	}
	if len(resp) >= ReportLen {
		return resp[:ReportLen], nil
	}
	out := make([]byte, ReportLen)
	copy(out, resp)
	return out, nil
}

func getProtocolVersion() []byte { return []byte{cmdGetProtocolVersion} }

func parseViaProtocol(resp []byte) (int, error) {
	resp, err := report(resp)
	if err != nil {
		return 0, err
	}
	return int(binary.BigEndian.Uint16(resp[1:3])), nil
}

func getKeyboardID() []byte { return []byte{cmdVialPrefix, vialGetKeyboardID} }

func parseKeyboardID(resp []byte) (protocol uint32, uid [8]byte, err error) {
	resp, err = report(resp)
	if err != nil {
		return 0, uid, err
	}
	protocol = binary.LittleEndian.Uint32(resp[0:4])
	copy(uid[:], resp[4:12])
	return protocol, uid, nil
}

func getDefinitionSize() []byte { return []byte{cmdVialPrefix, vialGetSize} }

func parseDefinitionSize(resp []byte) (int, error) {
	resp, err := report(resp)
	if err != nil {
		return 0, err
	}
	n := binary.LittleEndian.Uint32(resp[0:4])
	if n > 1<<20 {
		return 0, fmt.Errorf("vial definition is %d bytes", n)
	}
	return int(n), nil
}

// definitionRequest asks for one 32-byte block. block is the block index,
// not a byte offset.
func definitionRequest(block int) ([]byte, error) {
	if block < 0 {
		return nil, fmt.Errorf("definition block %d", block)
	}
	msg := []byte{cmdVialPrefix, vialGetDefinition, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(msg[2:], uint32(block))
	return msg, nil
}

// definitionChunk returns the next bytes of a definition. remain is how many
// bytes of the definition are still unread; the device still sends a full report.
func definitionChunk(resp []byte, remain int) ([]byte, error) {
	resp, err := report(resp)
	if err != nil {
		return nil, err
	}
	if remain < 0 {
		return nil, fmt.Errorf("definition remaining %d", remain)
	}
	n := remain
	if n > ReportLen {
		n = ReportLen
	}
	out := make([]byte, n)
	copy(out, resp[:n])
	return out, nil
}

func getMacroCount() []byte { return []byte{cmdMacroGetCount} }

// parseMacroCount reads the VIA macro-count reply. A reply that is not that
// command means the keyboard has no macro slots to offer.
func parseMacroCount(resp []byte) (int, error) {
	resp, err := report(resp)
	if err != nil {
		return 0, err
	}
	if resp[0] != cmdMacroGetCount {
		return 0, nil
	}
	n := int(resp[1])
	if n > 128 {
		n = 128
	}
	return n, nil
}

func getLayerCount() []byte { return []byte{cmdGetLayerCount} }

func parseLayerCount(resp []byte) (int, error) {
	resp, err := report(resp)
	if err != nil {
		return 0, err
	}
	n := int(resp[1])
	if n < 1 || n > 16 {
		return 0, fmt.Errorf("layer count %d", n)
	}
	return n, nil
}

func keymapRequest(offset, n int) ([]byte, error) {
	if offset < 0 || offset > 0xFFFF || n < 1 || n > Chunk {
		return nil, fmt.Errorf("keymap read offset %d length %d", offset, n)
	}
	return []byte{cmdKeymapGetBuffer, byte(offset >> 8), byte(offset), byte(n)}, nil
}

func keymapChunk(resp []byte, n int) ([]byte, error) {
	resp, err := report(resp)
	if err != nil {
		return nil, err
	}
	if n < 1 || n > Chunk {
		return nil, fmt.Errorf("keymap chunk %d", n)
	}
	out := make([]byte, n)
	copy(out, resp[4:4+n])
	return out, nil
}

func setKeycode(layer, row, col int, code uint16) ([]byte, error) {
	if layer < 0 || row < 0 || col < 0 || layer > 0xFF || row > 0xFF || col > 0xFF {
		return nil, fmt.Errorf("key %d %d,%d", layer, row, col)
	}
	return []byte{cmdSetKeycode, byte(layer), byte(row), byte(col), byte(code >> 8), byte(code)}, nil
}

func getKeycode(layer, row, col int) ([]byte, error) {
	if layer < 0 || row < 0 || col < 0 || layer > 0xFF || row > 0xFF || col > 0xFF {
		return nil, fmt.Errorf("key %d %d,%d", layer, row, col)
	}
	return []byte{cmdGetKeycode, byte(layer), byte(row), byte(col)}, nil
}

func parseKeycode(resp []byte) (uint16, error) {
	resp, err := report(resp)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(resp[4:6]), nil
}

func unlockStatus() []byte { return []byte{cmdVialPrefix, vialUnlockStatus} }

// parseUnlockStatus reads the lock flag and the matrix positions that must
// be held. The firmware fills unused positions with 0xFF.
func parseUnlockStatus(resp []byte) (open, pending bool, keys []KeyPos, err error) {
	resp, err = report(resp)
	if err != nil {
		return false, false, nil, err
	}
	open = resp[0] == 1
	pending = resp[1] == 1
	for i := 0; i < 15; i++ {
		row, col := int(resp[2+i*2]), int(resp[3+i*2])
		if row == 0xFF || col == 0xFF {
			break
		}
		keys = append(keys, KeyPos{Row: row, Col: col})
	}
	return open, pending, keys, nil
}

func unlockStart() []byte { return []byte{cmdVialPrefix, vialUnlockStart} }

func unlockPoll() []byte { return []byte{cmdVialPrefix, vialUnlockPoll} }

func parseUnlockPoll(resp []byte) (open bool, left int, err error) {
	resp, err = report(resp)
	if err != nil {
		return false, 0, err
	}
	return resp[0] == 1, int(resp[2]), nil
}

func lockKeyboard() []byte { return []byte{cmdVialPrefix, vialLock} }

const keyboardLayoutOptions = 0x02

func getLayoutOptions() []byte {
	return []byte{cmdGetKeyboardValue, keyboardLayoutOptions}
}

func parseLayoutOptionBits(resp []byte) (uint32, error) {
	resp, err := report(resp)
	if err != nil {
		return 0, err
	}
	if resp[0] != cmdGetKeyboardValue || resp[1] != keyboardLayoutOptions {
		return 0, fmt.Errorf("keyboard did not return layout options")
	}
	return binary.BigEndian.Uint32(resp[2:6]), nil
}

func setLayoutOptions(bits uint32) []byte {
	msg := []byte{cmdSetKeyboardValue, keyboardLayoutOptions, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(msg[2:6], bits)
	return msg
}

func getEncoder(layer, index int) ([]byte, error) {
	if layer < 0 || layer > 255 || index < 0 || index > 255 {
		return nil, fmt.Errorf("encoder %d,%d is outside the keymap", layer, index)
	}
	return []byte{cmdVialPrefix, vialGetEncoder, byte(layer), byte(index)}, nil
}

func parseEncoder(resp []byte) (ccw, cw uint16, err error) {
	resp, err = report(resp)
	if err != nil {
		return 0, 0, err
	}
	if resp[0] == cmdVialPrefix && resp[1] == vialGetEncoder {
		return 0, 0, fmt.Errorf("keyboard did not return encoder keys")
	}
	return binary.BigEndian.Uint16(resp[0:2]), binary.BigEndian.Uint16(resp[2:4]), nil
}

func setEncoder(layer, index, dir int, code uint16) ([]byte, error) {
	if layer < 0 || layer > 255 || index < 0 || index > 255 || (dir != 0 && dir != 1) {
		return nil, fmt.Errorf("encoder %d %d,%d is outside the keymap", layer, index, dir)
	}
	msg := []byte{cmdVialPrefix, vialSetEncoder, byte(layer), byte(index), byte(dir), 0, 0}
	binary.BigEndian.PutUint16(msg[5:7], code)
	return msg, nil
}

func readEncoder(ex Exchanger, layer, index int) (ccw, cw uint16, err error) {
	req, err := getEncoder(layer, index)
	if err != nil {
		return 0, 0, err
	}
	resp, err := call(ex, req)
	if err != nil {
		return 0, 0, err
	}
	return parseEncoder(resp)
}

func readEncoders(ex Exchanger, layers, n int) ([]uint16, error) {
	out := make([]uint16, layers*n*2)
	for layer := 0; layer < layers; layer++ {
		for idx := 0; idx < n; idx++ {
			ccw, cw, err := readEncoder(ex, layer, idx)
			if err != nil {
				return nil, err
			}
			base := (layer*n + idx) * 2
			out[base] = ccw
			out[base+1] = cw
		}
	}
	return out, nil
}
