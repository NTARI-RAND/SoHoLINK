package orchestrator

import (
	"strings"
	"testing"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/types"
)

func TestMarketplaceToAgent_AllKnownValues(t *testing.T) {
	for _, w := range types.AllMarketplaceWorkloadTypes() {
		got, err := MarketplaceToAgent(w)
		if err != nil {
			t.Errorf("MarketplaceToAgent(%q) unexpected error: %v", w, err)
			continue
		}
		if got == "" {
			t.Errorf("MarketplaceToAgent(%q) returned empty agent type", w)
		}
	}
}

func TestMarketplaceToAgent_UnknownReturnsError(t *testing.T) {
	_, err := MarketplaceToAgent(types.MarketplaceWorkloadType("unknown_future_type"))
	if err == nil {
		t.Fatal("expected error for unknown marketplace type, got nil")
	}
	if !strings.Contains(err.Error(), "unknown_future_type") {
		t.Errorf("error %q does not name the unknown value", err.Error())
	}
}

func TestMustValidateWorkloadMapping_PassesWhenComplete(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MustValidateWorkloadMapping panicked unexpectedly: %v", r)
		}
	}()
	MustValidateWorkloadMapping()
}

func TestMustValidateWorkloadMapping_PanicsOnMissing(t *testing.T) {
	missing := types.MarketplaceObjectStorage
	saved := marketplaceToAgent[missing]
	delete(marketplaceToAgent, missing)
	t.Cleanup(func() { marketplaceToAgent[missing] = saved })

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for incomplete mapping, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value is not a string: %v", r)
		}
		if !strings.Contains(msg, string(missing)) {
			t.Errorf("panic message %q does not name the missing value %q", msg, missing)
		}
	}()
	MustValidateWorkloadMapping()
}
