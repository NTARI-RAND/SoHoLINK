//go:build !windows

package main

import "errors"

// errNotWindows is returned by the Windows-service flag handlers on any
// non-Windows build. The flags (--install, --uninstall, --service) are
// meaningful only on Windows, where the SCM provides the service model
// these helpers drive. On Linux and macOS, service management is
// handled by systemd or launchd wrappers outside this binary.
var errNotWindows = errors.New("Windows service management is only available on Windows builds")

func installService() error { return errNotWindows }
func removeService() error  { return errNotWindows }
func runAsService() error   { return errNotWindows }
