package main

import (
	"syscall"
	"unsafe"
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

const mbIconError = 0x00000010

// showWindowsError displays a native Windows message box with the error text.
func showWindowsError(msg string) {
	title, _ := syscall.UTF16PtrFromString("SoHoLINK")
	text, _ := syscall.UTF16PtrFromString(msg)
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(title)),
		mbIconError,
	)
}
