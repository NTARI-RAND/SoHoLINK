package printer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── GCodeValidator Tests ──────────────────────────────────────────────────────

func TestNewGCodeValidator_Defaults(t *testing.T) {
	v := NewGCodeValidator()
	if v.MaxTemp != 280 {
		t.Errorf("MaxTemp = %d, want 280", v.MaxTemp)
	}
	if v.MaxBedTemp != 110 {
		t.Errorf("MaxBedTemp = %d, want 110", v.MaxBedTemp)
	}
	if v.MaxFeedRate != 15000 {
		t.Errorf("MaxFeedRate = %d, want 15000", v.MaxFeedRate)
	}
	if v.MaxAccel != 5000 {
		t.Errorf("MaxAccel = %d, want 5000", v.MaxAccel)
	}
	if len(v.ProhibitedCommands) != 5 {
		t.Errorf("ProhibitedCommands count = %d, want 5", len(v.ProhibitedCommands))
	}
}

func TestNewGCodeValidator_ProhibitedCommands(t *testing.T) {
	v := NewGCodeValidator()
	expected := map[string]bool{"M0": true, "M1": true, "M112": true, "M997": true, "M999": true}
	for _, cmd := range v.ProhibitedCommands {
		if !expected[cmd] {
			t.Errorf("unexpected prohibited command: %s", cmd)
		}
	}
}

func writeGCodeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.gcode")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test gcode file: %v", err)
	}
	return path
}

func TestValidate_ValidGCode(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "simple move commands",
			content: "G0 X10 Y20 Z5 F3000\nG1 X50 Y50 E1.5 F1500\n",
		},
		{
			name:    "comments only",
			content: "; This is a comment\n; Another comment\n",
		},
		{
			name:    "empty lines and comments",
			content: "\n; comment\n\nG28\n\n; home\n",
		},
		{
			name:    "safe temperature",
			content: "M104 S200\nM140 S60\nM109 S200\nM190 S60\n",
		},
		{
			name:    "inline comments",
			content: "G1 X10 Y20 F3000 ; move to position\n",
		},
		{
			name:    "safe feedrate",
			content: "G1 X10 F14999\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := NewGCodeValidator()
			path := writeGCodeFile(t, tc.content)
			if err := v.Validate(path); err != nil {
				t.Errorf("Validate() returned error for valid gcode: %v", err)
			}
		})
	}
}

func TestValidate_ProhibitedCommands(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "M0 unconditional stop", content: "G28\nM0\n"},
		{name: "M1 sleep", content: "G28\nM1\n"},
		{name: "M112 emergency stop", content: "G28\nM112\n"},
		{name: "M997 firmware update", content: "G28\nM997\n"},
		{name: "M999 reset", content: "G28\nM999\n"},
		{name: "M0 with parameters", content: "G28\nM0 S5\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := NewGCodeValidator()
			path := writeGCodeFile(t, tc.content)
			err := v.Validate(path)
			if err == nil {
				t.Error("Validate() should return error for prohibited command")
			}
		})
	}
}

func TestValidate_HotendTempExceedsLimit(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "M104 over limit", content: "M104 S300\n"},
		{name: "M109 over limit", content: "M109 S350\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := NewGCodeValidator()
			path := writeGCodeFile(t, tc.content)
			err := v.Validate(path)
			if err == nil {
				t.Error("Validate() should return error for excessive hotend temp")
			}
		})
	}
}

func TestValidate_BedTempExceedsLimit(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "M140 over limit", content: "M140 S120\n"},
		{name: "M190 over limit", content: "M190 S150\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := NewGCodeValidator()
			path := writeGCodeFile(t, tc.content)
			err := v.Validate(path)
			if err == nil {
				t.Error("Validate() should return error for excessive bed temp")
			}
		})
	}
}

func TestValidate_FeedrateExceedsLimit(t *testing.T) {
	v := NewGCodeValidator()
	path := writeGCodeFile(t, "G1 X100 F20000\n")
	err := v.Validate(path)
	if err == nil {
		t.Error("Validate() should return error for excessive feedrate")
	}
}

