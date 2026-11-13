//go:build darwin

package hid

import (
	"strings"
	"testing"

	"github.com/halqme/govial/protocol"
)

func TestReadConnectedVial(t *testing.T) {
	infos, err := List()
	if err != nil {
		t.Fatal(err)
	}
	var info Info
	found := false
	for _, candidate := range infos {
		if strings.Contains(candidate.Serial, "vial:f64c2b3c") {
			info = candidate
			found = true
			break
		}
	}
	if !found {
		t.Skip("no Vial keyboard connected")
	}
	dev, err := Open(info)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	kb, err := protocol.Read(dev)
	if err != nil {
		t.Fatal(err)
	}
	if kb.Layers < 1 || len(kb.Keys) == 0 {
		t.Fatalf("empty keyboard %+v", kb)
	}
	t.Logf("%s uid=%s via=%d vial=%d layers=%d keys=%d", kb.Name, kb.UIDString(), kb.ViaProtocol, kb.VialProtocol, kb.Layers, len(kb.Keys))
	lock, err := protocol.Security(dev)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("unlocked=%v pending=%v hold=%v", lock.Open, lock.Pending, lock.Keys)
}
