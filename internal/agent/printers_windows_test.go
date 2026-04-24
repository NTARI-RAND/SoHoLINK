//go:build windows

package agent

import "testing"

func TestExtractVIDFromInstanceID(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"prusa", `USB\VID_2C99&PID_0002\0123456`, "2C99"},
		{"lowercase normalized", `USB\vid_2c99&PID_0002\0123456`, ""}, // marker is case-sensitive
		{"ch340", `USB\VID_1A86&PID_7523\5&1234abcd&0&1`, "1A86"},
		{"no marker", `HID\SomeOtherDevice\Instance`, ""},
		{"truncated", `USB\VID_2C`, ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractVIDFromInstanceID(tc.input)
			if got != tc.expected {
				t.Fatalf("input=%q want=%q got=%q", tc.input, tc.expected, got)
			}
		})
	}
}
