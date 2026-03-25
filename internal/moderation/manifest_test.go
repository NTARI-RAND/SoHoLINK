package moderation

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ValidateManifest tests
// ---------------------------------------------------------------------------

func TestValidateManifest(t *testing.T) {
	validDescription := "This is a valid description that is at least twenty characters long."

	tests := []struct {
		name       string
		manifest   WorkloadManifest
		wantErrors int    // expected number of errors (0 = valid)
		wantSubstr string // if non-empty, at least one error must contain this
	}{
		{
			name: "fully valid manifest",
			manifest: WorkloadManifest{
				PurposeCategory: "data_processing",
				Description:     validDescription,
				NetworkAccess:   "none",
			},
			wantErrors: 0,
		},
		{
			name: "valid with declared_only and endpoints",
			manifest: WorkloadManifest{
				PurposeCategory:   "ml_training",
				Description:       validDescription,
				NetworkAccess:     "declared_only",
				ExternalEndpoints: []string{"https://api.example.com"},
			},
			wantErrors: 0,
		},
		{
			name: "valid with unrestricted network",
			manifest: WorkloadManifest{
				PurposeCategory: "rendering",
				Description:     validDescription,
				NetworkAccess:   "unrestricted",
			},
			wantErrors: 0,
		},
		{
			name: "valid hardware_access with scientific_compute",
			manifest: WorkloadManifest{
				PurposeCategory: "scientific_compute",
				Description:     validDescription,
				NetworkAccess:   "none",
				HardwareAccess:  true,
			},
			wantErrors: 0,
		},
		{
			name: "missing purpose_category",
			manifest: WorkloadManifest{
				Description:   validDescription,
				NetworkAccess: "none",
			},
			wantErrors: 1,
			wantSubstr: "purpose_category is required",
		},
		{
			name: "invalid purpose_category",
			manifest: WorkloadManifest{
				PurposeCategory: "hacking",
				Description:     validDescription,
				NetworkAccess:   "none",
			},
			wantErrors: 1,
			wantSubstr: "purpose_category must be one of",
		},
		{
			name: "description too short",
			manifest: WorkloadManifest{
				PurposeCategory: "rendering",
				Description:     "short",
				NetworkAccess:   "none",
			},
			wantErrors: 1,
			wantSubstr: "description must be at least 20 characters",
		},
		{
			name: "description is only whitespace",
			manifest: WorkloadManifest{
				PurposeCategory: "rendering",
				Description:     "                    ",
				NetworkAccess:   "none",
			},
			wantErrors: 1,
			wantSubstr: "description must be at least 20 characters",
		},
		{
			name: "missing network_access",
			manifest: WorkloadManifest{
				PurposeCategory: "rendering",
				Description:     validDescription,
			},
			wantErrors: 1,
			wantSubstr: "network_access is required",
		},
		{
			name: "invalid network_access value",
			manifest: WorkloadManifest{
				PurposeCategory: "rendering",
				Description:     validDescription,
				NetworkAccess:   "open",
			},
			wantErrors: 1,
			wantSubstr: "network_access must be none, declared_only, or unrestricted",
		},
		{
			name: "declared_only without endpoints",
			manifest: WorkloadManifest{
				PurposeCategory:   "rendering",
				Description:       validDescription,
				NetworkAccess:     "declared_only",
				ExternalEndpoints: nil,
			},
			wantErrors: 1,
			wantSubstr: "external_endpoints must list at least one endpoint",
		},
		{
			name: "hardware_access with disallowed category",
			manifest: WorkloadManifest{
				PurposeCategory: "rendering",
				Description:     validDescription,
				NetworkAccess:   "none",
				HardwareAccess:  true,
			},
			wantErrors: 1,
			wantSubstr: "hardware_access=true requires purpose_category",
		},
		{
			name: "multiple errors at once",
			manifest: WorkloadManifest{
				PurposeCategory: "",
				Description:     "short",
				NetworkAccess:   "",
			},
			wantErrors: 3, // purpose, description, network_access
		},
		{
			name: "all allowed purpose categories accepted",
			manifest: WorkloadManifest{
				PurposeCategory: "conferencing",
				Description:     validDescription,
				NetworkAccess:   "none",
			},
			wantErrors: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateManifest(&tc.manifest)
			if len(errs) != tc.wantErrors {
				t.Errorf("got %d errors, want %d:\n%s", len(errs), tc.wantErrors, strings.Join(errs, "\n"))
			}
			if tc.wantSubstr != "" {
				found := false
				for _, e := range errs {
					if strings.Contains(e, tc.wantSubstr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", tc.wantSubstr, errs)
				}
			}
		})
	}
}

