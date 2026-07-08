package operator

import "testing"

// TestRunSuiteCScratch verifies the in-memory §4.2.2.1 lazy-expiry exercise
// passes with the {3,6,9,12,365} thresholds and the 2-key floor. This is the
// grader's own logic, exercised with NO database — it must be green for any
// operator (SoHoLINK holds the mock keys and drives the exercise itself).
func TestRunSuiteCScratch(t *testing.T) {
	passed, detail := runSuiteCScratch()
	if !passed {
		t.Fatalf("Suite C scratch exercise failed: %s", detail)
	}
}

// TestSuiteCThresholds pins the §4.2.2.1 distribution so a change is deliberate.
func TestSuiteCThresholds(t *testing.T) {
	want := []int{3, 6, 9, 12, 365}
	if len(suiteCThresholds) != len(want) {
		t.Fatalf("threshold count: got %d want %d", len(suiteCThresholds), len(want))
	}
	for i, v := range want {
		if suiteCThresholds[i] != v {
			t.Errorf("threshold[%d]: got %d want %d", i, suiteCThresholds[i], v)
		}
	}
	if suiteCFloor != 2 {
		t.Errorf("suiteC floor: got %d want 2", suiteCFloor)
	}
}

// TestGenerateCode confirms the 2FA code is six numeric digits, zero-padded.
func TestGenerateCode(t *testing.T) {
	for i := 0; i < 100; i++ {
		code, err := generateCode()
		if err != nil {
			t.Fatalf("generateCode: %v", err)
		}
		if len(code) != verificationCodeDigits {
			t.Fatalf("code %q length %d, want %d", code, len(code), verificationCodeDigits)
		}
		for _, c := range code {
			if c < '0' || c > '9' {
				t.Fatalf("code %q has non-digit %q", code, c)
			}
		}
	}
}

// TestFirstDiff spot-checks the FAIL-detail offset helper.
func TestFirstDiff(t *testing.T) {
	cases := []struct {
		a, b []byte
		want int
	}{
		{[]byte{1, 2, 3}, []byte{1, 2, 3}, 3},
		{[]byte{1, 2, 3}, []byte{1, 9, 3}, 1},
		{[]byte{1, 2}, []byte{1, 2, 3}, 2},
		{[]byte{}, []byte{1}, 0},
	}
	for i, c := range cases {
		if got := firstDiff(c.a, c.b); got != c.want {
			t.Errorf("case %d: firstDiff=%d want %d", i, got, c.want)
		}
	}
}
