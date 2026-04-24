//go:build !windows

package agent

import "testing"

func TestNormalizeIoregVID(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"decimal prusa", "11417", "2C99"},
		{"hex prusa", "0x2C99", "2C99"},
		{"hex lowercase", "0x2c99", "2C99"},
		{"decimal ch340", "6790", "1A86"},
		{"zero rejected", "0", ""},
		{"overflow rejected", "99999999", ""},
		{"garbage rejected", "notanumber", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeIoregVID(tc.input)
			if got != tc.expected {
				t.Fatalf("input=%q want=%q got=%q", tc.input, tc.expected, got)
			}
		})
	}
}

func TestHumanizeLinuxSerialName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{
			"usb-Prusa_Research__prusa3d.com__Original_Prusa_i3_MK3_CZPX1234-if00",
			"Prusa Research  prusa3d.com  Original Prusa i3 MK3 CZPX1234",
		},
		{
			"usb-Creality_3D_Ender_3-if00",
			"Creality 3D Ender 3",
		},
		{
			"usb-NoInterfaceSuffix",
			"NoInterfaceSuffix",
		},
	}
	for _, tc := range cases {
		got := humanizeLinuxSerialName(tc.input)
		if got != tc.expected {
			t.Fatalf("input=%q want=%q got=%q", tc.input, tc.expected, got)
		}
	}
}

func TestParseLpstatOutput_MultiplePrinters(t *testing.T) {
	input := `printer laser is idle.  enabled since Mon Apr  1 00:00:00 2026
	Description: HP LaserJet Pro
	Location: Office
printer photo is idle.  enabled since Mon Apr  1 00:00:00 2026
	Description: Canon PIXMA
`
	printers := parseLpstatOutput(input)
	if len(printers) != 2 {
		t.Fatalf("expected 2 printers, got %d", len(printers))
	}
	if printers[0].ID != "cups:laser" || printers[0].Model != "HP LaserJet Pro" {
		t.Fatalf("unexpected first printer: %+v", printers[0])
	}
	if printers[1].ID != "cups:photo" || printers[1].Model != "Canon PIXMA" {
		t.Fatalf("unexpected second printer: %+v", printers[1])
	}
}

func TestParseLpstatOutput_Empty(t *testing.T) {
	if out := parseLpstatOutput(""); out != nil {
		t.Fatalf("expected nil, got %v", out)
	}
}

func TestParseLpstatOutput_NoDescription(t *testing.T) {
	input := "printer laser is idle.  enabled since Mon Apr  1 00:00:00 2026\n"
	printers := parseLpstatOutput(input)
	if len(printers) != 1 {
		t.Fatalf("expected 1 printer, got %d", len(printers))
	}
	if printers[0].Model != "" {
		t.Fatalf("expected empty model, got %q", printers[0].Model)
	}
}
