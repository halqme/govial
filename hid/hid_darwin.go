//go:build darwin

// Package hid lists and exchanges 32-byte reports with raw HID devices.
// It calls IOKit through purego so the app still builds with CGO_ENABLED=0.
package hid

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	UsagePage = 0xFF60
	Usage     = 0x61
	reportLen = 32
)

// Info is one raw HID interface speaking the VIA usage page.
type Info struct {
	Name      string
	Serial    string
	VendorID  uint16
	ProductID uint16
	Entry     uint64
}

// Device is an open interface. Exchange and Close run on the process HID thread.
type Device struct {
	entry uint64
	ref   uintptr
	state *hidState
}

type hidState struct {
	id     uintptr
	report []byte
	pin    runtime.Pinner
	got    int
	inbox  [64]byte
}

var (
	statesMu sync.Mutex
	states   = map[uintptr]*hidState{}
	nextID   uintptr
)

var (
	loadOnce sync.Once
	loadErr  error
	queue    chan hidJob
	ready    = make(chan struct{})

	libCF    uintptr
	libIOKit uintptr

	cfStringCreate      func(alloc uintptr, cstr *byte, encoding uint32) uintptr
	cfStringGetCString  func(str uintptr, buf *byte, n int, encoding uint32) bool
	cfStringGetTypeID   func() uintptr
	cfNumberGetTypeID   func() uintptr
	cfGetTypeID         func(obj uintptr) uintptr
	cfNumberGetValue    func(num uintptr, typ int, value unsafe.Pointer) bool
	cfNumberCreate      func(alloc uintptr, typ int, value unsafe.Pointer) uintptr
	cfDictionaryCreate  func(alloc uintptr, keys, values unsafe.Pointer, n int, keyCB, valCB uintptr) uintptr
	cfRelease           func(obj uintptr)
	cfRetain            func(obj uintptr) uintptr
	cfSetGetCount       func(set uintptr) int64
	cfSetGetValues      func(set uintptr, values unsafe.Pointer)
	cfRunLoopGetCurrent func() uintptr
	cfRunLoopRunInMode  func(mode uintptr, seconds float64, ret bool) int32

	mgrCreate        func(alloc uintptr, opts int) uintptr
	mgrSetMatching   func(mgr, dict uintptr)
	mgrOpen          func(mgr uintptr, opts int) int32
	mgrSchedule      func(mgr, loop, mode uintptr)
	mgrCopyDevices   func(mgr uintptr) uintptr
	devGetProperty   func(dev, key uintptr) uintptr
	devGetService    func(dev uintptr) uint32
	regEntryID       func(entry uint32, id *uint64) int32
	devOpen          func(dev uintptr, opts int) int32
	devClose         func(dev uintptr, opts int) int32
	devSetReport     func(dev uintptr, typ int32, id int64, report *byte, n int64) int32
	devRegisterInput func(dev uintptr, report *byte, n int64, cb, ctx uintptr)
	devSchedule      func(dev, loop, mode uintptr)

	inputCallback uintptr

	kDefaultMode                              uintptr
	keyVendor, keyProduct, keySerial, keyName uintptr
	keyUsagePage, keyUsage                    uintptr
	dictKeyCB, dictValCB                      uintptr

	manager uintptr
)

type hidJob struct {
	fn  func() error
	err chan error
}

func onHID(fn func() error) error {
	loadOnce.Do(func() {
		queue = make(chan hidJob)
		go hidThread()
	})
	<-ready
	if loadErr != nil {
		return loadErr
	}
	job := hidJob{fn: fn, err: make(chan error, 1)}
	queue <- job
	return <-job.err
}

func hidThread() {
	runtime.LockOSThread()
	loadErr = initIO()
	close(ready)
	if loadErr != nil {
		for job := range queue {
			job.err <- loadErr
		}
		return
	}
	for job := range queue {
		job.err <- job.fn()
	}
}

