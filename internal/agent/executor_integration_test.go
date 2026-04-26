//go:build docker_integration

package agent

import "testing"

// Integration tests for the hardened executor. Build-tagged so they only
// run when explicitly requested:
//
//	go test -tags=docker_integration ./internal/agent/...
//
// These tests require a running Docker daemon and a SoHoLINK worker image
// with a non-root USER directive declared in the test allowlist. Wired up
// in B7 once allowlist signing publishes the first real worker image.
//
// Coverage planned for B7 wire-up:
//   - End-to-end: hardened container starts, workload executes, exits cleanly
//   - EgressNone: container cannot reach external hosts
//   - EgressOutbound: container can reach external hosts
//   - Per-job network created and torn down with the job
//   - tmpfs scratch writable up to 256 MiB; ENOSPC detected past that limit
func TestIntegration_NotYetWired(t *testing.T) {
	t.Skip("integration tests need SoHoLINK worker image — wired in B7")
}
