package protocol

import (
	"bytes"
	"encoding/json"
)

// LayoutOption is one VIA layout option, such as where a trackball sits.
// Choices[0] is the default arrangement.
type LayoutOption struct {
	Name    string
	Choices []string
}

func parseLayoutOptions(raw json.RawMessage) []LayoutOption {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	out := make([]LayoutOption, 0, len(items))
	for _, item := range items {
		item = bytes.TrimSpace(item)
		if len(item) == 0 {
			continue
		}
		if item[0] == '[' {
			var names []string
			if err := json.Unmarshal(item, &names); err != nil || len(names) < 2 {
				out = append(out, LayoutOption{})
				continue
			}
			out = append(out, LayoutOption{Name: names[0], Choices: append([]string(nil), names[1:]...)})
			continue
		}
		var name string
		if err := json.Unmarshal(item, &name); err != nil {
			name = ""
		}
		out = append(out, LayoutOption{Name: name, Choices: []string{"オフ", "オン"}})
	}
	return out
}

func (k *Keyboard) hasOptionalKeys() bool {
	if k == nil {
		return false
	}
	for _, key := range k.Keys {
		if key.HasOption {
			return true
		}
	}
	return false
}

// Layouts is the keyboard's layout options, in definition order.
func (k *Keyboard) Layouts() []LayoutOption {
	if k == nil || len(k.layouts) == 0 {
		return nil
	}
	out := make([]LayoutOption, len(k.layouts))
	copy(out, k.layouts)
	return out
}

// Choices is the selected arrangement of each layout option.
func (k *Keyboard) Choices() []int {
	if k == nil || len(k.layouts) == 0 {
		return nil
	}
	out := make([]int, len(k.layouts))
	copy(out, k.choices)
	return out
}

// SetChoice selects one arrangement. It does not send it to the keyboard.
func (k *Keyboard) SetChoice(option, choice int) bool {
	if k == nil || option < 0 || option >= len(k.layouts) {
		return false
	}
	if choice < 0 || choice >= len(k.layouts[option].Choices) {
		return false
	}
	if len(k.choices) != len(k.layouts) {
		k.choices = make([]int, len(k.layouts))
	}
	k.choices[option] = choice
	return true
}

// Shows reports whether the key belongs to the selected arrangements.
// Keys that are not part of an option are always shown. Others are shown
// only for the selected choice, which starts at the default.
func (k *Keyboard) Shows(key Key) bool {
	if !key.HasOption {
		return true
	}
	selected := 0
	if k != nil && key.Option >= 0 && key.Option < len(k.choices) {
		selected = k.choices[key.Option]
	}
	return key.Choice == selected
}

// Place moves an alternate arrangement onto the default one. VIA stores the
// other choices somewhere else in the layout and shifts them onto the
// default choice's top-left key when that choice is selected.
func (k *Keyboard) Place(key Key) Key {
	if k == nil || !key.HasOption || key.Choice == 0 {
		return key
	}
	zx, zy, zok := k.pivot(key.Option, 0)
	hx, hy, hok := k.pivot(key.Option, key.Choice)
	if !zok || !hok {
		return key
	}
	dx, dy := hx-zx, hy-zy
	key.X -= dx
	key.Y -= dy
	key.RotX -= dx
	key.RotY -= dy
	return key
}

func (k *Keyboard) pivot(group, choice int) (float64, float64, bool) {
	k.ensurePivots()
	v, ok := k.pivots[[2]int{group, choice}]
	return v[0], v[1], ok
}

func (k *Keyboard) ensurePivots() {
	if k.pivots != nil {
		return
	}
	k.pivots = map[[2]int][2]float64{}
	seen := map[[2]int]bool{}
	for _, key := range k.Keys {
		if !key.HasOption {
			continue
		}
		id := [2]int{key.Option, key.Choice}
		if seen[id] {
			cur := k.pivots[id]
			if key.Y > cur[1] || (key.Y == cur[1] && key.X >= cur[0]) {
				continue
			}
		}
		k.pivots[id] = [2]float64{key.X, key.Y}
		seen[id] = true
	}
}

// LayoutBits packs the selected choices the way VIA stores them.
func (k *Keyboard) LayoutBits() uint32 {
	if k == nil {
		return 0
	}
	var packed uint32
	for i, opt := range k.layouts {
		bits := uint(choiceBits(len(opt.Choices)))
		choice := uint32(0)
		if i < len(k.choices) && k.choices[i] > 0 {
			choice = uint32(k.choices[i])
		}
		packed = (packed << bits) | choice
	}
	return packed
}

// UseLayoutBits selects the arrangements stored on the keyboard.
func (k *Keyboard) UseLayoutBits(packed uint32) {
	if k == nil || len(k.layouts) == 0 {
		return
	}
	nums := make([]int, len(k.layouts))
	for i, opt := range k.layouts {
		nums[i] = len(opt.Choices)
	}
	k.choices = unpackChoices(packed, nums)
}

func choiceBits(n int) int {
	if n <= 0 {
		return 0
	}
	for i := 0; i < 5; i++ {
		if 2<<i >= n {
			return 1 + i
		}
	}
	return 0
}

func unpackChoices(packed uint32, nums []int) []int {
	out := make([]int, len(nums))
	for i := len(nums) - 1; i >= 0; i-- {
		bits := uint(choiceBits(nums[i]))
		var mask uint32
		if bits > 0 && bits < 32 {
			mask = (1 << bits) - 1
		}
		choice := int(packed & mask)
		if nums[i] > 0 && (choice < 0 || choice >= nums[i]) {
			choice = 0
		}
		out[i] = choice
		packed >>= bits
	}
	return out
}

// ReadLayoutOptions reads the arrangements stored on the keyboard.
func ReadLayoutOptions(ex Exchanger) (uint32, error) {
	resp, err := call(ex, getLayoutOptions())
	if err != nil {
		return 0, err
	}
	return parseLayoutOptionBits(resp)
}

// WriteLayoutOptions stores the packed arrangements on the keyboard.
func WriteLayoutOptions(ex Exchanger, bits uint32) error {
	_, err := call(ex, setLayoutOptions(bits))
	return err
}