func TestValidateManifest_AllPurposeCategories(t *testing.T) {
	validDescription := "This is a valid description that is at least twenty characters long."
	for cat := range AllowedPurposeCategories {
		t.Run("category_"+cat, func(t *testing.T) {
			m := &WorkloadManifest{
				PurposeCategory: cat,
				Description:     validDescription,
				NetworkAccess:   "none",
			}
			errs := ValidateManifest(m)
			for _, e := range errs {
				if strings.Contains(e, "purpose_category") {
					t.Errorf("category %q should be valid, got error: %s", cat, e)
				}
			}
		})
	}
}

func TestValidateManifest_HardwareAccessAllowedCategories(t *testing.T) {
	validDescription := "This is a valid description that is at least twenty characters long."
	allowed := []string{"security_monitoring", "scientific_compute", "other"}
	for _, cat := range allowed {
		t.Run("hw_allowed_"+cat, func(t *testing.T) {
			m := &WorkloadManifest{
				PurposeCategory: cat,
				Description:     validDescription,
				NetworkAccess:   "none",
				HardwareAccess:  true,
			}
			errs := ValidateManifest(m)
			for _, e := range errs {
				if strings.Contains(e, "hardware_access") {
					t.Errorf("hardware_access should be allowed for %q, got: %s", cat, e)
				}
			}
		})
	}
}

func TestValidateManifest_DescriptionExactly20Chars(t *testing.T) {
	m := &WorkloadManifest{
		PurposeCategory: "rendering",
		Description:     "12345678901234567890", // exactly 20 chars
		NetworkAccess:   "none",
	}
	errs := ValidateManifest(m)
	if len(errs) != 0 {
		t.Errorf("description of exactly 20 chars should be valid, got errors: %v", errs)
	}
}

func TestValidateManifest_Description19Chars(t *testing.T) {
	m := &WorkloadManifest{
		PurposeCategory: "rendering",
		Description:     "1234567890123456789", // 19 chars
		NetworkAccess:   "none",
	}
	errs := ValidateManifest(m)
	if len(errs) != 1 {
		t.Errorf("expected 1 error for 19-char description, got %d: %v", len(errs), errs)
	}
}

// ---------------------------------------------------------------------------
// NewPassthroughSafetyPolicy tests
// ---------------------------------------------------------------------------

func TestNewPassthroughSafetyPolicy(t *testing.T) {
	sp := NewPassthroughSafetyPolicy()
	if sp == nil {
		t.Fatal("NewPassthroughSafetyPolicy returned nil")
	}
	if sp.enabled {
		t.Fatal("passthrough policy should not be enabled")
	}
}

func TestPassthroughSafetyPolicy_AllowsEverything(t *testing.T) {
	sp := NewPassthroughSafetyPolicy()
	ctx := context.Background()

	manifests := []WorkloadManifest{
		{PurposeCategory: "rendering", Description: "test", NetworkAccess: "unrestricted"},
		{PurposeCategory: "ml_training", Description: "test", NetworkAccess: "none", HardwareAccess: true},
		{}, // even empty manifest
	}

	for i, m := range manifests {
		allowed, reasons, err := sp.Allow(ctx, m, "")
		if err != nil {
			t.Errorf("manifest %d: unexpected error: %v", i, err)
		}
		if !allowed {
			t.Errorf("manifest %d: passthrough should allow everything", i)
		}
		if len(reasons) != 0 {
			t.Errorf("manifest %d: passthrough should have no deny reasons, got: %v", i, reasons)
		}
	}
}

func TestPassthroughSafetyPolicy_AllowWithCID(t *testing.T) {
	sp := NewPassthroughSafetyPolicy()
	ctx := context.Background()
	m := WorkloadManifest{
		PurposeCategory: "data_processing",
		Description:     "long enough description for validation",
		NetworkAccess:   "none",
	}
	allowed, _, err := sp.Allow(ctx, m, "QmSomeCID12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("passthrough should allow even with CID")
	}
}

// ---------------------------------------------------------------------------
// ErrDIDBlocked / ErrContentBlocked sentinel tests
// ---------------------------------------------------------------------------

func TestSentinelErrors(t *testing.T) {
	if ErrDIDBlocked == nil {
		t.Fatal("ErrDIDBlocked should not be nil")
	}
	if ErrContentBlocked == nil {
		t.Fatal("ErrContentBlocked should not be nil")
	}
	if !strings.Contains(ErrDIDBlocked.Error(), "suspended") {
		t.Errorf("ErrDIDBlocked message unexpected: %s", ErrDIDBlocked.Error())
	}
	if !strings.Contains(ErrContentBlocked.Error(), "blocked") {
		t.Errorf("ErrContentBlocked message unexpected: %s", ErrContentBlocked.Error())
	}
}
