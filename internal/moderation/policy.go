package moderation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/open-policy-agent/opa/rego"
)

// SafetyPolicy evaluates workload submissions against the platform-level OPA
// safety prohibition rules. These rules apply to all workloads regardless of
// provider configuration and cannot be overridden by individual nodes.
type SafetyPolicy struct {
	prohibitionsQuery rego.PreparedEvalQuery
	egressQuery       rego.PreparedEvalQuery
	enabled           bool
}

// NewSafetyPolicy loads and compiles the OPA safety policy files.
// prohibitionsPath and egressPath are paths to the .rego files.
// If either file does not exist, the policy is loaded in passthrough mode
// (all workloads allowed) with a warning log.
func NewSafetyPolicy(prohibitionsPath, egressPath string) (*SafetyPolicy, error) {
	sp := &SafetyPolicy{}

	prohibContents, err := os.ReadFile(prohibitionsPath)
	if err != nil {
		log.Printf("[moderation/policy] WARNING: safety_prohibitions.rego not found (%v) — policy disabled", err)
		return sp, nil
	}

	egressContents, err := os.ReadFile(egressPath)
	if err != nil {
		log.Printf("[moderation/policy] WARNING: network_egress.rego not found (%v) — policy disabled", err)
		return sp, nil
	}

	prohibQuery, err := rego.New(
		rego.Query("data.soholink.safety.allow"),
		rego.Module("safety_prohibitions.rego", string(prohibContents)),
	).PrepareForEval(context.Background())
	if err != nil {
		return nil, fmt.Errorf("compile safety prohibitions policy: %w", err)
	}

	egressQuery, err := rego.New(
		rego.Query("data.soholink.network.deny_private_network"),
		rego.Module("network_egress.rego", string(egressContents)),
	).PrepareForEval(context.Background())
	if err != nil {
		return nil, fmt.Errorf("compile network egress policy: %w", err)
	}

	sp.prohibitionsQuery = prohibQuery
	sp.egressQuery = egressQuery
	sp.enabled = true
	return sp, nil
}

// NewPassthroughSafetyPolicy creates a policy that allows everything.
// Used when OPA policy files are not available (e.g. in tests).
func NewPassthroughSafetyPolicy() *SafetyPolicy {
	return &SafetyPolicy{enabled: false}
}

// Allow evaluates the manifest against safety prohibition and network egress
// policies. Returns (allowed bool, denyReasons []string, err error).
//
// cid is the IPFS CID of uploaded content (if any); pass "" for workload-only
// submissions that have no associated content hash.
func (p *SafetyPolicy) Allow(ctx context.Context, manifest WorkloadManifest, cid string) (bool, []string, error) {
	if !p.enabled {
		return true, nil, nil
	}

	// Build input document
	input := map[string]interface{}{
		"cid": cid,
		"manifest": map[string]interface{}{
			"purpose_category":    manifest.PurposeCategory,
			"description":         manifest.Description,
			"network_access":      manifest.NetworkAccess,
			"external_endpoints":  manifest.ExternalEndpoints,
			"hardware_access":     manifest.HardwareAccess,
			"capabilities":        manifest.Capabilities,
			"output_destinations": manifest.OutputDestinations,
			"data_sources":        manifest.DataSources,
		},
	}

	// Evaluate prohibition rules
	results, err := p.prohibitionsQuery.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return false, nil, fmt.Errorf("safety policy eval: %w", err)
	}

	allowed := true
	if len(results) > 0 && len(results[0].Expressions) > 0 {
		if v, ok := results[0].Expressions[0].Value.(bool); ok {
			allowed = v
		}
	}

	// Collect deny_reasons if not allowed
	var denyReasons []string
	if !allowed {
		reasonsQuery, err2 := rego.New(
			rego.Query("data.soholink.safety.deny_reasons"),
		).PrepareForEval(ctx)
		if err2 == nil {
			rResults, rErr := reasonsQuery.Eval(ctx, rego.EvalInput(input))
			if rErr == nil && len(rResults) > 0 && len(rResults[0].Expressions) > 0 {
				if b, jsonErr := json.Marshal(rResults[0].Expressions[0].Value); jsonErr == nil {
					json.Unmarshal(b, &denyReasons) //nolint:errcheck
				}
			}
		}
		if len(denyReasons) == 0 {
			denyReasons = []string{"workload_rejected_by_safety_policy"}
		}
	}

	// Evaluate egress rules (RFC 1918 blocking)
	egressResults, err := p.egressQuery.Eval(ctx, rego.EvalInput(input))
	if err == nil && len(egressResults) > 0 && len(egressResults[0].Expressions) > 0 {
		if denied, ok := egressResults[0].Expressions[0].Value.(bool); ok && denied {
			allowed = false
			denyReasons = append(denyReasons, "private_network_endpoint_declared")
		}
	}

	if !allowed {
		log.Printf("[moderation/policy] workload DENIED — reasons: %v", denyReasons)
	}

	return allowed, denyReasons, nil
}

// CheckEgress evaluates whether a workload is permitted to make outbound
// connections to destIP. Returns (allowed bool, error).
//
// This is called at workload start time to build the static iptables OUTPUT
// ACCEPT list — destinations not in the allowed list are blocked by the
// default-deny OUTPUT DROP rule applied by the WorkloadExecutor.
//
// A destination is denied when deny_private_network is true for that IP
// (RFC 1918, loopback, link-local, or IPv6 ULA space).
func (p *SafetyPolicy) CheckEgress(ctx context.Context, destIP string) (bool, error) {
	if !p.enabled {
		return true, nil
	}

	input := map[string]interface{}{
		"manifest": map[string]interface{}{
			"external_endpoints": []string{destIP},
			"network_access":     "declared_only",
		},
	}

	results, err := p.egressQuery.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return false, fmt.Errorf("egress policy eval: %w", err)
	}

	if len(results) > 0 && len(results[0].Expressions) > 0 {
		if denied, ok := results[0].Expressions[0].Value.(bool); ok && denied {
			log.Printf("[moderation/policy] egress to %s DENIED by network_egress policy", destIP)
			return false, nil
		}
	}

	return true, nil
}
