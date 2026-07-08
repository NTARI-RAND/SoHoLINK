package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/sounding"
)

// OperatorHeader is the transport for an operator transmission on the node-side
// seam: the base64 of a JSON-encoded operatorTransmissionWire in the
// X-SohoCloud-Operator request header. Ed25519 signatures are 64 bytes so a
// compact form fits a header; a JSON body form is the documented fallback for
// the ML-DSA swap (much larger signatures). The design (§6) permits either; this
// build uses the header form for the node-side handlers.
const OperatorHeader = "X-SohoCloud-Operator"

// SwapRequiredHeader is set on a successful response when one of the two signing
// keys has reached its expiration threshold, signalling the operator to rotate.
const SwapRequiredHeader = "X-Operator-Swap-Required"

// operatorTimestampWindow is the +/- freshness window applied to the signed
// TsUnixNano. Checked against the exact signed int64, never round-tripped
// through time.Time (SPEC §11.1 / design §6 step 4).
const operatorTimestampWindow = 5 * time.Minute

// operatorNonceRetention is how long a consumed nonce is retained in
// operator_nonces. It must be >= operatorTimestampWindow so a nonce cannot be
// replayed within the freshness window after its row would otherwise be pruned.
const operatorNonceRetention = 10 * time.Minute

// operatorTransmissionWire is the JSON shape carried (base64-encoded) in the
// OperatorHeader. It maps 1:1 onto operator.OperatorTransmission. Byte fields
// are base64 std-encoded.
type operatorTransmissionWire struct {
	OperatorID string `json:"operator_id"`
	TsUnixNano int64  `json:"ts_unix_nano"`
	Nonce      string `json:"nonce"` // base64 std
	Seq        uint64 `json:"seq"`
	Algo       string `json:"algo"`
	Idx0       int    `json:"idx0"`
	Idx1       int    `json:"idx1"`
	Sig0       string `json:"sig0"` // base64 std
	Sig1       string `json:"sig1"` // base64 std
}

// operatorContextKey is the private context key under which a verified
// operator's identity + signing indices are stored for RequireOperator.
type operatorContextKey struct{}

// VerifiedOperator is the identity a successful OperatorAuth places in the
// request context, analogous to the SPIFFE ID placed by RequireSPIFFE.
type VerifiedOperator struct {
	OperatorID string
	Idx0, Idx1 int
}

// operatorVerifier is the subset of *operator.Repository the middleware needs.
// Defined as an interface so tests can substitute a fake without a database.
type operatorVerifier interface {
	GetActiveKeyMap(ctx context.Context, operatorID string) (map[int]operator.KeyRecord, error)
	CheckAndAdvance(ctx context.Context, operatorID, coordinatorID string, seq uint64, nonce []byte, scope string, nonceExpiry time.Time) error
	RecordUsageAndExpire(ctx context.Context, operatorID string, idx0, idx1 int) (bool, error)
}

