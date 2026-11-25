package mapper

import (
	"testing"

	"github.com/evandrojr/pad2xbox/internal/evdev"
)

func fakeDevice() *evdev.Device {
	return &evdev.Device{AbsInfo: map[uint16]evdev.AbsInfo{
		evdev.ABS_X:  {Minimum: 0, Maximum: 255},
		evdev.ABS_Y:  {Minimum: 0, Maximum: 255},
		evdev.ABS_Z:  {Minimum: 0, Maximum: 255},
		evdev.ABS_RZ: {Minimum: 0, Maximum: 255},
	}}
}

func TestButtonTranslation(t *testing.T) {
	s := New(fakeDevice(), 0.08)
	cases := []struct {
		code uint16
		want uint16
	}{
		{evdev.BTN0, evdev.BTN_NORTH},
		{evdev.BTN1, evdev.BTN_EAST},
		{evdev.BTN2, evdev.BTN_SOUTH},
		{evdev.BTN3, evdev.BTN_WEST},
		{evdev.BTN6, evdev.BTN_TL},
		{evdev.BTN7, evdev.BTN_TR},
		{evdev.BTN8, evdev.BTN_SELECT},
		{evdev.BTN9, evdev.BTN_START},
		{evdev.BTN10, evdev.BTN_THUMBL},
		{evdev.BTN11, evdev.BTN_THUMBR},
	}
	for _, tc := range cases {
		s.Apply(evdev.Event{Type: evdev.EV_KEY, Code: tc.code, Value: 1})
		keys, _ := s.Snapshot()
		if got := keys[tc.want]; got != 1 {
			t.Errorf("botão %#x: esperado %#x=1, obtido %d", tc.code, tc.want, got)
		}
	}
}

func TestDigitalTriggers(t *testing.T) {
	s := New(fakeDevice(), 0.08)
	s.Apply(evdev.Event{Type: evdev.EV_KEY, Code: evdev.BTN4, Value: 1})
	s.Apply(evdev.Event{Type: evdev.EV_KEY, Code: evdev.BTN5, Value: 1})
	_, axes := s.Snapshot()
	if axes[evdev.ABS_Z] != 255 || axes[evdev.ABS_RZ] != 255 {
		t.Fatalf("gatilhos: LT=%d RT=%d, esperado 255/255", axes[evdev.ABS_Z], axes[evdev.ABS_RZ])
	}
}

func TestStickNormalizationAndDeadzone(t *testing.T) {
	s := New(fakeDevice(), 0.1)
	// Centro do eixo [0..255] é 127.5: valor 140 fica dentro da deadzone.
	s.Apply(evdev.Event{Type: evdev.EV_ABS, Code: evdev.ABS_X, Value: 140})
	s.Apply(evdev.Event{Type: evdev.EV_ABS, Code: evdev.ABS_Y, Value: 128})
	_, axes := s.Snapshot()
	if axes[evdev.ABS_X] != 0 || axes[evdev.ABS_Y] != 0 {
		t.Fatalf("deadzone falhou: X=%d Y=%d", axes[evdev.ABS_X], axes[evdev.ABS_Y])
	}

	// Valor máximo deve virar +32767 no stick esquerdo.
	s.Apply(evdev.Event{Type: evdev.EV_ABS, Code: evdev.ABS_X, Value: 255})
	_, axes = s.Snapshot()
	if axes[evdev.ABS_X] != 32767 {
		t.Fatalf("X=%d, esperado 32767", axes[evdev.ABS_X])
	}

	// Stick direito: ABS_Z -> ABS_RX, ABS_RZ -> ABS_RY.
	s.Apply(evdev.Event{Type: evdev.EV_ABS, Code: evdev.ABS_Z, Value: 255})
	s.Apply(evdev.Event{Type: evdev.EV_ABS, Code: evdev.ABS_RZ, Value: 128})
	_, axes = s.Snapshot()
	if axes[evdev.ABS_RX] != 32767 {
		t.Fatalf("ABS_RX=%d, esperado 32767", axes[evdev.ABS_RX])
	}
}

func TestHat(t *testing.T) {
	s := New(fakeDevice(), 0.08)
	s.Apply(evdev.Event{Type: evdev.EV_ABS, Code: evdev.ABS_HAT0X, Value: -1})
	_, axes := s.Snapshot()
	if axes[evdev.ABS_HAT0X] != -1 {
		t.Fatalf("HAT0X=%d, esperado -1", axes[evdev.ABS_HAT0X])
	}
}