func initIO() (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("hid: %v", recovered)
		}
	}()
	libCF, err = purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return err
	}
	libIOKit, err = purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return err
	}
	purego.RegisterLibFunc(&cfStringCreate, libCF, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&cfStringGetCString, libCF, "CFStringGetCString")
	purego.RegisterLibFunc(&cfStringGetTypeID, libCF, "CFStringGetTypeID")
	purego.RegisterLibFunc(&cfNumberGetTypeID, libCF, "CFNumberGetTypeID")
	purego.RegisterLibFunc(&cfGetTypeID, libCF, "CFGetTypeID")
	purego.RegisterLibFunc(&cfNumberGetValue, libCF, "CFNumberGetValue")
	purego.RegisterLibFunc(&cfNumberCreate, libCF, "CFNumberCreate")
	purego.RegisterLibFunc(&cfDictionaryCreate, libCF, "CFDictionaryCreate")
	purego.RegisterLibFunc(&cfRelease, libCF, "CFRelease")
	purego.RegisterLibFunc(&cfRetain, libCF, "CFRetain")
	purego.RegisterLibFunc(&cfSetGetCount, libCF, "CFSetGetCount")
	purego.RegisterLibFunc(&cfSetGetValues, libCF, "CFSetGetValues")
	purego.RegisterLibFunc(&cfRunLoopGetCurrent, libCF, "CFRunLoopGetCurrent")
	purego.RegisterLibFunc(&cfRunLoopRunInMode, libCF, "CFRunLoopRunInMode")

	purego.RegisterLibFunc(&mgrCreate, libIOKit, "IOHIDManagerCreate")
	purego.RegisterLibFunc(&mgrSetMatching, libIOKit, "IOHIDManagerSetDeviceMatching")
	purego.RegisterLibFunc(&mgrOpen, libIOKit, "IOHIDManagerOpen")
	purego.RegisterLibFunc(&mgrSchedule, libIOKit, "IOHIDManagerScheduleWithRunLoop")
	purego.RegisterLibFunc(&mgrCopyDevices, libIOKit, "IOHIDManagerCopyDevices")
	purego.RegisterLibFunc(&devGetProperty, libIOKit, "IOHIDDeviceGetProperty")
	purego.RegisterLibFunc(&devGetService, libIOKit, "IOHIDDeviceGetService")
	purego.RegisterLibFunc(&regEntryID, libIOKit, "IORegistryEntryGetRegistryEntryID")
	purego.RegisterLibFunc(&devOpen, libIOKit, "IOHIDDeviceOpen")
	purego.RegisterLibFunc(&devClose, libIOKit, "IOHIDDeviceClose")
	purego.RegisterLibFunc(&devSetReport, libIOKit, "IOHIDDeviceSetReport")
	purego.RegisterLibFunc(&devRegisterInput, libIOKit, "IOHIDDeviceRegisterInputReportCallback")
	purego.RegisterLibFunc(&devSchedule, libIOKit, "IOHIDDeviceScheduleWithRunLoop")

	kDefaultMode = dataSymbol(libCF, "kCFRunLoopDefaultMode")
	// IOHIDKeys.h defines these as C strings, not exported CFString symbols.
	keyVendor = cfString("VendorID")
	keyProduct = cfString("ProductID")
	keySerial = cfString("SerialNumber")
	keyName = cfString("Product")
	keyUsagePage = cfString("PrimaryUsagePage")
	keyUsage = cfString("PrimaryUsage")
	dictKeyCB = symbolAddr(libCF, "kCFTypeDictionaryKeyCallBacks")
	dictValCB = symbolAddr(libCF, "kCFTypeDictionaryValueCallBacks")

	inputCallback = purego.NewCallback(onInput)

	manager = mgrCreate(0, 0)
	if manager == 0 {
		return errors.New("IOHIDManagerCreate failed")
	}
	matching := usageMatching(UsagePage, Usage)
	mgrSetMatching(manager, matching)
	if matching != 0 {
		cfRelease(matching)
	}
	if status := mgrOpen(manager, 0); status != 0 {
		return fmt.Errorf("IOHIDManagerOpen: %s", ioReturn(status))
	}
	mgrSchedule(manager, cfRunLoopGetCurrent(), kDefaultMode)
	cfRunLoopRunInMode(kDefaultMode, 0.05, false)
	return nil
}

func cfString(s string) uintptr {
	buf := append([]byte(s), 0)
	return cfStringCreate(0, &buf[0], 0x08000100)
}

func dataSymbol(lib uintptr, name string) uintptr {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		panic(err)
	}
	return **(**uintptr)(unsafe.Pointer(&p))
}

func symbolAddr(lib uintptr, name string) uintptr {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		panic(err)
	}
	return p
}

func usageMatching(page, usage int) uintptr {
	pageKey := keyUsagePage
	usageKey := keyUsage
	pageNum := number(int32(page))
	usageNum := number(int32(usage))
	defer cfRelease(pageNum)
	defer cfRelease(usageNum)
	keys := [2]uintptr{pageKey, usageKey}
	vals := [2]uintptr{pageNum, usageNum}
	return cfDictionaryCreate(0, unsafe.Pointer(&keys), unsafe.Pointer(&vals), 2, dictKeyCB, dictValCB)
}

func number(v int32) uintptr {
	return cfNumberCreate(0, 3, unsafe.Pointer(&v)) // kCFNumberSInt32Type
}

// List returns raw HID interfaces on the VIA usage page.
func List() ([]Info, error) {
	var infos []Info
	err := onHID(func() error {
		cfRunLoopRunInMode(kDefaultMode, 0.02, false)
		set := mgrCopyDevices(manager)
		if set == 0 {
			return nil
		}
		defer cfRelease(set)
		n := int(cfSetGetCount(set))
		if n == 0 {
			return nil
		}
		vals := make([]uintptr, n)
		cfSetGetValues(set, unsafe.Pointer(&vals[0]))
		for _, dev := range vals {
			page, okPage := propInt(dev, keyUsagePage)
			usage, okUsage := propInt(dev, keyUsage)
			if !okPage || !okUsage || page != UsagePage || usage != Usage {
				continue
			}
			var id uint64
			if regEntryID(devGetService(dev), &id) != 0 || id == 0 {
				continue
			}
			vendor, _ := propInt(dev, keyVendor)
			product, _ := propInt(dev, keyProduct)
			name := propString(dev, keyName)
			if name == "" {
				name = fmt.Sprintf("%04X:%04X", vendor, product)
			}
			infos = append(infos, Info{
				Name:      name,
				Serial:    propString(dev, keySerial),
				VendorID:  uint16(vendor),
				ProductID: uint16(product),
				Entry:     id,
			})
		}
		return nil
	})
	return infos, err
}