// OperatorAuth wraps a node-side handler so it REQUIRES a valid 2-of-7 operator
// transmission. This is mutually exclusive per route, NOT additive: a handler
// wrapped with OperatorAuth demands a present-and-valid token; absence is a hard
// 401, never a fall-through to a weaker authenticator. An attacker therefore
// cannot strip the header to downgrade the route.
//
// coordinatorID identifies this coordinator for the per-(operator,coordinator)
// anti-replay window. The verify order mirrors design §6:
//
//  1. header present and decodable,
//     2-7. pure protocol Verify against GetActiveKeyMap (nonce length, algo,
//     distinct indices, index range, key presence, algo-pin, sig validity),
//     where a nil keymap (non-active operator) fails closed,
//  8. anti-replay CAS (CheckAndAdvance) in one fail-closed transaction,
//  9. RecordUsageAndExpire (downstream of CAS => exactly-once), setting
//     X-Operator-Swap-Required on threshold crossing.
func OperatorAuth(repo operatorVerifier, coordinatorID string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get(OperatorHeader)
		if raw == "" {
			// REQUIRED, not additive: absence is a hard reject.
			writeError(w, http.StatusUnauthorized, "operator transmission required")
			return
		}

		tx, err := decodeOperatorHeader(raw)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "malformed operator transmission")
			return
		}

		// Freshness on the EXACT signed int64 (never through time.Time). Done
		// before the DB/verify work so a stale transmission is cheap to reject.
		nowNano := time.Now().UnixNano()
		windowNano := int64(operatorTimestampWindow)
		if delta := nowNano - tx.TsUnixNano; delta > windowNano || delta < -windowNano {
			writeError(w, http.StatusUnauthorized, "operator transmission timestamp outside freshness window")
			return
		}

		// Steps 5-7: obtain keys via the single chokepoint and run pure Verify.
		// A nil keymap (pending/verified-but-not-active/revoked operator, or a
		// mixed-algo active set) means "authenticate nothing" — Verify against a
		// nil map fails closed (no key at any index). A DB error is also
		// fail-closed.
		km, err := repo.GetActiveKeyMap(r.Context(), tx.OperatorID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "operator not authenticated")
			return
		}
		if err := tx.Verify(km); err != nil {
			writeError(w, http.StatusUnauthorized, "operator transmission verification failed")
			return
		}

		// Step 8: anti-replay CAS in one fail-closed transaction. Any error
		// (replay or DB failure) rejects — never fail open.
		nonceExpiry := time.Now().Add(operatorNonceRetention)
		if err := repo.CheckAndAdvance(r.Context(), tx.OperatorID, coordinatorID, tx.Seq, tx.Nonce, "production", nonceExpiry); err != nil {
			writeError(w, http.StatusUnauthorized, "operator transmission replay rejected")
			return
		}

		// Step 9: usage accounting + lazy expiry, downstream of the CAS so a
		// replayed transmission (already rejected) never double-counts. A
		// failure here does not undo the accepted transmission; log-and-proceed
		// would leave usage under-counted, so treat a failure as fail-closed to
		// keep expiry accounting sound.
		swapRequired, err := repo.RecordUsageAndExpire(r.Context(), tx.OperatorID, tx.Idx0, tx.Idx1)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "operator usage accounting failed")
			return
		}
		if swapRequired {
			w.Header().Set(SwapRequiredHeader, "true")
		}

		ctx := context.WithValue(r.Context(), operatorContextKey{}, VerifiedOperator{
			OperatorID: tx.OperatorID,
			Idx0:       tx.Idx0,
			Idx1:       tx.Idx1,
		})
		// Bridge the verified operator identity onto the demand-sounding context
		// key so instrumentation deep in the call stack (e.g. orchestrator
		// SubmitJob, which cannot import this package) records the real
		// operator_id without any further plumbing. Observation only.
		ctx = sounding.ContextWithOperatorID(ctx, tx.OperatorID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OperatorFromContext retrieves the VerifiedOperator stored by OperatorAuth.
// Returns false if the request did not pass through OperatorAuth.
func OperatorFromContext(ctx context.Context) (VerifiedOperator, bool) {
	v, ok := ctx.Value(operatorContextKey{}).(VerifiedOperator)
	return v, ok
}

// decodeOperatorHeader base64-decodes and JSON-parses the OperatorHeader into a
// protocol OperatorTransmission. It decodes the byte fields (nonce, sigs) but
// performs NO cryptographic checks — those are the protocol Verify's job.
func decodeOperatorHeader(raw string) (operator.OperatorTransmission, error) {
	jsonBytes, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return operator.OperatorTransmission{}, err
	}
	var wire operatorTransmissionWire
	if err := json.Unmarshal(jsonBytes, &wire); err != nil {
		return operator.OperatorTransmission{}, err
	}
	nonce, err := base64.StdEncoding.DecodeString(wire.Nonce)
	if err != nil {
		return operator.OperatorTransmission{}, err
	}
	sig0, err := base64.StdEncoding.DecodeString(wire.Sig0)
	if err != nil {
		return operator.OperatorTransmission{}, err
	}
	sig1, err := base64.StdEncoding.DecodeString(wire.Sig1)
	if err != nil {
		return operator.OperatorTransmission{}, err
	}
	return operator.OperatorTransmission{
		OperatorID: wire.OperatorID,
		TsUnixNano: wire.TsUnixNano,
		Nonce:      nonce,
		Seq:        wire.Seq,
		Algo:       wire.Algo,
		Idx0:       wire.Idx0,
		Idx1:       wire.Idx1,
		Sig0:       sig0,
		Sig1:       sig1,
	}, nil
}

// EncodeOperatorHeader is the client-side counterpart to decodeOperatorHeader:
// it serializes a signed transmission to the base64 header value. Exported so
// the conformance harness (Suite B) and tests can build the header exactly as a
// real operator would.
func EncodeOperatorHeader(tx operator.OperatorTransmission) string {
	wire := operatorTransmissionWire{
		OperatorID: tx.OperatorID,
		TsUnixNano: tx.TsUnixNano,
		Nonce:      base64.StdEncoding.EncodeToString(tx.Nonce),
		Seq:        tx.Seq,
		Algo:       tx.Algo,
		Idx0:       tx.Idx0,
		Idx1:       tx.Idx1,
		Sig0:       base64.StdEncoding.EncodeToString(tx.Sig0),
		Sig1:       base64.StdEncoding.EncodeToString(tx.Sig1),
	}
	jsonBytes, _ := json.Marshal(wire) //nolint:errcheck // wire has no unmarshalable fields
	return base64.StdEncoding.EncodeToString(jsonBytes)
}
