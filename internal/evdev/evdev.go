// Package evdev lê eventos de dispositivos de entrada do Linux (/dev/input/event*).
// Usa apenas a biblioteca padrão: ioctl EVIOC* via syscall.
package evdev

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Tipos de evento.
const (
	EV_SYN = 0x00
	EV_KEY = 0x01
	EV_ABS = 0x03
	EV_MSC = 0x04
)

// Códigos de eventos usados por controles.
const (
	SYN_REPORT = 0

	BTN_JOYSTICK = 0x120
	BTN_GAMEPAD  = 0x130

	// Botões "legacy" do DragonRise 0079:0006 (BTN_TRIGGER..BTN_BASE6).
	BTN0       = 0x120 // Triângulo
	BTN1       = 0x121 // Círculo
	BTN2       = 0x122 // X (Cross)
	BTN3       = 0x123 // Quadrado
	BTN4       = 0x124 // L2
	BTN5       = 0x125 // R2
	BTN6       = 0x126 // L1
	BTN7       = 0x127 // R1
	BTN8       = 0x128 // Select
	BTN9       = 0x129 // Start
	BTN10      = 0x12a // L3
	BTN11      = 0x12b // R3
	BTN_SOUTH  = 0x130 // Cross (PS) / A (Xbox)
	BTN_EAST   = 0x131 // Circle / B
	BTN_NORTH  = 0x133 // Triangle / Y
	BTN_WEST   = 0x134 // Square / X
	BTN_TL     = 0x136 // L1 / LB
	BTN_TR     = 0x137 // R1 / RB
	BTN_TL2    = 0x138 // L2 / LT (digital)
	BTN_TR2    = 0x139 // R2 / RT (digital)
	BTN_SELECT = 0x13a // Share / Back
	BTN_START  = 0x13b // Options / Start
	BTN_MODE   = 0x13c // PS / Guide / MODE
	BTN_THUMBL = 0x13d // L3
	BTN_THUMBR = 0x13e // R3

	ABS_X     = 0x00
	ABS_Y     = 0x01
	ABS_Z     = 0x02
	ABS_RX    = 0x03
	ABS_RY    = 0x04
	ABS_RZ    = 0x05
	ABS_HAT0X = 0x10
	ABS_HAT0Y = 0x11
)

var ErrTimeout = errors.New("evdev: timeout")

