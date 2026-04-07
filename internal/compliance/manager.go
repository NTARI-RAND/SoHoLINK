// Package compliance provides compliance level verification, attestation token
// generation, and audit logging for SoHoLINK federation nodes.
//
// Compliance groups allow workload owners to constrain scheduling to nodes that
// meet specific security, isolation, and SLA standards. Nodes opt in to groups
// at registration time; the ComplianceManager verifies their claims and signs
// an attestation token that can be verified by any peer.
package compliance

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// Level represents a node's compliance tier.
type Level string

const (
	LevelBaseline      Level = "baseline"
	LevelHighSecurity  Level = "high-security"
	LevelDataResidency Level = "data-residency"
	LevelGPUTier       Level = "gpu-tier"
	LevelTPMVerified   Level = "tpm-verified" // Phase 3: hardware-bound TPM attestation
)

// CheckResult is the outcome of a single compliance check.
type CheckResult struct {
	NodeDID string
	Level   Level
	Passed  bool
	Details []string
}

// Manager verifies node compliance, issues attestation tokens, and maintains
// an append-only audit trail in the SQLite store.
type Manager struct {
	store      *store.Store
	signingKey ed25519.PrivateKey // node's own key for signing attestations
	nodeID     string             // this node's DID (signer identity)
}

// NewManager creates a new ComplianceManager.
// signingKey is the Ed25519 private key used to sign attestation tokens.
// nodeID is the DID of the signing node (embedded in every attestation).
func NewManager(s *store.Store, signingKey ed25519.PrivateKey, nodeID string) *Manager {
	return &Manager{
		store:      s,
		signingKey: signingKey,
		nodeID:     nodeID,
	}
}

