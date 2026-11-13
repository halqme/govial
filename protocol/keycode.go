package protocol

import (
	"fmt"
	"strings"
)

// KeyName is the short label for a keycode in the Vial protocol 6 map,
// which is QMK's current quantum keycode layout. Codes this map does not
// name stay as four hex digits.
func KeyName(code uint16) string {
	if name, ok := exactNames[code]; ok {
		return name
	}
	switch {
	case code <= 0x00FF:
		return basicName(byte(code), false)
	case code <= 0x1FFF:
		return modifiedName(code)
	case code <= 0x3FFF:
		mods := byte((code >> 8) & 0x1F)
		return modBits(mods) + "/" + basicName(byte(code), true)
	case code <= 0x4FFF:
		return fmt.Sprintf("LT(%d,%s)", (code>>8)&0xF, basicName(byte(code), true))
	case code <= 0x51FF:
		return fmt.Sprintf("LM(%d,%s)", (code>>5)&0xF, modBits(byte(code&0x1F)))
	case code <= 0x521F:
		return fmt.Sprintf("TO(%d)", code&0x1F)
	case code <= 0x523F:
		return fmt.Sprintf("MO(%d)", code&0x1F)
	case code <= 0x525F:
		return fmt.Sprintf("DF(%d)", code&0x1F)
	case code <= 0x527F:
		return fmt.Sprintf("TG(%d)", code&0x1F)
	case code <= 0x529F:
		return fmt.Sprintf("OSL(%d)", code&0x1F)
	case code <= 0x52BF:
		return "OSM(" + modBits(byte(code&0x1F)) + ")"
	case code <= 0x52DF:
		return fmt.Sprintf("TT(%d)", code&0x1F)
	case code <= 0x52FF:
		return fmt.Sprintf("PDF(%d)", code&0x1F)
	case code >= 0x5600 && code <= 0x56EF:
		return "SH(" + basicName(byte(code), true) + ")"
	case code >= 0x5700 && code <= 0x57FF:
		return fmt.Sprintf("TD(%d)", code&0xFF)
	case code >= 0x7700 && code <= 0x777F:
		return fmt.Sprintf("M%d", code-0x7700)
	case code >= 0x7E00 && code <= 0x7E3F:
		return fmt.Sprintf("USER%02d", code-0x7E00)
	default:
		return fmt.Sprintf("%04X", code)
	}
}

// MomentaryLayer is MO(layer): the layer stays while the key is held.
// Layers are 0 through 31.
func MomentaryLayer(layer int) (uint16, bool) {
	return layerCode(0x5220, layer, 31)
}

// ToggleLayer is TG(layer).
func ToggleLayer(layer int) (uint16, bool) {
	return layerCode(0x5260, layer, 31)
}

// ToLayer is TO(layer): the key switches to that layer.
func ToLayer(layer int) (uint16, bool) {
	return layerCode(0x5200, layer, 31)
}

// TapToggleLayer is TT(layer).
func TapToggleLayer(layer int) (uint16, bool) {
	return layerCode(0x52C0, layer, 31)
}

// OneShotLayer is OSL(layer).
func OneShotLayer(layer int) (uint16, bool) {
	return layerCode(0x5280, layer, 31)
}

// DefaultLayer is DF(layer).
func DefaultLayer(layer int) (uint16, bool) {
	return layerCode(0x5240, layer, 31)
}

func layerCode(base uint16, layer, max int) (uint16, bool) {
	if layer < 0 || layer > max {
		return 0, false
	}
	return base + uint16(layer), true
}

// LayerTap is LT(layer, tap). The layer is 0 through 15 and tap is one
// basic keycode.
func LayerTap(layer int, tap uint16) (uint16, bool) {
	if layer < 0 || layer > 15 || tap > 0xFF {
		return 0, false
	}
	return 0x4000 | uint16(layer)<<8 | tap, true
}

// MacroKey is macro slot index, 0 through 127.
func MacroKey(index int) (uint16, bool) {
	if index < 0 || index > 127 {
		return 0, false
	}
	return 0x7700 + uint16(index), true
}

func modifiedName(code uint16) string {
	mods := byte((code >> 8) & 0x1F)
	inner := byte(code)
	if inner == 0 {
		return modBits(mods)
	}
	return modBits(mods) + "(" + basicName(inner, true) + ")"
}

func modBits(mods byte) string {
	switch mods {
	case 0x0F:
		return "HYPR"
	case 0x07:
		return "MEH"
	}
	var b strings.Builder
	if mods&0x10 != 0 {
		b.WriteByte('R')
	}
	if mods&0x01 != 0 {
		b.WriteByte('C')
	}
	if mods&0x02 != 0 {
		b.WriteByte('S')
	}
	if mods&0x04 != 0 {
		b.WriteByte('A')
	}
	if mods&0x08 != 0 {
		b.WriteByte('G')
	}
	if b.Len() == 0 {
		return "?"
	}
	return b.String()
}

func basicName(code byte, nested bool) string {
	if code == 0 {
		if nested {
			return "No"
		}
		return ""
	}
	if code == 1 {
		return "▽"
	}
	if name, ok := basicNames[code]; ok {
		return name
	}
	return fmt.Sprintf("%02X", code)
}

