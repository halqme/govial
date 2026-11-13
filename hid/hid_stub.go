//go:build !darwin

package hid

import "errors"

// Info is a raw HID interface a Vial keyboard can be opened on.
type Info struct {
	Name      string
	Serial    string
	VendorID  uint16
	ProductID uint16
	Entry     uint64
}

// Device is an open raw HID connection.
type Device struct{}

func List() ([]Info, error) {
	return nil, errors.New("raw HID is only implemented on macOS")
}

func Open(Info) (*Device, error) {
	return nil, errors.New("raw HID is only implemented on macOS")
}

func (d *Device) Exchange([]byte) ([]byte, error) {
	return nil, errors.New("raw HID is only implemented on macOS")
}

func (d *Device) Close() {}
