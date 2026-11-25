// Package mapper traduz um controle genérico (layout PlayStation/DirectInput)
// para o layout Xbox 360 esperado por SDL, Steam e XInput.
package mapper

import (
	"math"

	"github.com/evandrojr/pad2xbox/internal/evdev"
)

// O hid-dragonrise entrega botões "legacy" (0x120..0x12b), não BTN_GAMEPAD.
// Ordem física do PC TWIN SHOCK (0079:0006), confirmada pelo descritor HID:
// Triângulo, Círculo, X, Quadrado, L2, R2, L1, R1, Select, Start, L3, R3.
// A conversão para o layout Xbox é feita aqui.
var buttonMap = map[uint16]uint16{
	evdev.BTN0:  evdev.BTN_NORTH,  // Triângulo -> Y
	evdev.BTN1:  evdev.BTN_EAST,   // Círculo   -> B
	evdev.BTN2:  evdev.BTN_SOUTH,  // X/Cross   -> A
	evdev.BTN3:  evdev.BTN_WEST,   // Quadrado  -> X
	evdev.BTN6:  evdev.BTN_TL,     // L1        -> LB
	evdev.BTN7:  evdev.BTN_TR,     // R1        -> RB
	evdev.BTN8:  evdev.BTN_SELECT, // Select    -> Back
	evdev.BTN9:  evdev.BTN_START,  // Start     -> Start
	evdev.BTN10: evdev.BTN_THUMBL, // L3
	evdev.BTN11: evdev.BTN_THUMBR, // R3
}

// L2/R2 são digitais neste controle; viram eixos analógicos 0..255 no Xbox.
var digitalTriggers = map[uint16]uint16{
	evdev.BTN4: evdev.ABS_Z,  // L2 -> LT
	evdev.BTN5: evdev.ABS_RZ, // R2 -> RT
}

// Eixos do DragonRise "PC TWIN SHOCK": X/Y = stick esquerdo,
// Z/RZ = stick direito, HAT0 = D-pad. Se o controle tiver RX/RY,
// eles são usados como gatilhos analógicos LT/RT.
var axisMap = map[uint16]uint16{
	evdev.ABS_X:     evdev.ABS_X,
	evdev.ABS_Y:     evdev.ABS_Y,
	evdev.ABS_Z:     evdev.ABS_RX,
	evdev.ABS_RZ:    evdev.ABS_RY,
	evdev.ABS_RX:    evdev.ABS_Z,
	evdev.ABS_RY:    evdev.ABS_RZ,
	evdev.ABS_HAT0X: evdev.ABS_HAT0X,
	evdev.ABS_HAT0Y: evdev.ABS_HAT0Y,
}

type State struct {
	src      *evdev.Device
	deadzone float64
	keys     map[uint16]int32
	axes     map[uint16]int32
	norm     map[uint16]float64 // eixos de origem normalizados para -1..1
}

func New(src *evdev.Device, deadzone float64) *State {
	if deadzone < 0 {
		deadzone = 0
	}
	if deadzone > 0.9 {
		deadzone = 0.9
	}
	return &State{
		src:      src,
		deadzone: deadzone,
		keys:     make(map[uint16]int32),
		axes:     make(map[uint16]int32),
		norm:     make(map[uint16]float64),
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// normalized converte o valor bruto usando os limites informados pelo evdev.
func (s *State) normalized(code uint16, value int32) float64 {
	info, ok := s.src.AbsInfo[code]
	if !ok || info.Maximum <= info.Minimum {
		return float64(value)
	}
	center := (float64(info.Minimum) + float64(info.Maximum)) / 2
	half := (float64(info.Maximum) - float64(info.Minimum)) / 2
	return (float64(value) - center) / half
}

// stick aplica deadzone radial e renormaliza o vetor para a faixa cheia.
func (s *State) stick(srcX, srcY, dstX, dstY uint16) {
	x, y := s.norm[srcX], s.norm[srcY]
	mag := math.Hypot(x, y)
	if mag <= s.deadzone {
		x, y = 0, 0
	} else if mag > 0 {
		scale := (mag - s.deadzone) / ((1 - s.deadzone) * mag)
		x *= scale
		y *= scale
	}
	s.axes[dstX] = int32(clamp(x, -1, 1) * 32767)
	s.axes[dstY] = int32(clamp(y, -1, 1) * 32767)
}

func (s *State) Apply(ev evdev.Event) {
	switch ev.Type {
	case evdev.EV_KEY:
		if target, ok := buttonMap[ev.Code]; ok {
			s.keys[target] = ev.Value
		}
		// Gatilhos digitais viram eixos analógicos (0 ou 255), como no xpad.
		if target, ok := digitalTriggers[ev.Code]; ok {
			s.axes[target] = ev.Value * 255
		}

	case evdev.EV_ABS:
		n := s.normalized(ev.Code, ev.Value)
		s.norm[ev.Code] = n
		if target, ok := axisMap[ev.Code]; ok {
			switch target {
			case evdev.ABS_Z, evdev.ABS_RZ: // gatilhos: 0..255
				s.axes[target] = int32(clamp01(n) * 255)
			case evdev.ABS_HAT0X, evdev.ABS_HAT0Y: // D-pad: -1/0/1
				s.axes[target] = ev.Value
			default: // sticks: -32768..32767
				s.axes[target] = int32(clamp(n, -1, 1) * 32767)
			}
		}
		switch ev.Code {
		case evdev.ABS_X, evdev.ABS_Y:
			s.stick(evdev.ABS_X, evdev.ABS_Y, evdev.ABS_X, evdev.ABS_Y)
		case evdev.ABS_Z, evdev.ABS_RZ:
			s.stick(evdev.ABS_Z, evdev.ABS_RZ, evdev.ABS_RX, evdev.ABS_RY)
		}
	}
}

func clamp01(v float64) float64 { return clamp(v, 0, 1) }

// Snapshot devolve cópias dos mapas para envio seguro.
func (s *State) Snapshot() (map[uint16]int32, map[uint16]int32) {
	keys := make(map[uint16]int32, len(s.keys))
	for k, v := range s.keys {
		keys[k] = v
	}
	axes := make(map[uint16]int32, len(s.axes))
	for k, v := range s.axes {
		axes[k] = v
	}
	return keys, axes
}