var exactNames = map[uint16]string{
	0x56F0: "SW_T",
	0x56F1: "SW_TT",
	0x56F2: "SW_ON",
	0x56F3: "SW_OFF",
	0x56F4: "SH_OFF",
	0x56F5: "SH_ON",
	0x56F6: "SH_OS",
	0x7840: "ROn",
	0x7841: "ROff",
	0x7842: "RTog",
	0x7843: "RNxt",
	0x7844: "RPrv",
	0x7845: "Hue+",
	0x7846: "Hue-",
	0x7847: "Sat+",
	0x7848: "Sat-",
	0x7849: "Bri+",
	0x784A: "Bri-",
	0x784B: "Spd+",
	0x784C: "Spd-",
	0x7C00: "Boot",
	0x7C01: "Reboot",
	0x7C03: "EEClr",
	0x7C73: "CapsWd",
	0x7C79: "Repeat",
	0x7C7A: "AltRep",
	0x7C7B: "LLock",
}

var basicNames = map[byte]string{
	0x04: "A", 0x05: "B", 0x06: "C", 0x07: "D", 0x08: "E", 0x09: "F",
	0x0A: "G", 0x0B: "H", 0x0C: "I", 0x0D: "J", 0x0E: "K", 0x0F: "L",
	0x10: "M", 0x11: "N", 0x12: "O", 0x13: "P", 0x14: "Q", 0x15: "R",
	0x16: "S", 0x17: "T", 0x18: "U", 0x19: "V", 0x1A: "W", 0x1B: "X",
	0x1C: "Y", 0x1D: "Z",
	0x1E: "1", 0x1F: "2", 0x20: "3", 0x21: "4", 0x22: "5",
	0x23: "6", 0x24: "7", 0x25: "8", 0x26: "9", 0x27: "0",
	0x28: "Ent", 0x29: "Esc", 0x2A: "Bksp", 0x2B: "Tab", 0x2C: "Space",
	0x2D: "-", 0x2E: "=", 0x2F: "[", 0x30: "]", 0x31: "\\",
	0x32: "NU#", 0x33: ";", 0x34: "'", 0x35: "`", 0x36: ",", 0x37: ".", 0x38: "/",
	0x39: "Caps",
	0x3A: "F1", 0x3B: "F2", 0x3C: "F3", 0x3D: "F4", 0x3E: "F5", 0x3F: "F6",
	0x40: "F7", 0x41: "F8", 0x42: "F9", 0x43: "F10", 0x44: "F11", 0x45: "F12",
	0x46: "PScr", 0x47: "ScrL", 0x48: "Paus", 0x49: "Ins", 0x4A: "Home", 0x4B: "PgUp",
	0x4C: "Del", 0x4D: "End", 0x4E: "PgDn", 0x4F: "Right", 0x50: "Left", 0x51: "Down", 0x52: "Up",
	0x53: "Num", 0x54: "/", 0x55: "*", 0x56: "-", 0x57: "+", 0x58: "KEnt",
	0x59: "1", 0x5A: "2", 0x5B: "3", 0x5C: "4", 0x5D: "5", 0x5E: "6", 0x5F: "7",
	0x60: "8", 0x61: "9", 0x62: "0", 0x63: ".", 0x64: "NU\\", 0x65: "App", 0x66: "Pwr", 0x67: "=",
	0x68: "F13", 0x69: "F14", 0x6A: "F15", 0x6B: "F16", 0x6C: "F17", 0x6D: "F18",
	0x6E: "F19", 0x6F: "F20", 0x70: "F21", 0x71: "F22", 0x72: "F23", 0x73: "F24",
	0x7F: "Mute", 0x80: "Vol+", 0x81: "Vol-",
	0x87: "Int1", 0x88: "Int2", 0x89: "Int3", 0x8A: "Int4", 0x8B: "Int5",
	0x8C: "Int6", 0x8D: "Int7", 0x8E: "Int8", 0x8F: "Int9",
	0x90: "Lang1", 0x91: "Lang2", 0x92: "Lang3", 0x93: "Lang4", 0x94: "Lang5",
	0x95: "Lang6", 0x96: "Lang7", 0x97: "Lang8", 0x98: "Lang9",
	0xA5: "Pwr", 0xA6: "Sleep", 0xA7: "Wake",
	0xA8: "Mute", 0xA9: "Vol+", 0xAA: "Vol-",
	0xAB: "Next", 0xAC: "Prev", 0xAD: "Stop", 0xAE: "Play", 0xB0: "Eject",
	0xBD: "Bri+", 0xBE: "Bri-",
	0xCD: "MUp", 0xCE: "MDn", 0xCF: "MLt", 0xD0: "MRt",
	0xD1: "MB1", 0xD2: "MB2", 0xD3: "MB3", 0xD4: "MB4",
	0xD5: "MB5", 0xD6: "MB6", 0xD7: "MB7", 0xD8: "MB8",
	0xD9: "WhUp", 0xDA: "WhDn", 0xDB: "WhL", 0xDC: "WhR",
	0xDD: "Acl0", 0xDE: "Acl1", 0xDF: "Acl2",
	0xE0: "LCtl", 0xE1: "LSft", 0xE2: "LAlt", 0xE3: "LGui",
	0xE4: "RCtl", 0xE5: "RSft", 0xE6: "RAlt", 0xE7: "RGui",
}
