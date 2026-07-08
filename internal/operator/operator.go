// Package operator is SoHoLINK's coordinator-side integration of the
// sohocloud-protocol operator identity layer ("Layer C": frontend-to-coordinator
// authentication). This file establishes the dependency seam only; the verify
// chokepoint (GetActiveKeyMap), invite-gated creation, keyset-hash binding,
// replay CAS, and lazy-expiry described in docs/operator-onboarding-design.md
// §3 are added in a later step. Nothing here authenticates anything yet.
//
// The types below re-export the protocol's operator vocabulary so the rest of
// SoHoLINK refers to a single canonical definition rather than duplicating the
// wire shapes. The protocol package is the source of truth for the canonical
// byte format, the 2-of-7 discipline, and the domain tags.
package operator

import protoop "github.com/NTARI-RAND/sohocloud-protocol/operator"

// KeyRecord is a registered operator public key at one index. SoHoLINK stores
// only public keys; it never holds an operator private key.
type KeyRecord = protoop.KeyRecord

// OperatorTransmission is the frontend's 2-of-7-signed transmission to the
// coordinator. SoHoLINK verifies these on the node-side seam (later step).
type OperatorTransmission = protoop.OperatorTransmission

// OperatorRotation authorizes swapping a new public key at one index, signed by
// two current active keys.
type OperatorRotation = protoop.OperatorRotation

// ConformanceResponse is an operator's signed response to a conformance
// challenge; its distinct domain tag domain-separates it from a live
// transmission.
type ConformanceResponse = protoop.ConformanceResponse

// Protocol-level constants surfaced for the data layer and later verify path.
const (
	// KeyIndexCount is the number of keypairs an operator holds (indices 0..6).
	KeyIndexCount = protoop.KeyIndexCount
	// MinNonceLen is the minimum per-transmission nonce length.
	MinNonceLen = protoop.MinNonceLen
	// AlgoEd25519 is the v0 algorithm string bound into canonical bytes.
	AlgoEd25519 = protoop.AlgoEd25519
)
