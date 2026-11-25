package main

import "golang.org/x/sys/windows"

var allowSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")

func allowActivationForeground(pid int) {
	// A launcher started by the foreground app can pass its activation right
	// to the existing GoIM process before that process shows its window.
	_, _, _ = allowSetForegroundWindow.Call(uintptr(pid))
}