func TestValidate_FileNotFound(t *testing.T) {
	v := NewGCodeValidator()
	err := v.Validate("/nonexistent/path/test.gcode")
	if err == nil {
		t.Error("Validate() should return error for missing file")
	}
}

func TestValidate_CustomLimits(t *testing.T) {
	v := &GCodeValidator{
		MaxTemp:            200,
		MaxBedTemp:         80,
		MaxFeedRate:        5000,
		MaxAccel:           2000,
		ProhibitedCommands: []string{"M0"},
	}

	// Temperature within custom limit should pass
	path := writeGCodeFile(t, "M104 S190\nM140 S70\nG1 X10 F4000\n")
	if err := v.Validate(path); err != nil {
		t.Errorf("Validate() should pass within custom limits: %v", err)
	}

	// Temperature above custom limit should fail
	path = writeGCodeFile(t, "M104 S210\n")
	if err := v.Validate(path); err == nil {
		t.Error("Validate() should fail when exceeding custom temp limit")
	}
}

func TestParseParameter(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		param string
		want  int
	}{
		{name: "simple S param", line: "M104 S220", param: "S", want: 220},
		{name: "F param", line: "G1 X10 Y20 F3000", param: "F", want: 3000},
		{name: "X param", line: "G1 X150 Y200", param: "X", want: 150},
		{name: "missing param", line: "G28", param: "S", want: 0},
		{name: "float param truncated", line: "G1 F3000.5", param: "F", want: 3000},
		{name: "param not present", line: "M104 S200", param: "T", want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseParameter(tc.line, tc.param)
			if got != tc.want {
				t.Errorf("parseParameter(%q, %q) = %d, want %d", tc.line, tc.param, got, tc.want)
			}
		})
	}
}

// ── PrintJob Tests ────────────────────────────────────────────────────────────

func TestPrintJob_Struct(t *testing.T) {
	job := PrintJob{
		JobID:          "job_001",
		TransactionID:  "tx_abc",
		UserDID:        "did:key:z123",
		ProviderDID:    "did:key:z456",
		PrinterType:    "3d",
		GCodePath:      "/tmp/model.gcode",
		DocumentPath:   "",
		Copies:         1,
		EstimatedGrams: 25.5,
		EstimatedPages: 0,
	}

	if job.JobID != "job_001" {
		t.Errorf("JobID = %q, want %q", job.JobID, "job_001")
	}
	if job.PrinterType != "3d" {
		t.Errorf("PrinterType = %q, want %q", job.PrinterType, "3d")
	}
	if job.EstimatedGrams != 25.5 {
		t.Errorf("EstimatedGrams = %f, want 25.5", job.EstimatedGrams)
	}
}

func TestPrintResult_Struct(t *testing.T) {
	result := PrintResult{
		JobID:         "job_001",
		Status:        "completed",
		ActualGrams:   24.8,
		ActualPages:   0,
		PrintDuration: 45 * time.Minute,
	}

	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if result.PrintDuration != 45*time.Minute {
		t.Errorf("PrintDuration = %v, want 45m", result.PrintDuration)
	}
}

func TestPrintJob_2DFields(t *testing.T) {
	job := PrintJob{
		JobID:          "job_002",
		PrinterType:    "2d",
		DocumentPath:   "/tmp/doc.pdf",
		Copies:         3,
		EstimatedPages: 10,
	}

	if job.PrinterType != "2d" {
		t.Errorf("PrinterType = %q, want %q", job.PrinterType, "2d")
	}
	if job.Copies != 3 {
		t.Errorf("Copies = %d, want 3", job.Copies)
	}
	if job.EstimatedPages != 10 {
		t.Errorf("EstimatedPages = %d, want 10", job.EstimatedPages)
	}
}

func TestPrintResult_StatusValues(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{name: "completed", status: "completed"},
		{name: "failed", status: "failed"},
		{name: "cancelled", status: "cancelled"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := PrintResult{Status: tc.status}
			if r.Status != tc.status {
				t.Errorf("Status = %q, want %q", r.Status, tc.status)
			}
		})
	}
}
