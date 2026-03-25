//go:build !windows

package main

import "fmt"

// showWindowsError is a no-op on non-Windows platforms.
func showWindowsError(msg string) {
	fmt.Println("SoHoLINK Error:", msg)
}
