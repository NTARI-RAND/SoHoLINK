package types_test

import (
	"strings"
	"testing"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/types"
)

func TestIsValid_AllKnownValuesPass(t *testing.T) {
	for _, w := range types.AllMarketplaceWorkloadTypes() {
		if !w.IsValid() {
			t.Errorf("AllMarketplaceWorkloadTypes() contains invalid value %q", w)
		}
	}
}

func TestParseMarketplaceWorkloadType_RoundTrips(t *testing.T) {
	for _, w := range types.AllMarketplaceWorkloadTypes() {
		got, err := types.ParseMarketplaceWorkloadType(string(w))
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", w, err)
		}
		if got != w {
			t.Errorf("Parse(%q) = %q, want %q", w, got, w)
		}
	}
}

func TestParseMarketplaceWorkloadType_RejectsUnknown(t *testing.T) {
	cases := []string{"", "banana", "inference", "compute"}
	for _, s := range cases {
		_, err := types.ParseMarketplaceWorkloadType(s)
		if err == nil {
			t.Errorf("Parse(%q) expected error, got nil", s)
			continue
		}
		if s != "" && !strings.Contains(err.Error(), s) {
			t.Errorf("Parse(%q) error %q does not mention the unknown value", s, err.Error())
		}
	}
}
