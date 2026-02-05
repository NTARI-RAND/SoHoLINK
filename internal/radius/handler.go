package radius

import (
	"context"
	"log"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/accounting"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/policy"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/verifier"
)

// Handler processes RADIUS authentication and accounting requests.
type Handler struct {
	verifier   *verifier.Verifier
	policyEng  *policy.Engine
	accounting *accounting.Collector
}

// HandleAuth processes RADIUS Access-Request packets.
func (h *Handler) HandleAuth(w radius.ResponseWriter, r *radius.Request) {
	start := time.Now()

	// Extract credentials from RADIUS packet (PAP)
	username := rfc2865.UserName_GetString(r.Packet)
	password := rfc2865.UserPassword_GetString(r.Packet)
	nasIP := rfc2865.NASIPAddress_Get(r.Packet)
	nasID := rfc2865.NASIdentifier_GetString(r.Packet)
	clientAddr := r.RemoteAddr.String()

	if username == "" {
		log.Printf("[radius] auth: rejected - no username from %s", clientAddr)
		h.sendReject(w, r, "missing username")
		h.recordEvent("auth_failure", "", username, clientAddr, nasID, "DENY", "missing_username", start)
		return
	}

	if password == "" {
		log.Printf("[radius] auth: rejected - no password for user '%s' from %s", username, clientAddr)
		h.sendReject(w, r, "missing credential token")
		h.recordEvent("auth_failure", "", username, clientAddr, nasID, "DENY", "missing_credential", start)
		return
	}

	// Step 1: Verify credentials (Ed25519 signature, expiration, revocation)
	ctx := context.Background()
	result, err := h.verifier.Verify(ctx, username, password)
	if err != nil {
		log.Printf("[radius] auth: internal error for user '%s': %v", username, err)
		h.sendReject(w, r, "internal error")
		h.recordEvent("auth_error", "", username, clientAddr, nasID, "DENY", "internal_error", start)
		return
	}

	if !result.Allowed {
		log.Printf("[radius] auth: denied user '%s': %s", username, result.Reason)
		h.sendReject(w, r, result.Reason)
		h.recordEvent("auth_failure", result.DID, username, clientAddr, nasID, "DENY", result.Reason, start)
		return
	}

	// Step 2: Evaluate authorization policy
	policyInput := &policy.AuthzInput{
		User:          result.Username,
		DID:           result.DID,
		Role:          result.Role,
		Authenticated: true,
		NASAddress:    nasIP.String(),
		Resource:      "network_access",
		Timestamp:     time.Now(),
	}

	policyResult, err := h.policyEng.Evaluate(ctx, policyInput)
	if err != nil {
		log.Printf("[radius] auth: policy error for user '%s': %v", username, err)
		h.sendReject(w, r, "policy evaluation error")
		h.recordEvent("auth_error", result.DID, username, clientAddr, nasID, "DENY", "policy_error", start)
		return
	}

	if !policyResult.Allow {
		reason := "policy_denied"
		if len(policyResult.DenyReasons) > 0 {
			reason = policyResult.DenyReasons[0]
		}
		log.Printf("[radius] auth: policy denied user '%s': %v", username, policyResult.DenyReasons)
		h.sendReject(w, r, reason)
		h.recordEvent("auth_failure", result.DID, username, clientAddr, nasID, "DENY", reason, start)
		return
	}

	// Step 3: Accept
	log.Printf("[radius] auth: accepted user '%s' (DID=%s, role=%s) in %v",
		username, result.DID[:20]+"...", result.Role, time.Since(start))

	resp := r.Response(radius.CodeAccessAccept)
	rfc2865.ReplyMessage_SetString(resp, "Welcome, "+username)
	w.Write(resp)

	h.recordEvent("auth_success", result.DID, username, clientAddr, nasID, "ALLOW", "authenticated", start)
}

// HandleAccounting processes RADIUS Accounting-Request packets.
func (h *Handler) HandleAccounting(w radius.ResponseWriter, r *radius.Request) {
	username := rfc2865.UserName_GetString(r.Packet)
	acctStatusType := rfc2866.AcctStatusType_Get(r.Packet)
	sessionID := rfc2866.AcctSessionID_GetString(r.Packet)
	clientAddr := r.RemoteAddr.String()

	var eventType string
	switch acctStatusType {
	case rfc2866.AcctStatusType_Value_Start:
		eventType = "acct_start"
	case rfc2866.AcctStatusType_Value_Stop:
		eventType = "acct_stop"
	case rfc2866.AcctStatusType_Value_InterimUpdate:
		eventType = "acct_interim"
	default:
		eventType = "acct_unknown"
	}

	log.Printf("[radius] accounting: %s for user '%s' session=%s from %s",
		eventType, username, sessionID, clientAddr)

	// Record accounting event
	event := &accounting.AccountingEvent{
		Timestamp:  time.Now().UTC(),
		EventType:  eventType,
		Username:   username,
		SessionID:  sessionID,
		ClientIP:   clientAddr,
	}
	if err := h.accounting.Record(event); err != nil {
		log.Printf("[radius] accounting: failed to record event: %v", err)
	}

	// Always respond with Accounting-Response (per RFC 2866)
	resp := r.Response(radius.CodeAccountingResponse)
	w.Write(resp)
}

// sendReject sends an Access-Reject response with a Reply-Message.
func (h *Handler) sendReject(w radius.ResponseWriter, r *radius.Request, reason string) {
	resp := r.Response(radius.CodeAccessReject)
	rfc2865.ReplyMessage_SetString(resp, reason)
	w.Write(resp)
}

// recordEvent logs an accounting event for an authentication attempt.
func (h *Handler) recordEvent(eventType, userDID, username, clientIP, nasID, decision, reason string, start time.Time) {
	event := &accounting.AccountingEvent{
		Timestamp:     time.Now().UTC(),
		EventType:     eventType,
		UserDID:       userDID,
		Username:      username,
		ClientIP:      clientIP,
		NASIdentifier: nasID,
		Decision:      decision,
		Reason:        reason,
		LatencyUS:     time.Since(start).Microseconds(),
	}

	if err := h.accounting.Record(event); err != nil {
		log.Printf("[radius] failed to record event: %v", err)
	}
}
