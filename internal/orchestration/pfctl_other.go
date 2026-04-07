//go:build !darwin

package orchestration

// pfctlLoadAnchor, pfctlFlushAnchor, and pfctlEnable are no-ops on non-darwin
// platforms. They are only invoked from the darwin case branches in firewall.go
// at runtime, but must be defined to satisfy the compiler on all platforms.

func pfctlLoadAnchor(_, _ string) error { return nil }
func pfctlFlushAnchor(_ string) error   { return nil }
func pfctlEnable()                      {}