type ID struct {
	Bus     uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

type AbsInfo struct {
	Value      int32
	Minimum    int32
	Maximum    int32
	Fuzz       int32
	Flat       int32
	Resolution int32
}

type Event struct {
	Type  uint16
	Code  uint16
	Value int32
}

type rawEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

const eventSize = int(unsafe.Sizeof(rawEvent{}))

// Info é um resumo de um dispositivo, usado por detect e autodetect.
type Info struct {
	Path    string
	Name    string
	Phys    string
	ID      ID
	KeyBits []byte
	AbsBits []byte
	AbsInfo map[uint16]AbsInfo
}

type Device struct {
	fd      int
	Path    string
	Name    string
	Phys    string
	ID      ID
	KeyBits []byte
	AbsBits []byte
	AbsInfo map[uint16]AbsInfo
}

func ioctl(fd int, req uintptr, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

// ioc monta um número de ioctl no formato _IOC(dir, type, nr, size).
// dir: 1 = write (kernel <- user), 2 = read (kernel -> user).
func ioc(dir, typ, nr, size uintptr) uintptr {
	return (dir << 30) | (size << 16) | (typ << 8) | nr
}

func Open(path string) (*Device, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	d := &Device{fd: fd, Path: path}

	d.Name = deviceName(fd)
	d.Phys = devicePhys(fd)
	if err := ioctl(fd, ioc(2, 'E', 0x02, uintptr(unsafe.Sizeof(ID{}))), uintptr(unsafe.Pointer(&d.ID))); err != nil {
		d.Close()
		return nil, fmt.Errorf("EVIOCGID: %w", err)
	}
	d.KeyBits, err = deviceBits(fd, EV_KEY, 96)
	if err != nil {
		d.Close()
		return nil, err
	}
	d.AbsBits, err = deviceBits(fd, EV_ABS, 8)
	if err != nil {
		d.Close()
		return nil, err
	}
	d.AbsInfo = make(map[uint16]AbsInfo)
	for code := 0; code < 64; code++ {
		if !d.HasAbs(uint16(code)) {
			continue
		}
		var info AbsInfo
		req := ioc(2, 'E', uintptr(0x40+code), uintptr(unsafe.Sizeof(info)))
		if err := ioctl(fd, req, uintptr(unsafe.Pointer(&info))); err == nil {
			d.AbsInfo[uint16(code)] = info
		}
	}
	return d, nil
}

func (d *Device) Close() error {
	if d.fd < 0 {
		return nil
	}
	err := syscall.Close(d.fd)
	d.fd = -1
	return err
}

func deviceName(fd int) string {
	buf := make([]byte, 256)
	req := ioc(2, 'E', 0x06, uintptr(len(buf)))
	if err := ioctl(fd, req, uintptr(unsafe.Pointer(&buf[0]))); err != nil {
		return ""
	}
	return strings.TrimRight(string(buf), "\x00 ")
}

func devicePhys(fd int) string {
	buf := make([]byte, 256)
	// EVIOCGPHYS(len)
	if err := ioctl(fd, ioc(2, 'E', 0x07, uintptr(len(buf))), uintptr(unsafe.Pointer(&buf[0]))); err != nil {
		return ""
	}
	return strings.TrimRight(string(buf), "\x00 ")
}

func deviceBits(fd int, kind uint16, size int) ([]byte, error) {
	buf := make([]byte, size)
	req := ioc(2, 'E', uintptr(0x20+kind), uintptr(size))
	if err := ioctl(fd, req, uintptr(unsafe.Pointer(&buf[0]))); err != nil {
		return nil, fmt.Errorf("EVIOCGBIT(%d): %w", kind, err)
	}
	return buf, nil
}

func hasBit(bits []byte, code uint16) bool {
	idx := int(code) / 8
	return idx < len(bits) && bits[idx]&(1<<(code%8)) != 0
}

func (d *Device) HasKey(code uint16) bool { return hasBit(d.KeyBits, code) }
func (d *Device) HasAbs(code uint16) bool { return hasBit(d.AbsBits, code) }

func (d *Device) Read() (Event, error) {
	var buf [eventSize]byte
	n, err := syscall.Read(d.fd, buf[:])
	if err != nil {
		return Event{}, err
	}
	if n != eventSize {
		return Event{}, fmt.Errorf("evdev: leitura curta (%d bytes)", n)
	}
	raw := *(*rawEvent)(unsafe.Pointer(&buf[0]))
	return Event{Type: raw.Type, Code: raw.Code, Value: raw.Value}, nil
}

// ReadTimeout espera até timeout por um evento. Evita travar no SIGINT.
func (d *Device) ReadTimeout(timeout time.Duration) (Event, error) {
	var set syscall.FdSet
	set.Bits[d.fd/64] |= 1 << uint(d.fd%64)
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	n, err := syscall.Select(d.fd+1, &set, nil, nil, &tv)
	if err != nil {
		if errors.Is(err, syscall.EINTR) {
			return Event{}, ErrTimeout
		}
		return Event{}, err
	}
	if n == 0 {
		return Event{}, ErrTimeout
	}
	return d.Read()
}

func Inspect(path string) (Info, error) {
	d, err := Open(path)
	if err != nil {
		return Info{}, err
	}
	defer d.Close()
	return Info{
		Path: path, Name: d.Name, Phys: d.Phys, ID: d.ID,
		KeyBits: d.KeyBits, AbsBits: d.AbsBits, AbsInfo: d.AbsInfo,
	}, nil
}

// Detect varre /dev/input/event* e devolve os controles encontrados.
func Detect() ([]Info, error) {
	paths, err := filepath.Glob("/dev/input/event[0-9]*")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var found []Info
	for _, path := range paths {
		info, err := Inspect(path)
		if err != nil {
			continue // dispositivo ocupado/permissão negada
		}
		// Exige sticks e botões de joystick, e descarta dispositivos virtuais
		// (Phys vazio, como o pad uinput criado por este próprio programa):
		// sem isso o serviço acabaria lendo a si mesmo num loop.
		isPad := info.Phys != "" &&
			info.HasAbs(ABS_X) && info.HasAbs(ABS_Y) &&
			(info.HasKey(BTN_GAMEPAD) || info.HasKey(BTN_JOYSTICK)) &&
			!strings.Contains(strings.ToLower(info.Name), "uinput")
		if isPad {
			found = append(found, info)
		}
	}
	return found, nil
}

func (i Info) HasKey(code uint16) bool { return hasBit(i.KeyBits, code) }
func (i Info) HasAbs(code uint16) bool { return hasBit(i.AbsBits, code) }

func KeyName(code uint16) string {
	switch code {
	case BTN_SOUTH:
		return "BTN_SOUTH (Cross/A)"
	case BTN_EAST:
		return "BTN_EAST (Circle/B)"
	case BTN_NORTH:
		return "BTN_NORTH (Triangle/Y)"
	case BTN_WEST:
		return "BTN_WEST (Square/X)"
	case BTN_TL:
		return "BTN_TL (L1/LB)"
	case BTN_TR:
		return "BTN_TR (R1/RB)"
	case BTN_TL2:
		return "BTN_TL2 (L2/LT)"
	case BTN_TR2:
		return "BTN_TR2 (R2/RT)"
	case BTN_SELECT:
		return "BTN_SELECT (Share/Back)"
	case BTN_START:
		return "BTN_START (Options/Start)"
	case BTN_MODE:
		return "BTN_MODE (PS/MODE)"
	case BTN_THUMBL:
		return "BTN_THUMBL (L3)"
	case BTN_THUMBR:
		return "BTN_THUMBR (R3)"
	}
	switch code {
	case BTN0:
		return "BTN_0 (Triângulo)"
	case BTN1:
		return "BTN_1 (Círculo)"
	case BTN2:
		return "BTN_2 (X/Cross)"
	case BTN3:
		return "BTN_3 (Quadrado)"
	case BTN4:
		return "BTN_4 (L2)"
	case BTN5:
		return "BTN_5 (R2)"
	case BTN6:
		return "BTN_6 (L1)"
	case BTN7:
		return "BTN_7 (R1)"
	case BTN8:
		return "BTN_8 (Select)"
	case BTN9:
		return "BTN_9 (Start)"
	case BTN10:
		return "BTN_10 (L3)"
	case BTN11:
		return "BTN_11 (R3)"
	}
	if code >= BTN_JOYSTICK && code < BTN_SOUTH {
		return fmt.Sprintf("BTN_%d", code-BTN_JOYSTICK)
	}
	return fmt.Sprintf("KEY_0x%02x", code)
}

func AbsName(code uint16) string {
	switch code {
	case ABS_X:
		return "ABS_X (stick esq. X)"
	case ABS_Y:
		return "ABS_Y (stick esq. Y)"
	case ABS_Z:
		return "ABS_Z"
	case ABS_RX:
		return "ABS_RX"
	case ABS_RY:
		return "ABS_RY"
	case ABS_RZ:
		return "ABS_RZ"
	case ABS_HAT0X:
		return "ABS_HAT0X (D-pad X)"
	case ABS_HAT0Y:
		return "ABS_HAT0Y (D-pad Y)"
	}
	return fmt.Sprintf("ABS_0x%02x", code)
}

func (e Event) String() string {
	switch e.Type {
	case EV_SYN:
		return "SYN REPORT"
	case EV_KEY:
		return fmt.Sprintf("KEY %-26s (%#04x) = %d", KeyName(e.Code), e.Code, e.Value)
	case EV_ABS:
		return fmt.Sprintf("ABS %-26s (%#04x) = %d", AbsName(e.Code), e.Code, e.Value)
	default:
		return fmt.Sprintf("EV_%02x code=%#04x value=%d", e.Type, e.Code, e.Value)
	}
}
