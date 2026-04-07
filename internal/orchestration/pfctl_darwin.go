//go:build darwin

package orchestration

// pfctlRun executes a pfctl command. It first attempts to delegate to the
// soholink-pfhelper privilege helper running as root (via Unix socket). If the
// helper socket is not available (e.g. the daemon is itself running as root, or
// the helper is not installed), it falls back to calling pfctl directly.
//
// The structured operations mirror the helper's Request type:
//
//	pfctlLoadAnchor(anchorName, anchorFile) → pfctl -a <anchor> -f <file>
//	pfctlFlushAnchor(anchorName)            → pfctl -a <anchor> -F rules
//	pfctlEnable()                           → pfctl -e

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os/exec"
	"time"
)

const pfHelperSock = "/var/run/soholink-pfhelper.sock"

// pfHelperReq mirrors cmd/soholink-pfhelper Request.
type pfHelperReq struct {
	Op         string `json:"op"`
	AnchorName string `json:"anchor_name,omitempty"`
	AnchorFile string `json:"anchor_file,omitempty"`
}

// pfHelperResp mirrors cmd/soholink-pfhelper Response.
type pfHelperResp struct {
	OK     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// callHelper dials the privilege helper, sends req, and returns the response.
// Returns an error if the socket is unavailable or the call fails.
func callHelper(req pfHelperReq) (pfHelperResp, error) {
	conn, err := net.DialTimeout("unix", pfHelperSock, 2*time.Second)
	if err != nil {
		return pfHelperResp{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return pfHelperResp{}, fmt.Errorf("pfhelper write: %w", err)
	}

	var resp pfHelperResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return pfHelperResp{}, fmt.Errorf("pfhelper read: %w", err)
	}
	return resp, nil
}

// pfctlLoadAnchor loads a pf anchor from a conf file.
// Uses the privilege helper if available, falls back to direct exec.
func pfctlLoadAnchor(anchorName, anchorFile string) error {
	resp, err := callHelper(pfHelperReq{Op: "load_anchor", AnchorName: anchorName, AnchorFile: anchorFile})
	if err == nil {
		if !resp.OK {
			return fmt.Errorf("pfhelper load_anchor: %s", resp.Error)
		}
		return nil
	}
	// Helper unavailable — fall back to direct exec (requires root).
	log.Printf("[firewall] pfhelper unavailable (%v), calling pfctl directly", err)
	out, execErr := exec.Command("pfctl", "-a", anchorName, "-f", anchorFile).CombinedOutput()
	if execErr != nil {
		log.Printf("[firewall] pfctl load anchor failed (need root?): %v — %s", execErr, string(out))
	}
	return nil // non-fatal: log and continue
}

// pfctlFlushAnchor flushes all rules from a pf anchor.
// Uses the privilege helper if available, falls back to direct exec.
func pfctlFlushAnchor(anchorName string) error {
	resp, err := callHelper(pfHelperReq{Op: "flush_anchor", AnchorName: anchorName})
	if err == nil {
		if !resp.OK {
			return fmt.Errorf("pfhelper flush_anchor: %s", resp.Error)
		}
		return nil
	}
	log.Printf("[firewall] pfhelper unavailable (%v), calling pfctl directly", err)
	out, execErr := exec.Command("pfctl", "-a", anchorName, "-F", "rules").CombinedOutput()
	if execErr != nil {
		log.Printf("[firewall] pfctl flush anchor failed: %v — %s", execErr, string(out))
	}
	return nil
}

// pfctlEnable enables pf (idempotent).
// Uses the privilege helper if available, falls back to direct exec.
func pfctlEnable() {
	resp, err := callHelper(pfHelperReq{Op: "enable_pf"})
	if err == nil {
		if !resp.OK {
			log.Printf("[firewall] pfhelper enable_pf: %s", resp.Error)
		}
		return
	}
	log.Printf("[firewall] pfhelper unavailable (%v), calling pfctl directly", err)
	_ = exec.Command("pfctl", "-e").Run()
}
