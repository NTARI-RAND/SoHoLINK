package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/did"
)

type challengeResp struct {
	Nonce     string `json:"nonce"`
	ExpiresAt string `json:"expires_at"`
}

type connectReq struct {
	Nonce      string `json:"nonce"`
	PublicKey  string `json:"public_key"`
	Signature  string `json:"signature"`
	DeviceName string `json:"device_name"`
}

type connectResp struct {
	Token string `json:"token"`
	DID   string `json:"did"`
}

func main() {
	baseURL := flag.String("url", "http://localhost:8080", "Base URL of SoHoLINK API")
	keyPath := flag.String("key", filepath.Join(os.Getenv("USERPROFILE"), ".soholink", "identity", "private.pem"), "Path to private key")
	endpoint := flag.String("endpoint", "/api/reputation/ledger", "Endpoint to query")
	flag.Parse()

	fmt.Println("[*] SoHoLINK API Test")
	fmt.Printf("[*] Base URL: %s\n", *baseURL)
	fmt.Printf("[*] Key: %s\n", *keyPath)

	// Load private key
	privKey, err := did.LoadPrivateKey(*keyPath)
	if err != nil {
		log.Fatalf("[!] Failed to load private key: %v", err)
	}
	fmt.Println("[*] Private key loaded")

	// Extract public key
	pubKey := privKey.Public().(ed25519.PublicKey)
	pubKeyB64 := base64.StdEncoding.EncodeToString(pubKey)
	fmt.Printf("[*] Public key: %s\n", truncate(pubKeyB64, 50))

	// Step 1: Get nonce
	fmt.Println("\n[Step 1] Getting nonce...")
	challengeURL := *baseURL + "/api/auth/challenge"
	resp, err := http.Get(challengeURL)
	if err != nil {
		log.Fatalf("[!] Failed to get nonce: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var challenge challengeResp
	if err := json.Unmarshal(body, &challenge); err != nil {
		log.Fatalf("[!] Failed to parse challenge response: %v", err)
	}
	fmt.Printf("[*] Nonce: %s\n", challenge.Nonce)

	// Step 2: Sign nonce
	fmt.Println("\n[Step 2] Signing nonce...")
	signature := ed25519.Sign(privKey, []byte(challenge.Nonce))
	signatureB64 := base64.StdEncoding.EncodeToString(signature)
	fmt.Printf("[*] Signature: %s\n", truncate(signatureB64, 50))

	// Step 3: Authenticate
	fmt.Println("\n[Step 3] Authenticating...")
	connectURL := *baseURL + "/api/auth/connect"
	connectReqBody := connectReq{
		Nonce:      challenge.Nonce,
		PublicKey:  pubKeyB64,
		Signature:  signatureB64,
		DeviceName: "test-device",
	}

	fmt.Printf("[DEBUG] Request body: %+v\n", connectReqBody)

	reqJSON, _ := json.Marshal(connectReqBody)
	fmt.Printf("[DEBUG] Request JSON: %s\n", string(reqJSON))
	fmt.Println("\n[*] NOTE: If auth fails with 'unauthorized':")
	fmt.Println("    The public key stored in the server database doesn't match this key.")
	fmt.Println("    The server has its own node identity separate from the config files.")
	fmt.Println("    Try clearing AppData\\Local\\SoHoLINK\\data and restarting the server.")

	httpResp, err := http.Post(connectURL, "application/json", bytes.NewBuffer(reqJSON))
	if err != nil {
		log.Fatalf("[!] Failed to authenticate: %v", err)
	}

	body, _ = io.ReadAll(httpResp.Body)
	httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		fmt.Printf("[!] Authentication failed: %d\n", httpResp.StatusCode)
		fmt.Printf("[!] Response: %s\n", string(body))
		fmt.Println("\n[*] DEBUGGING TIPS:")
		fmt.Println("    1. The app might have generated a different keypair than what's in the config")
		fmt.Println("    2. Check AppData\\Local\\SoHoLINK\\data for node identity keys")
		fmt.Println("    3. The node public key in the database might not match this file")
		os.Exit(1)
	}

	var connectResp connectResp
	if err := json.Unmarshal(body, &connectResp); err != nil {
		log.Fatalf("[!] Failed to parse connect response: %v", err)
	}
	fmt.Printf("[*] Authentication successful!\n")
	fmt.Printf("[*] Token: %s\n", truncate(connectResp.Token, 50))
	fmt.Printf("[*] DID: %s\n", connectResp.DID)

	// Step 4: Query endpoint
	fmt.Printf("\n[Step 4] Querying %s...\n", *endpoint)
	client := &http.Client{}
	req, err := http.NewRequest("GET", *baseURL+*endpoint, nil)
	if err != nil {
		log.Fatalf("[!] Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+connectResp.Token)

	httpResp, err = client.Do(req)
	if err != nil {
		log.Fatalf("[!] Failed to query endpoint: %v", err)
	}
	defer httpResp.Body.Close()

	body, _ = io.ReadAll(httpResp.Body)
	fmt.Printf("[*] Status: %d\n", httpResp.StatusCode)

	if httpResp.StatusCode == 200 {
		var result interface{}
		if err := json.Unmarshal(body, &result); err == nil {
			pretty, _ := json.MarshalIndent(result, "", "  ")
			fmt.Printf("[*] Response:\n%s\n", string(pretty[:min(500, len(pretty))]))
		} else {
			fmt.Printf("[*] Response: %s\n", string(body[:min(200, len(body))]))
		}
	} else {
		fmt.Printf("[!] Error: %s\n", string(body))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
