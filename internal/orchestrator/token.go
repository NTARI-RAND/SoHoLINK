package orchestrator

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GenerateJobToken produces a URL-safe token in the form:
//
//	base64(jobID|nodeID|expireUnix) + "." + base64(hmac-sha256)
func GenerateJobToken(jobID, nodeID string, ttl time.Duration, secret []byte) (string, error) {
	if jobID == "" || nodeID == "" {
		return "", fmt.Errorf("generate job token: jobID and nodeID must not be empty")
	}
	expire := time.Now().Add(ttl).Unix()
	raw := fmt.Sprintf("%s|%s|%d", jobID, nodeID, expire)
	payload := base64.RawURLEncoding.EncodeToString([]byte(raw))

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payload + "." + sig, nil
}

// VerifyJobToken validates the HMAC signature and expiry, then returns the
// embedded jobID and nodeID.
func VerifyJobToken(token string, secret []byte) (jobID, nodeID string, err error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("verify job token: invalid format")
	}
	payload, sig := parts[0], parts[1]

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return "", "", fmt.Errorf("verify job token: invalid signature")
	}

	rawBytes, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", "", fmt.Errorf("verify job token: decode payload: %w", err)
	}

	claims := strings.Split(string(rawBytes), "|")
	if len(claims) != 3 {
		return "", "", fmt.Errorf("verify job token: malformed claims")
	}

	expireUnix, err := strconv.ParseInt(claims[2], 10, 64)
	if err != nil {
		return "", "", fmt.Errorf("verify job token: parse expiry: %w", err)
	}
	if time.Now().Unix() > expireUnix {
		return "", "", fmt.Errorf("verify job token: token expired")
	}

	return claims[0], claims[1], nil
}
