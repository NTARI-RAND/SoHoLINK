//go:build darwin

// soholink-pfhelper is a minimal privilege helper that manages pf(4) anchors
// on behalf of the SoHoLINK daemon. It must run as root (via launchd) and
// listens on a Unix domain socket at /var/run/soholink-pfhelper.sock.
//
// Only three operations are accepted:
//
//	load_anchor  — load anchor rules from an approved conf file
//	flush_anchor — flush all rules in a named anchor
//	enable_pf    — enable the pf firewall (idempotent)
//
// Security constraints enforced by the helper:
//   - AnchorName must start with "soholink/"
//   - AnchorFile must be under /var/tmp/soholink/
//   - No shell interpretation; every pfctl invocation uses exec.Command directly
package main

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

const sockPath = "/var/run/soholink-pfhelper.sock"

// Request is the JSON payload sent by the SoHoLINK daemon over the socket.
type Request struct {
	// Op is the operation to perform: "load_anchor", "flush_anchor", "enable_pf".
	Op string `json:"op"`
	// AnchorName is the pf anchor path, e.g. "soholink/abc12345-8100".
	// Required for load_anchor and flush_anchor.
	AnchorName string `json:"anchor_name,omitempty"`
	// AnchorFile is the absolute path to the anchor conf file.
	// Required for load_anchor.
	AnchorFile string `json:"anchor_file,omitempty"`
}

// Response is the JSON reply sent back to the caller.
type Response struct {
	OK     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

func main() {
	log.SetPrefix("[pfhelper] ")

	// Remove any stale socket left by a previous crash.
	_ = os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		log.Fatalf("listen %s: %v", sockPath, err)
	}
	// 0660 — root:staff; SoHoLINK daemon user must be in the staff group
	// (all macOS admin users are). Adjust to match your deployment policy.
	if err := os.Chmod(sockPath, 0660); err != nil {
		log.Printf("chmod socket: %v (continuing)", err)
	}

	// Graceful shutdown on SIGTERM / SIGINT.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		ln.Close()
		os.Remove(sockPath)
		os.Exit(0)
	}()

	log.Printf("listening on %s", sockPath)
	for {
		conn, err := ln.Accept()
		if err != nil {
			// ln.Close() was called — normal shutdown path.
			return
		}
		go handle(conn)
	}
}

func handle(conn net.Conn) {
	defer conn.Close()

	var r Request
	if err := json.NewDecoder(conn).Decode(&r); err != nil {
		writeResp(conn, Response{Error: "decode request: " + err.Error()})
		return
	}

	var resp Response
	switch r.Op {
	case "load_anchor":
		resp = opLoadAnchor(r)
	case "flush_anchor":
		resp = opFlushAnchor(r)
	case "enable_pf":
		resp = opEnablePF()
	default:
		resp = Response{Error: "unknown op: " + r.Op}
	}
	writeResp(conn, resp)
}

// opLoadAnchor validates inputs then runs: pfctl -a <anchor> -f <file>
func opLoadAnchor(r Request) Response {
	if err := validateAnchorName(r.AnchorName); err != nil {
		return Response{Error: err.Error()}
	}
	if err := validateAnchorFile(r.AnchorFile); err != nil {
		return Response{Error: err.Error()}
	}
	out, err := exec.Command("pfctl", "-a", r.AnchorName, "-f", r.AnchorFile).CombinedOutput()
	if err != nil {
		return Response{OK: false, Output: string(out), Error: err.Error()}
	}
	return Response{OK: true, Output: string(out)}
}

// opFlushAnchor validates the anchor name then runs: pfctl -a <anchor> -F rules
func opFlushAnchor(r Request) Response {
	if err := validateAnchorName(r.AnchorName); err != nil {
		return Response{Error: err.Error()}
	}
	out, err := exec.Command("pfctl", "-a", r.AnchorName, "-F", "rules").CombinedOutput()
	if err != nil {
		return Response{OK: false, Output: string(out), Error: err.Error()}
	}
	return Response{OK: true, Output: string(out)}
}

// opEnablePF runs: pfctl -e (idempotent — already-enabled is not an error)
func opEnablePF() Response {
	out, err := exec.Command("pfctl", "-e").CombinedOutput()
	if err != nil {
		// "pf already enabled" exits with code 1 on some macOS versions — treat as OK.
		if strings.Contains(string(out), "already enabled") {
			return Response{OK: true}
		}
		return Response{OK: false, Output: string(out), Error: err.Error()}
	}
	return Response{OK: true}
}

func validateAnchorName(name string) error {
	if !strings.HasPrefix(name, "soholink/") {
		return &validationError{"anchor_name must start with soholink/"}
	}
	// Prevent path traversal or extra slashes in anchor name (must be "soholink/<leaf>").
	leaf := name[len("soholink/"):]
	if strings.Contains(leaf, "/") || strings.Contains(leaf, "..") || leaf == "" {
		return &validationError{"anchor_name must be exactly one level under soholink/"}
	}
	return nil
}

func validateAnchorFile(path string) error {
	const allowedDir = "/var/tmp/soholink/"
	if !strings.HasPrefix(path, allowedDir) {
		return &validationError{"anchor_file must be under " + allowedDir}
	}
	// Prevent path traversal in file path.
	if strings.Contains(path, "..") {
		return &validationError{"anchor_file path traversal not allowed"}
	}
	return nil
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return "pfhelper validation: " + e.msg }

func writeResp(conn net.Conn, r Response) {
	_ = json.NewEncoder(conn).Encode(r)
}