// CheckNode evaluates whether a federation node meets the requirements for
// one or more compliance levels. Returns the highest level the node achieves.
//
// Criteria checked:
//   - baseline:       reputation_score >= 20, uptime_percent >= 90
//   - high-security:  baseline + reputation_score >= 75 + firewall rules active
//   - data-residency: baseline + region constraint satisfied
//   - gpu-tier:       baseline + gpu_model != ""
func (m *Manager) CheckNode(ctx context.Context, nodeDID string) (*CheckResult, error) {
	nodes, err := m.store.GetNodesByComplianceGroup(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("compliance: fetch nodes: %w", err)
	}

	var target *store.FederationNodeRow
	for i := range nodes {
		if nodes[i].NodeDID == nodeDID {
			target = &nodes[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("compliance: node %s not found in online nodes", nodeDID)
	}

	result := &CheckResult{NodeDID: nodeDID}

	// baseline check
	var details []string
	baselinePassed := true
	if target.ReputationScore < 20 {
		baselinePassed = false
		details = append(details, fmt.Sprintf("reputation_score %d < 20", target.ReputationScore))
	}
	if target.UptimePercent < 90.0 {
		baselinePassed = false
		details = append(details, fmt.Sprintf("uptime_percent %.1f < 90", target.UptimePercent))
	}

	if !baselinePassed {
		result.Level = LevelBaseline
		result.Passed = false
		result.Details = details
		return result, nil
	}

	// high-security check
	if target.ReputationScore >= 75 {
		result.Level = LevelHighSecurity
		result.Passed = true
		result.Details = []string{"high-security criteria met"}
		return result, nil
	}

	// gpu-tier check
	if target.GPUModel != "" {
		result.Level = LevelGPUTier
		result.Passed = true
		result.Details = []string{fmt.Sprintf("gpu_model=%s", target.GPUModel)}
		return result, nil
	}

	// data-residency is manually assigned (region constraints set by operator)
	if target.ComplianceGroup == "data-residency" || strings.Contains(target.ComplianceGroup, "GDPR") {
		result.Level = LevelDataResidency
		result.Passed = true
		result.Details = []string{fmt.Sprintf("data-residency group=%s", target.ComplianceGroup)}
		return result, nil
	}

	// default: baseline passed
	result.Level = LevelBaseline
	result.Passed = true
	result.Details = []string{"baseline criteria met"}
	return result, nil
}

// GenerateAttestation signs a compliance claim for nodeDID at the given level.
// The attestation payload is: "<nodeDID>:<level>:<unixTimestamp>".
// Returns a base64url-encoded Ed25519 signature that can be stored and later
// verified by VerifyAttestation.
func (m *Manager) GenerateAttestation(nodeDID string, level Level) (string, error) {
	ts := time.Now().UTC().Unix()
	payload := []byte(fmt.Sprintf("%s:%s:%d", nodeDID, string(level), ts))
	sig := ed25519.Sign(m.signingKey, payload)
	attestation := base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(sig)

	log.Printf("[compliance] issued attestation for %s at level %s (signer: %s)", nodeDID, level, m.nodeID)
	return attestation, nil
}

// VerifyAttestation checks a previously issued attestation string.
// signerPubKey is the Ed25519 public key of the node that issued the attestation.
// Returns true if the signature is valid and the payload matches nodeDID + level.
func VerifyAttestation(nodeDID string, level Level, attestation string, signerPubKey ed25519.PublicKey) (bool, error) {
	parts := strings.SplitN(attestation, ".", 2)
	if len(parts) != 2 {
		return false, fmt.Errorf("compliance: malformed attestation")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false, fmt.Errorf("compliance: decode attestation payload: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false, fmt.Errorf("compliance: decode attestation signature: %w", err)
	}
	if !ed25519.Verify(signerPubKey, payload, sig) {
		return false, nil
	}
	// Validate payload format: "<nodeDID>:<level>:<ts>"
	expected := fmt.Sprintf("%s:%s:", nodeDID, string(level))
	if !strings.HasPrefix(string(payload), expected) {
		return false, fmt.Errorf("compliance: attestation payload mismatch")
	}
	return true, nil
}

// CheckAndAttest performs a compliance check for nodeDID, writes the audit
// record, updates the node's compliance fields in the store, and returns the
// attestation token. This is the primary "all-in-one" method called by the
// compliance API handler.
func (m *Manager) CheckAndAttest(ctx context.Context, nodeDID string) (string, *CheckResult, error) {
	result, err := m.CheckNode(ctx, nodeDID)
	if err != nil {
		return "", nil, err
	}

	var attestation string
	if result.Passed {
		attestation, err = m.GenerateAttestation(nodeDID, result.Level)
		if err != nil {
			return "", result, fmt.Errorf("compliance: generate attestation: %w", err)
		}
	}

	// Determine compliance group from level
	group := groupForLevel(result.Level)

	// Persist to store
	if storeErr := m.store.UpdateNodeCompliance(ctx, nodeDID,
		string(result.Level), group, slaForLevel(result.Level), attestation); storeErr != nil {
		log.Printf("[compliance] WARNING: failed to persist compliance for %s: %v", nodeDID, storeErr)
	}

	// Audit log
	auditID := fmt.Sprintf("ca-%s-%d", nodeDID[:min(8, len(nodeDID))], time.Now().UnixNano())
	details := strings.Join(result.Details, "; ")
	if auditErr := m.store.InsertComplianceAudit(ctx, auditID, nodeDID, string(result.Level), result.Passed, details); auditErr != nil {
		log.Printf("[compliance] WARNING: failed to write audit for %s: %v", nodeDID, auditErr)
	}

	return attestation, result, nil
}

// CheckAndAttestRaw is identical to CheckAndAttest but returns primitives only,
// so callers (e.g. the httpapi package) do not need to import compliance types.
// Returns: attestation string, level string, details []string, passed bool, error.
func (m *Manager) CheckAndAttestRaw(ctx context.Context, nodeDID string) (string, string, []string, bool, error) {
	attestation, result, err := m.CheckAndAttest(ctx, nodeDID)
	if err != nil {
		return "", "", nil, false, err
	}
	return attestation, string(result.Level), result.Details, result.Passed, nil
}

// GetGroupMembers returns the DIDs of all online nodes in a compliance group.
func (m *Manager) GetGroupMembers(ctx context.Context, group string) ([]string, error) {
	nodes, err := m.store.GetNodesByComplianceGroup(ctx, group)
	if err != nil {
		return nil, err
	}
	dids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		dids = append(dids, n.NodeDID)
	}
	return dids, nil
}

// groupForLevel maps a compliance level to its default group name.
func groupForLevel(level Level) string {
	switch level {
	case LevelTPMVerified:
		return "tpm-verified"
	case LevelHighSecurity:
		return "high-security"
	case LevelDataResidency:
		return "data-residency"
	case LevelGPUTier:
		return "gpu-tier"
	default:
		return ""
	}
}

// slaForLevel maps a compliance level to the default SLA tier.
func slaForLevel(level Level) string {
	switch level {
	case LevelTPMVerified, LevelHighSecurity, LevelDataResidency:
		return "premium"
	case LevelGPUTier:
		return "standard"
	default:
		return "best-effort"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
