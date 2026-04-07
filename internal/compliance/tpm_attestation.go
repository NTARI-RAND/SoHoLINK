package compliance

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"time"

	itpm "github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/tpm"
)

// TPMAttestation holds a hardware-bound attestation claim produced by a
// TPM 2.0 device. It wraps the PCR quote with an Ed25519 software signature
// for backward compatibility with VerifyAttestation.
type TPMAttestation struct {
	NodeDID     string `json:"node_did"`
	Level       string `json:"level"`
	PCRQuote    string `json:"pcr_quote"`    // base64-encoded TPMS_ATTEST blob
	QuoteSig    string `json:"quote_sig"`    // base64-encoded TPM signature
	EKCert      string `json:"ek_cert"`      // base64-encoded DER EK certificate
	Nonce       string `json:"nonce"`        // base64-encoded anti-replay nonce
	AttestedAt  int64  `json:"attested_at"`  // Unix timestamp
	SoftwareSig string `json:"software_sig"` // Ed25519 attestation (compat)
	TPMPresent  bool   `json:"tpm_present"`  // false = software-only fallback
}

// GenerateTPMAttestation calls the TPM Attester to produce a hardware-bound
// claim, then wraps it in an Ed25519 software attestation for compatibility
// with VerifyAttestation. When the attester is not available (Available()==false),
// it falls back to a software-only attestation and sets TPMPresent = false.
func (m *Manager) GenerateTPMAttestation(
	ctx context.Context,
	nodeDID string,
	level Level,
	attester itpm.Attester,
	nonce []byte,
) (*TPMAttestation, string, error) {
	ta := &TPMAttestation{
		NodeDID:    nodeDID,
		Level:      string(level),
		Nonce:      base64.RawURLEncoding.EncodeToString(nonce),
		AttestedAt: time.Now().UTC().Unix(),
		TPMPresent: attester.Available(),
	}

	if attester.Available() {
		pcrIndices := []int{0, 1, 2, 3, 4, 7} // firmware + secure boot chain
		quote, sig, err := attester.Quote(ctx, pcrIndices, nonce)
		if err != nil {
			log.Printf("[compliance] TPM quote failed for %s: %v — falling back to software attestation", nodeDID, err)
			ta.TPMPresent = false
		} else {
			ta.PCRQuote = base64.RawURLEncoding.EncodeToString(quote)
			ta.QuoteSig = base64.RawURLEncoding.EncodeToString(sig)
		}

		if ekCert, err := attester.EKCert(ctx); err == nil {
			ta.EKCert = base64.RawURLEncoding.EncodeToString(ekCert)
		}
	}

	// Always generate a software Ed25519 attestation for compatibility
	softwareSig, err := m.GenerateAttestation(nodeDID, level)
	if err != nil {
		return nil, "", fmt.Errorf("compliance: TPM attestation software sig: %w", err)
	}
	ta.SoftwareSig = softwareSig

	return ta, softwareSig, nil
}

// CheckAndAttestTPM performs a compliance check for nodeDID, generates a
// TPM-bound attestation if hardware is available, persists the result, and
// returns the attestation string, result, and full TPMAttestation struct.
func (m *Manager) CheckAndAttestTPM(
	ctx context.Context,
	nodeDID string,
	attester itpm.Attester,
	nonce []byte,
) (string, *CheckResult, *TPMAttestation, error) {
	result, err := m.CheckNode(ctx, nodeDID)
	if err != nil {
		return "", nil, nil, err
	}

	// Promote to tpm-verified level if TPM is present and high-security baseline met
	if attester.Available() && result.Passed && result.Level == LevelHighSecurity {
		result.Level = LevelTPMVerified
		result.Details = append(result.Details, "tpm-verified: hardware attestation present")
	}

	ta, softwareSig, err := m.GenerateTPMAttestation(ctx, nodeDID, result.Level, attester, nonce)
	if err != nil {
		return "", result, nil, err
	}

	// Persist compliance fields
	group := groupForLevel(result.Level)
	if storeErr := m.store.UpdateNodeCompliance(ctx, nodeDID,
		string(result.Level), group, slaForLevel(result.Level), softwareSig); storeErr != nil {
		log.Printf("[compliance] WARNING: failed to persist TPM compliance for %s: %v", nodeDID, storeErr)
	}

	// Persist TPM attestation columns
	if storeErr := m.store.UpsertTPMAttestation(ctx, nodeDID,
		ta.EKCert, ta.PCRQuote, ta.QuoteSig, ta.Nonce, ta.AttestedAt); storeErr != nil {
		log.Printf("[compliance] WARNING: failed to persist TPM attestation for %s: %v", nodeDID, storeErr)
	}

	// Audit log
	auditID := fmt.Sprintf("tpm-%s-%d", nodeDID[:min(8, len(nodeDID))], time.Now().UnixNano())
	details := strings.Join(result.Details, "; ")
	if auditErr := m.store.InsertComplianceAudit(ctx, auditID, nodeDID, string(result.Level), result.Passed, details); auditErr != nil {
		log.Printf("[compliance] WARNING: failed to write TPM audit for %s: %v", nodeDID, auditErr)
	}

	// Append-only TPM attestation log
	logID := fmt.Sprintf("tpml-%s-%d", nodeDID[:min(8, len(nodeDID))], time.Now().UnixNano())
	if logErr := m.store.InsertTPMAttestationLog(ctx,
		logID, nodeDID, string(result.Level),
		ta.EKCert, ta.PCRQuote, ta.QuoteSig, ta.Nonce, softwareSig,
		ta.AttestedAt, ta.TPMPresent,
	); logErr != nil {
		log.Printf("[compliance] WARNING: failed to write TPM attestation log for %s: %v", nodeDID, logErr)
	}

	return softwareSig, result, ta, nil
}

// CheckAndAttestTPMRaw is the raw-primitive variant of CheckAndAttestTPM for
// use by httpapi handlers that cannot import compliance types directly.
// Returns: attestation, level, details, passed, tpmAttestation (JSON-ready struct as interface{}), error.
func (m *Manager) CheckAndAttestTPMRaw(
	ctx context.Context,
	nodeDID string,
	attester itpm.Attester,
	nonce []byte,
) (string, string, []string, bool, interface{}, error) {
	sig, result, ta, err := m.CheckAndAttestTPM(ctx, nodeDID, attester, nonce)
	if err != nil {
		return "", "", nil, false, nil, err
	}
	return sig, string(result.Level), result.Details, result.Passed, ta, nil
}
