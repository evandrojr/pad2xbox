package main

import (
	"reflect"
	"testing"
)

func TestParseSources(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"/dev/input/event30", []string{"/dev/input/event30"}},
		{"/dev/input/event30, /dev/input/event34", []string{"/dev/input/event30", "/dev/input/event34"}},
		{" , , ", nil},
	}
	for _, tc := range cases {
		if got := parseSources(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseSources(%q) = %v, esperado %v", tc.in, got, tc.want)
		}
	}
}