// Open opens the interface identified by info.Entry.
func Open(info Info) (*Device, error) {
	var dev *Device
	err := onHID(func() error {
		ref, err := find(info.Entry)
		if err != nil {
			return err
		}
		ref = cfRetain(ref)
		if status := devOpen(ref, 0); status != 0 {
			cfRelease(ref)
			return fmt.Errorf("open %s: %s", info.Name, ioReturn(status))
		}
		state := &hidState{report: make([]byte, 64)}
		state.pin.Pin(&state.report[0])
		statesMu.Lock()
		nextID++
		state.id = nextID
		states[state.id] = state
		statesMu.Unlock()
		devRegisterInput(ref, &state.report[0], int64(len(state.report)), inputCallback, state.id)
		devSchedule(ref, cfRunLoopGetCurrent(), kDefaultMode)
		dev = &Device{entry: info.Entry, ref: ref, state: state}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dev, nil
}

func find(entry uint64) (uintptr, error) {
	set := mgrCopyDevices(manager)
	if set == 0 {
		return 0, errors.New("no HID devices")
	}
	defer cfRelease(set)
	n := int(cfSetGetCount(set))
	if n == 0 {
		return 0, errors.New("keyboard is not connected")
	}
	vals := make([]uintptr, n)
	cfSetGetValues(set, unsafe.Pointer(&vals[0]))
	for _, dev := range vals {
		var id uint64
		if regEntryID(devGetService(dev), &id) != 0 {
			continue
		}
		if id == entry {
			return dev, nil
		}
	}
	return 0, errors.New("keyboard is not connected")
}

// Exchange writes a report and waits up to half a second for the reply.
func (d *Device) Exchange(msg []byte) ([]byte, error) {
	if d == nil || d.ref == 0 {
		return nil, errors.New("HID device is closed")
	}
	if len(msg) > reportLen {
		return nil, fmt.Errorf("report is %d bytes", len(msg))
	}
	var out []byte
	err := onHID(func() error {
		var req [reportLen]byte
		copy(req[:], msg)
		d.state.got = 0
		if status := devSetReport(d.ref, 1, 0, &req[0], int64(reportLen)); status != 0 {
			return fmt.Errorf("write report: %s", ioReturn(status))
		}
		// Return as soon as the input report is delivered instead of polling.
		deadline := time.Now().Add(500 * time.Millisecond)
		for d.state.got == 0 && time.Now().Before(deadline) {
			left := time.Until(deadline).Seconds()
			if left <= 0 {
				break
			}
			cfRunLoopRunInMode(kDefaultMode, left, true)
		}
		if d.state.got == 0 {
			return errors.New("keyboard did not answer")
		}
		n := d.state.got
		if n > reportLen {
			n = reportLen
		}
		out = append([]byte(nil), d.state.inbox[:n]...)
		return nil
	})
	runtime.KeepAlive(msg)
	return out, err
}

// Close releases the interface.
func (d *Device) Close() {
	if d == nil || d.ref == 0 {
		return
	}
	_ = onHID(func() error {
		statesMu.Lock()
		delete(states, d.state.id)
		statesMu.Unlock()
		devClose(d.ref, 0)
		cfRelease(d.ref)
		d.state.pin.Unpin()
		d.ref = 0
		return nil
	})
}

func onInput(ctx uintptr, _ int32, _ uintptr, _ uint32, _ uint32, report unsafe.Pointer, length int64) {
	if ctx == 0 || report == nil || length <= 0 {
		return
	}
	statesMu.Lock()
	state := states[ctx]
	statesMu.Unlock()
	if state == nil {
		return
	}
	n := int(length)
	if n > len(state.inbox) {
		n = len(state.inbox)
	}
	copy(state.inbox[:n], unsafe.Slice((*byte)(report), n))
	state.got = n
}

func propString(dev, key uintptr) string {
	v := devGetProperty(dev, key)
	if v == 0 || cfGetTypeID(v) != cfStringGetTypeID() {
		return ""
	}
	buf := make([]byte, 512)
	if !cfStringGetCString(v, &buf[0], len(buf), 0x08000100) {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return ""
}

func propInt(dev, key uintptr) (int, bool) {
	v := devGetProperty(dev, key)
	if v == 0 || cfGetTypeID(v) != cfNumberGetTypeID() {
		return 0, false
	}
	var n int32
	if !cfNumberGetValue(v, 3, unsafe.Pointer(&n)) {
		return 0, false
	}
	return int(n), true
}

func ioReturn(status int32) string {
	return fmt.Sprintf("0x%08X", uint32(status))
}
