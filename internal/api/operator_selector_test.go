package api

import (
	"net/http"
	"testing"
)

// sentinel flips *ran and writes 200, so a test can tell which arm of the
// selector handled the request.
func sentinel(ran *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*ran = true
		w.WriteHeader(http.StatusOK)
	})
}

// No operator header => the SPIFFE arm handles it and the operator verifier is
// never touched.
func TestOperatorOrSPIFFE_NoHeader_UsesSPIFFEPath(t *testing.T) {
	fv := &fakeVerifier{}
	var opRan, spiffeRan bool
	h := OperatorOrSPIFFE(fv, "soholink", sentinel(&spiffeRan), sentinel(&opRan))

	rec := doRequest(t, h, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !spiffeRan {
		t.Fatal("header-less request must take the SPIFFE arm")
	}
	if opRan {
		t.Fatal("operator arm must not run without the header")
	}
	if fv.getKeyMapCalls != 0 {
		t.Errorf("operator verifier must not be consulted on the SPIFFE arm, got %d calls", fv.getKeyMapCalls)
	}
}

// A valid operator header => the operator arm handles it; the SPIFFE arm is not
// consulted.
func TestOperatorOrSPIFFE_ValidHeader_UsesOperatorPath(t *testing.T) {
	privs, km := testKeyset(t)
	fv := &fakeVerifier{km: km}
	var opRan, spiffeRan bool
	h := OperatorOrSPIFFE(fv, "soholink", sentinel(&spiffeRan), sentinel(&opRan))

	tx := signedTransmission(t, privs, "cloudy", 1, 0, 3)
	rec := doRequest(t, h, EncodeOperatorHeader(tx))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !opRan {
		t.Fatal("valid transmission must take the operator arm")
	}
	if spiffeRan {
		t.Fatal("SPIFFE arm must not run when the operator header is present")
	}
}

// A present-but-malformed header => hard 401. It must NOT fall through to
// either arm — no downgrade to SPIFFE, no reaching the protected handler.
func TestOperatorOrSPIFFE_InvalidHeader_RejectsNoDowngrade(t *testing.T) {
	fv := &fakeVerifier{}
	var opRan, spiffeRan bool
	h := OperatorOrSPIFFE(fv, "soholink", sentinel(&spiffeRan), sentinel(&opRan))

	rec := doRequest(t, h, "!!!not-base64-json!!!")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for a malformed operator header, got %d", rec.Code)
	}
	if opRan || spiffeRan {
		t.Fatal("present-but-invalid header must reject, never fall through to either arm")
	}
}
