//go:build windows && !vivahead

package desktop

import (
	"math"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	attentionUser32  = windows.NewLazySystemDLL("user32.dll")
	foregroundWindow = attentionUser32.NewProc("GetForegroundWindow")
	windowProcess    = attentionUser32.NewProc("GetWindowThreadProcessId")
	windowClientRect = attentionUser32.NewProc("GetClientRect")
	clientToScreen   = attentionUser32.NewProc("ClientToScreen")
	windowDPI        = attentionUser32.NewProc("GetDpiForWindow")
)

type nativePoint struct {
	X int32
	Y int32
}

// A visible GoIM window may still be behind another app or its own preview.
func mainWindowForeground(window windowGeometry) bool {
	return windowForeground(window)
}

func windowForeground(window windowGeometry) bool {
	if window == nil {
		return false
	}
	handle, _, _ := foregroundWindow.Call()
	if handle == 0 {
		return false
	}
	var pid uint32
	windowProcess.Call(handle, uintptr(unsafe.Pointer(&pid)))
	if int(pid) != os.Getpid() {
		return false
	}
	frame, ok := window.Frame()
	if !ok {
		return false
	}
	var client windows.Rect
	status, _, _ := windowClientRect.Call(handle, uintptr(unsafe.Pointer(&client)))
	if status == 0 {
		return false
	}
	origin := nativePoint{}
	status, _, _ = clientToScreen.Call(handle, uintptr(unsafe.Pointer(&origin)))
	if status == 0 {
		return false
	}
	scale := float32(1)
	if windowDPI.Find() == nil {
		if dpi, _, _ := windowDPI.Call(handle); dpi > 0 {
			scale = float32(dpi) / 96
		}
	}
	return math.Abs(float64(frame.X-float32(origin.X)/scale)) <= 2 &&
		math.Abs(float64(frame.Y-float32(origin.Y)/scale)) <= 2 &&
		math.Abs(float64(frame.Width-float32(client.Right-client.Left)/scale)) <= 2 &&
		math.Abs(float64(frame.Height-float32(client.Bottom-client.Top)/scale)) <= 2
}
