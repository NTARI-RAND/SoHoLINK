package store

import (
	"context"
	"fmt"
)

// TPMAttestationRow represents a row from the tpm_attestations table.
type TPMAttestationRow struct {
	AttestationID string
	NodeDID       string
	Level         string
	TPMEKCert     string
	TPMPCRQuote   string
	TPMQuoteSig   string
	Nonce         string
	SoftwareSig   string
	AttestedAt    int64
	Verified      bool
}

// UpsertTPMAttestation updates the TPM attestation columns on federation_nodes.
func (s *Store) UpsertTPMAttestation(ctx context.Context,
	nodeDID, ekCert, pcrQuote, quoteSig, nonce string, ts int64,
) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE federation_nodes
		SET tpm_ek_cert     = ?,
		    tpm_pcr_quote   = ?,
		    tpm_quote_sig   = ?,
		    tpm_quote_nonce = ?,
		    tpm_quote_ts    = ?
		WHERE node_did = ?`,
		ekCert, pcrQuote, quoteSig, nonce, ts, nodeDID,
	)
	return err
}

// InsertTPMAttestationLog appends an entry to the tpm_attestations audit table.
func (s *Store) InsertTPMAttestationLog(ctx context.Context,
	id, nodeDID, level, ekCert, pcrQuote, quoteSig, nonce, softwareSig string,
	ts int64, verified bool,
) error {
	verifiedInt := 0
	if verified {
		verifiedInt = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tpm_attestations
		    (attestation_id, node_did, level, tpm_ek_cert, tpm_pcr_quote,
		     tpm_quote_sig, nonce, software_sig, attested_at, verified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, nodeDID, level, ekCert, pcrQuote, quoteSig, nonce, softwareSig, ts, verifiedInt,
	)
	return err
}

// GetLatestTPMAttestation returns the most recent TPM attestation for a node.
func (s *Store) GetLatestTPMAttestation(ctx context.Context, nodeDID string) (*TPMAttestationRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT attestation_id, node_did, level, tpm_ek_cert, tpm_pcr_quote,
		       tpm_quote_sig, nonce, software_sig, attested_at, verified
		FROM tpm_attestations
		WHERE node_did = ?
		ORDER BY attested_at DESC
		LIMIT 1`,
		nodeDID,
	)

	var r TPMAttestationRow
	var verifiedInt int
	err := row.Scan(
		&r.AttestationID, &r.NodeDID, &r.Level,
		&r.TPMEKCert, &r.TPMPCRQuote, &r.TPMQuoteSig,
		&r.Nonce, &r.SoftwareSig, &r.AttestedAt, &verifiedInt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: GetLatestTPMAttestation: %w", err)
	}
	r.Verified = verifiedInt == 1
	return &r, nil
}
