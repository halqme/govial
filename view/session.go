package view

import (
	"time"

	"github.com/halqme/govial/hid"
	"github.com/halqme/govial/locales"
	"github.com/halqme/govial/protocol"
)

func (a *App) Refresh() {
	if a.busy {
		return
	}
	a.busy = true
	a.clearFault()
	a.gen++
	gen := a.gen
	entry := a.entry
	go func() {
		infos, err := hid.List()
		if err != nil {
			a.finish(gen, nil, nil, nil, protocol.Unlock{}, err, entry, false)
			return
		}
		if len(infos) == 0 {
			a.finish(gen, infos, nil, nil, protocol.Unlock{}, nil, 0, true)
			return
		}
		info := infos[0]
		for _, candidate := range infos {
			if candidate.Entry == entry {
				info = candidate
				break
			}
		}
		a.load(gen, infos, info)
	}()
}

func (a *App) open(info hid.Info) {
	if a.busy {
		return
	}
	a.busy = true
	a.clearFault()
	a.gen++
	gen := a.gen
	go func() {
		a.load(gen, nil, info)
	}()
}

func (a *App) load(gen int, infos []hid.Info, info hid.Info) {
	dev, err := hid.Open(info)
	if err != nil {
		a.finish(gen, infos, nil, nil, protocol.Unlock{}, err, info.Entry, true)
		return
	}
	kb, err := protocol.Read(dev)
	lock, lockErr := protocol.Security(dev)
	if err != nil {
		if lockErr == nil && lock.Pending {
			a.finish(gen, infos, dev, nil, lock, nil, info.Entry, true)
			return
		}
		dev.Close()
		a.finish(gen, infos, nil, nil, protocol.Unlock{}, err, info.Entry, true)
		return
	}
	if lockErr != nil {
		dev.Close()
		a.finish(gen, infos, nil, nil, protocol.Unlock{}, lockErr, info.Entry, true)
		return
	}
	a.finish(gen, infos, dev, kb, lock, nil, info.Entry, true)
}

func (a *App) finish(gen int, infos []hid.Info, dev *hid.Device, kb *protocol.Keyboard, lock protocol.Unlock, err error, entry uint64, loaded bool) {
	apply := func() {
		if a.gen != gen {
			if dev != nil {
				dev.Close()
			}
			return
		}
		if infos != nil {
			a.infos = infos
		}
		if loaded {
			if a.dev != nil {
				a.dev.Close()
			}
			a.dev = dev
			a.kb = kb
			a.entry = entry
			a.layer = 0
			a.sel = -1
			a.closeMacros()
			a.unlocked = lock.Open
			a.hold = lock.Keys
			a.unlocking = lock.Pending
			a.remain = 50
		}
		a.busy = false
		if err != nil {
			a.setFault(explain(err))
			a.unlocking = false
			return
		}
		if a.unlocking {
			a.busy = true
			a.schedulePoll(gen)
		}
	}
	if a.win == nil {
		apply()
		return
	}
	a.win.Update(apply)
}

func (a *App) saveLayout(prev []int) {
	if a.dev == nil || a.kb == nil {
		return
	}
	a.busy = true
	a.clearFault()
	a.gen++
	gen := a.gen
	dev := a.dev
	kb := a.kb
	bits := kb.LayoutBits()
	go func() {
		err := protocol.WriteLayoutOptions(dev, bits)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if err != nil && a.kb == kb {
				for i, choice := range prev {
					kb.SetChoice(i, choice)
				}
				a.layout = kb.Choices()
				a.setFault(explain(err))
			}
		})
	}()
}

func (a *App) assign(code uint16) {
	if a.busy || a.kb == nil || a.unlocking || a.sel < 0 || a.sel >= len(a.kb.Keys) {
		return
	}
	if !a.unlocked {
		a.setFault(fault{kind: faultLockedAssign})
		return
	}
	if a.dev == nil {
		return
	}
	key := a.kb.Keys[a.sel]
	if key.Encoder {
		a.assignEncoder(key, code)
		return
	}
	if !key.HasMatrix {
		return
	}
	a.busy = true
	a.clearFault()
	a.gen++
	gen := a.gen
	dev := a.dev
	kb := a.kb
	layer, row, col := a.layer, key.Row, key.Col
	go func() {
		got, read, err := kb.WriteKey(dev, layer, row, col, code)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if read && a.kb == kb {
				a.kb.Store(layer, row, col, got)
			}
			if err != nil {
				a.setFault(explain(err))
			}
		})
	}()
}

func (a *App) assignEncoder(key protocol.Key, code uint16) {
	a.busy = true
	a.clearFault()
	a.gen++
	gen := a.gen
	dev := a.dev
	kb := a.kb
	layer, index, dir := a.layer, key.EncoderIndex, key.EncDir
	go func() {
		got, read, err := kb.WriteEncoder(dev, layer, index, dir, code)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			if read && a.kb == kb {
				a.kb.StoreEncoder(layer, index, dir, got)
			}
			if err != nil {
				a.setFault(explain(err))
			}
		})
	}()
}

func (a *App) beginUnlock() {
	if a.busy || a.dev == nil || a.unlocked {
		return
	}
	a.busy = true
	a.clearFault()
	a.gen++
	gen := a.gen
	dev := a.dev
	go func() {
		err := protocol.BeginUnlock(dev)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen {
				return
			}
			if err != nil {
				a.busy = false
				a.setFault(explain(err))
				return
			}
			a.unlocking = true
			a.remain = 50
			a.schedulePoll(gen)
		})
	}()
}

func (a *App) lock() {
	if a.busy || a.dev == nil || !a.unlocked {
		return
	}
	a.busy = true
	a.clearFault()
	a.gen++
	gen := a.gen
	dev := a.dev
	go func() {
		err := protocol.LockKeyboard(dev)
		// The keyboard can lock and still drop the reply. The status
		// read is what refresh uses, so a locked status wins.
		status, statusErr := protocol.Security(dev)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen {
				return
			}
			a.busy = false
			a.finishLock(err, status, statusErr)
		})
	}()
}

// finishLock applies a lock attempt. A locked status wins even when the
// lock command itself reported an error.
func (a *App) finishLock(cmdErr error, status protocol.Unlock, statusErr error) {
	if statusErr == nil {
		a.unlocked = status.Open
		a.unlocking = status.Pending
		if len(status.Keys) > 0 {
			a.hold = status.Keys
		}
		if !status.Open && !status.Pending {
			a.clearFault()
			a.flash = locales.FlashLocked()
			return
		}
		if cmdErr != nil {
			a.setFault(explain(cmdErr))
			return
		}
		a.setFault(fault{kind: faultLockFailed})
		return
	}
	if cmdErr != nil {
		a.setFault(explain(cmdErr))
		return
	}
	a.unlocked = false
	a.unlocking = false
	a.clearFault()
	a.flash = locales.FlashLocked()
}

func (a *App) schedulePoll(gen int) {
	dev := a.dev
	go func() {
		time.Sleep(200 * time.Millisecond)
		open, left, err := protocol.PollUnlock(dev)
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.gen != gen || !a.unlocking {
				return
			}
			if err != nil {
				a.setFault(explain(err))
				a.busy = false
				a.unlocking = false
				return
			}
			a.remain = left
			if !open {
				a.schedulePoll(gen)
				return
			}
			a.unlocked = true
			a.unlocking = false
			a.busy = false
			a.clearFault()
			a.flash = locales.FlashUnlocked()
			if a.kb == nil {
				a.Refresh()
			}
		})
	}()
}
