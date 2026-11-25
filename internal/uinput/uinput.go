// Package uinput cria um controle Xbox 360 virtual via /dev/uinput.
// IDs e ranges copies do driver xpad do kernel, para SDL/Steam/XInput reconhecerem.
package uinput

import (
	"fmt"
	"sort"
	"syscall"
	"unsafe"
)

// Ioctls do uinput (valores conferidos com <linux/uinput.h>).
const (
	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502
	uiDevSetup   = 0x405c5503
	uiAbsSetup   = 0x401c5504
	uiSetEVBit   = 0x40045564
	uiSetKeyBit  = 0x40045565
	uiSetAbsBit  = 0x40045567

	busUSB = 0x03

	evSYN = 0x00
	evKEY = 0x01
	evABS = 0x03

	synReport = 0

	// Layout XInput / xpad.
	btnSouth  = 0x130 // A
	btnEast   = 0x131 // B
	btnNorth  = 0x133 // Y
	btnWest   = 0x134 // X
	btnTL     = 0x136 // LB
	btnTR     = 0x137 // RB
	btnSelect = 0x13a // Back
	btnStart  = 0x13b // Start
	btnMode   = 0x13c // Guide
	btnThumbL = 0x13d // L3
	btnThumbR = 0x13e // R3

	absX     = 0x00
	absY     = 0x01
	absZ     = 0x02 // LT analógico
	absRX    = 0x03
	absRY    = 0x04
	absRZ    = 0x05 // RT analógico
	absHat0X = 0x10
	absHat0Y = 0x11
)

type inputID struct {
	Bus     uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

type absInfo struct {
	Value      int32
	Minimum    int32
	Maximum    int32
	Fuzz       int32
	Flat       int32
	Resolution int32
}

// struct uinput_abs_setup: code(2) + padding(2) + absinfo(24) = 28 bytes.
type absSetup struct {
	Code uint16
	_    [2]byte
	Info absInfo
}

// struct uinput_setup: input_id(8) + name[80] + ff_effects_max(4) = 92 bytes.
type devSetup struct {
	ID           inputID
	Name         [80]byte
	FFEffectsMax uint32
}

type rawEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

const eventSize = int(unsafe.Sizeof(rawEvent{}))

var targetKeys = []uint16{
	btnSouth, btnEast, btnNorth, btnWest,
	btnTL, btnTR, btnSelect, btnStart, btnMode,
	btnThumbL, btnThumbR,
}

type axisDef struct {
	code uint16
	min  int32
	max  int32
}

var targetAxes = []axisDef{
	{absX, -32768, 32767},
	{absY, -32768, 32767},
	{absRX, -32768, 32767},
	{absRY, -32768, 32767},
	{absZ, 0, 255},
	{absRZ, 0, 255},
	{absHat0X, -1, 1},
	{absHat0Y, -1, 1},
}

type Device struct {
	fd   int
	Name string
}

func ioctl(fd int, req uintptr, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

// Create cria e registra o dispositivo virtual. name/vendor/product definem
// a identidade apresentada ao SDL/Steam (padrão: Microsoft X-Box 360 pad).
func Create(name string, vendor, product uint16) (*Device, error) {
	fd, err := syscall.Open("/dev/uinput", syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("abrir /dev/uinput: %w (veja o README sobre permissões)", err)
	}
	fail := func(err error) (*Device, error) {
		syscall.Close(fd)
		return nil, err
	}

	for _, ev := range []uint16{evSYN, evKEY, evABS} {
		if err := ioctl(fd, uiSetEVBit, uintptr(ev)); err != nil {
			return fail(fmt.Errorf("UI_SET_EVBIT(%d): %w", ev, err))
		}
	}
	for _, code := range targetKeys {
		if err := ioctl(fd, uiSetKeyBit, uintptr(code)); err != nil {
			return fail(fmt.Errorf("UI_SET_KEYBIT(%#x): %w", code, err))
		}
	}
	for _, axis := range targetAxes {
		if err := ioctl(fd, uiSetAbsBit, uintptr(axis.code)); err != nil {
			return fail(fmt.Errorf("UI_SET_ABSBIT(%#x): %w", axis.code, err))
		}
		setup := absSetup{
			Code: axis.code,
			Info: absInfo{Minimum: axis.min, Maximum: axis.max},
		}
		if err := ioctl(fd, uiAbsSetup, uintptr(unsafe.Pointer(&setup))); err != nil {
			return fail(fmt.Errorf("UI_ABS_SETUP(%#x): %w", axis.code, err))
		}
	}

	setup := devSetup{
		ID: inputID{
			Bus:     busUSB,
			Vendor:  vendor,
			Product: product,
			Version: 0x0110,
		},
		FFEffectsMax: 0,
	}
	copy(setup.Name[:], name)
	if err := ioctl(fd, uiDevSetup, uintptr(unsafe.Pointer(&setup))); err != nil {
		return fail(fmt.Errorf("UI_DEV_SETUP: %w", err))
	}
	if err := ioctl(fd, uiDevCreate, 0); err != nil {
		return fail(fmt.Errorf("UI_DEV_CREATE: %w", err))
	}
	return &Device{fd: fd, Name: name}, nil
}

func writeEvent(fd int, typ, code uint16, value int32) error {
	var raw rawEvent
	raw.Type = typ
	raw.Code = code
	raw.Value = value
	buf := (*[eventSize]byte)(unsafe.Pointer(&raw))
	_, err := syscall.Write(fd, buf[:])
	return err
}

// Send publica um estado completo do controle (teclas, eixos e SYN_REPORT).
func (d *Device) Send(keys map[uint16]int32, axes map[uint16]int32) error {
	kcodes := make([]int, 0, len(keys))
	for code := range keys {
		kcodes = append(kcodes, int(code))
	}
	sort.Ints(kcodes)
	for _, c := range kcodes {
		if err := writeEvent(d.fd, evKEY, uint16(c), keys[uint16(c)]); err != nil {
			return err
		}
	}
	acodes := make([]int, 0, len(axes))
	for code := range axes {
		acodes = append(acodes, int(code))
	}
	sort.Ints(acodes)
	for _, c := range acodes {
		if err := writeEvent(d.fd, evABS, uint16(c), axes[uint16(c)]); err != nil {
			return err
		}
	}
	return writeEvent(d.fd, evSYN, synReport, 0)
}

func (d *Device) Close() error {
	if d.fd < 0 {
		return nil
	}
	_ = ioctl(d.fd, uiDevDestroy, 0)
	err := syscall.Close(d.fd)
	d.fd = -1
	return err
}
