//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

var attachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// attachParentConsole reconnects --check output to the parent console when the binary is built as a GUI subsystem app.
func attachParentConsole() {
	const attachParent = ^uintptr(0)
	if r, _, _ := attachConsole.Call(attachParent); r == 0 {
		return
	}
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != 0 {
		os.Stdout = os.NewFile(uintptr(h), "stdout")
	}
	if h, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE); err == nil && h != 0 {
		os.Stderr = os.NewFile(uintptr(h), "stderr")
	}
}
