package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Key is one physical key from a Vial layout, in keyboard-layout-editor units.
// X and Y are the top left before rotation. Rotation is clockwise degrees
// around (RotX, RotY), matching keyboard-layout-editor.
type Key struct {
	X, Y, W, H float64
	Rot        float64
	RotX, RotY float64
	Labels     []string
	Decal      bool

	HasMatrix            bool
	Row, Col             int
	Encoder              bool
	EncoderIndex, EncDir int

	// HasOption is set when this key is one arrangement of a VIA layout
	// option. Choice 0 is the arrangement shown until another is selected.
	HasOption      bool
	Option, Choice int
}

type cursor struct {
	x, y      float64
	w, h      float64
	r, rx, ry float64
	align     int
	decal     bool
}

func (c *cursor) resetProps() {
	c.w, c.h = 1, 1
	c.decal = false
}

// Contains reports whether a point in the same unit space lies on the key,
// after undoing its clockwise rotation.
func (k Key) Contains(px, py float64) bool {
	x, y := px, py
	if k.Rot != 0 {
		// Inverse of the clockwise rotation MyGo and keyboard-layout-editor draw:
		// x' = cos x - sin y, y' = sin x + cos y.
		rad := k.Rot * math.Pi / 180
		c, s := math.Cos(rad), math.Sin(rad)
		dx, dy := px-k.RotX, py-k.RotY
		x = dx*c + dy*s + k.RotX
		y = -dx*s + dy*c + k.RotY
	}
	return x >= k.X && y >= k.Y && x < k.X+k.W && y < k.Y+k.H
}

func parseLayout(raw json.RawMessage) ([]Key, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("layout is empty")
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("layout: %w", err)
	}
	cur := cursor{w: 1, h: 1}
	var keys []Key
	for _, row := range rows {
		row = bytes.TrimSpace(row)
		if len(row) == 0 || row[0] != '[' {
			continue
		}
		var items []json.RawMessage
		if err := json.Unmarshal(row, &items); err != nil {
			return nil, fmt.Errorf("layout row: %w", err)
		}
		for _, item := range items {
			item = bytes.TrimSpace(item)
			if len(item) == 0 {
				continue
			}
			if item[0] == '{' {
				if err := applyProps(&cur, item); err != nil {
					return nil, err
				}
				continue
			}
			var label string
			if err := json.Unmarshal(item, &label); err != nil {
				return nil, fmt.Errorf("key label: %w", err)
			}
			keys = append(keys, keyFromLabel(cur, label))
			cur.x += cur.w
			cur.resetProps()
		}
		cur.y++
		cur.x = cur.rx
	}
	return keys, nil
}

func keyFromLabel(cur cursor, label string) Key {
	k := Key{
		X: cur.x, Y: cur.y, W: cur.w, H: cur.h,
		Rot: cur.r, RotX: cur.rx, RotY: cur.ry,
		Decal:  cur.decal,
		Labels: strings.Split(label, "\n"),
	}
	slots := legendSlots(k.Labels, cur.align)
	if group, choice, ok := layoutGroup(k.Labels, cur.align); ok {
		k.HasOption = true
		k.Option = group
		k.Choice = choice
	}
	if strings.EqualFold(slots[4], "e") {
		if idx, dir, ok := splitPair(slots[0]); ok && idx >= 0 && (dir == 0 || dir == 1) {
			k.Encoder = true
			k.EncoderIndex = idx
			k.EncDir = dir
			return k
		}
	}
	if row, col, ok := splitPair(slots[0]); ok && row >= 0 && col >= 0 {
		k.HasMatrix = true
		k.Row = row
		k.Col = col
	}
	return k
}

func splitPair(s string) (int, int, bool) {
	left, right, ok := strings.Cut(s, ",")
	if !ok {
		return 0, 0, false
	}
	a, errA := strconv.Atoi(strings.TrimSpace(left))
	b, errB := strconv.Atoi(strings.TrimSpace(right))
	if errA != nil || errB != nil {
		return 0, 0, false
	}
	return a, b, true
}

func applyProps(cur *cursor, raw json.RawMessage) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("key properties: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("key properties are not an object")
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		name, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("key property name is not a string")
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return err
		}
		if err := applyProp(cur, name, value); err != nil {
			return err
		}
	}
	return nil
}

func applyProp(cur *cursor, name string, raw json.RawMessage) error {
	switch name {
	case "d":
		cur.decal = true
		return nil
	case "a", "r", "rx", "ry", "x", "y", "w", "h":
	default:
		return nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return fmt.Errorf("key property %s: %w", name, err)
	}
	switch name {
	case "a":
		cur.align = int(n)
	case "r":
		cur.r = n
	case "rx":
		cur.rx = n
		cur.x = n
		cur.y = cur.ry
	case "ry":
		cur.ry = n
		cur.x = cur.rx
		cur.y = n
	case "x":
		cur.x += n
	case "y":
		cur.y += n
	case "w":
		cur.w = n
	case "h":
		cur.h = n
	}
	return nil
}

// labelAlign maps a legend's place in the key text to keyboard-layout-editor's
// twelve label slots. Vial starts at center-front, the fifth row. Slot 4 is
// the encoder mark and slot 8 is the layout option.
var labelAlign = [8][12]int{
	{0, 6, 2, 8, 9, 11, 3, 5, 1, 4, 7, 10},
	{1, 7, -1, -1, 9, 11, 4, -1, -1, -1, -1, 10},
	{3, -1, 5, -1, 9, 11, -1, -1, 4, -1, -1, 10},
	{4, -1, -1, -1, 9, 11, -1, -1, -1, -1, -1, 10},
	{0, 6, 2, 8, 10, -1, 3, 5, 1, 4, 7, -1},
	{1, 7, -1, -1, 10, -1, 4, -1, -1, -1, -1, -1},
	{3, -1, 5, -1, 10, -1, -1, -1, 4, -1, -1, -1},
	{4, -1, -1, -1, 10, -1, -1, -1, -1, -1, -1, -1},
}

// layoutGroup reads a VIA layout option from the key's legends.
// A key with no option legend is not part of one.
func legendSlots(labels []string, align int) [12]string {
	if align < 0 || align >= len(labelAlign) {
		align = 4
	}
	var slot [12]string
	for i, label := range labels {
		if i >= len(labelAlign[align]) {
			break
		}
		n := labelAlign[align][i]
		if n >= 0 && n < len(slot) {
			slot[n] = strings.TrimSpace(label)
		}
	}
	return slot
}

func layoutGroup(labels []string, align int) (group, choice int, ok bool) {
	slot := legendSlots(labels, align)
	if slot[8] == "" {
		return 0, 0, false
	}
	group, choice, ok = splitPair(slot[8])
	if !ok || group < 0 || choice < 0 {
		return 0, 0, false
	}
	return group, choice, true
}
