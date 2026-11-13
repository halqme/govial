//go:build darwin

package hid

import "testing"

func TestListRawHID(t *testing.T) {
	infos, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		t.Logf("%s vid=%04X pid=%04X serial=%q entry=%d", info.Name, info.VendorID, info.ProductID, info.Serial, info.Entry)
	}
}
