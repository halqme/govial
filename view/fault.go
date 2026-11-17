package view

import (
	"errors"
	"strings"

	"github.com/halqme/govial/locales"
	"github.com/halqme/govial/protocol"
)

type faultKind int

const (
	faultNone faultKind = iota
	faultRaw
	faultKept
	faultNoAnswer
	faultDisconnected
	faultOutside
	faultWrite
	faultLockedAssign
	faultHoldBasic
	faultLockFailed
	faultMacroText
	faultMacroDelay
	faultMacroWide
	faultMacroFit
)

// fault is why the banner is showing, so a language change can say it again.
type fault struct {
	kind       faultKind
	arg        string
	need, have int
}

func (f fault) text() string {
	switch f.kind {
	case faultRaw:
		return f.arg
	case faultKept:
		return locales.ErrKept(f.arg)
	case faultNoAnswer:
		return locales.ErrNoAnswer()
	case faultDisconnected:
		return locales.ErrDisconnected()
	case faultOutside:
		return locales.ErrOutside()
	case faultWrite:
		return locales.ErrWrite()
	case faultLockedAssign:
		return locales.ErrLockedAssign()
	case faultHoldBasic:
		return locales.NoteHoldBasicOnly()
	case faultLockFailed:
		return locales.ErrLockFailed()
	case faultMacroText:
		return locales.ErrMacroText()
	case faultMacroDelay:
		return locales.ErrMacroDelay()
	case faultMacroWide:
		return locales.ErrMacroWide()
	case faultMacroFit:
		return locales.ErrMacroFit(f.need, f.have)
	default:
		return ""
	}
}

func (a *App) setFault(f fault) {
	a.fault = f
	a.err = f.text()
}

func (a *App) clearFault() {
	a.fault = fault{}
	a.err = ""
}

// banner is the failure on screen. A known failure follows the language;
// a message set from outside, as a test does, is shown as it is.
func (a *App) banner() string {
	if a.fault.kind != faultNone {
		return a.fault.text()
	}
	return a.err
}

func explain(err error) fault {
	if err == nil {
		return fault{}
	}
	var big *protocol.TooBigError
	if errors.As(err, &big) {
		return fault{kind: faultMacroFit, need: big.Need, have: big.Have}
	}
	var kept *protocol.Kept
	if errors.As(err, &kept) {
		name := protocol.KeyName(kept.Got)
		if name == "" {
			name = "No"
		}
		return fault{kind: faultKept, arg: name}
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "did not answer"):
		return fault{kind: faultNoAnswer}
	case strings.Contains(msg, "not connected"), strings.Contains(msg, "is closed"):
		return fault{kind: faultDisconnected}
	case strings.Contains(msg, "outside the keymap"):
		return fault{kind: faultOutside}
	case strings.Contains(msg, "write report"):
		return fault{kind: faultWrite}
	default:
		return fault{kind: faultRaw, arg: msg}
	}
}
