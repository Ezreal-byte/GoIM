//go:build windows && !vivahead

package desktop

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	attentionUser32  = windows.NewLazySystemDLL("user32.dll")
	foregroundWindow = attentionUser32.NewProc("GetForegroundWindow")
	windowProcess    = attentionUser32.NewProc("GetWindowThreadProcessId")
	windowTitle      = attentionUser32.NewProc("GetWindowTextW")
)

// A visible GoIM window may still be behind another app or its own preview.
// The main window has the fixed native title "GoIM".
func mainWindowForeground() bool {
	handle, _, _ := foregroundWindow.Call()
	if handle == 0 {
		return false
	}
	var pid uint32
	windowProcess.Call(handle, uintptr(unsafe.Pointer(&pid)))
	if int(pid) != os.Getpid() {
		return false
	}
	var title [12]uint16
	count, _, _ := windowTitle.Call(handle, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)))
	return count == 4 && windows.UTF16ToString(title[:]) == "GoIM"
}
