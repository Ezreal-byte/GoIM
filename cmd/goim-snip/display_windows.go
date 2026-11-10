//go:build windows

package main

import (
	"image"
	"sync"
	"syscall"
	"unsafe"

	"github.com/vivaui/viva/ui"
	"goim/internal/capture"
	"golang.org/x/sys/windows"
)

var (
	snipUser32             = windows.NewLazySystemDLL("user32.dll")
	snipDWM                = windows.NewLazySystemDLL("dwmapi.dll")
	snipGetCursor          = snipUser32.NewProc("GetCursorPos")
	snipInitialDPI         = snipUser32.NewProc("GetDpiForSystem")
	snipEnumWindows        = snipUser32.NewProc("EnumWindows")
	snipIsVisible          = snipUser32.NewProc("IsWindowVisible")
	snipIsIconic           = snipUser32.NewProc("IsIconic")
	snipGetShellWindow     = snipUser32.NewProc("GetShellWindow")
	snipGetDesktopWindow   = snipUser32.NewProc("GetDesktopWindow")
	snipGetWindowRect      = snipUser32.NewProc("GetWindowRect")
	snipGetWindowAttribute = snipDWM.NewProc("DwmGetWindowAttribute")
	snipWindowCallback     = syscall.NewCallback(appendSnipWindow)
	snipEnumMu             sync.Mutex
	snipEnumState          *windowEnumeration
)

type windowEnumeration struct {
	shell, desktop uintptr
	dwm            bool
	windows        []image.Rectangle
}

const (
	snipExtendedFrameBounds = 9
	snipCloaked             = 14
)

type snipRect struct{ left, top, right, bottom int32 }

func desktopCursor() (image.Point, bool) {
	var p struct{ x, y int32 }
	ok, _, _ := snipGetCursor.Call(uintptr(unsafe.Pointer(&p)))
	return image.Pt(int(p.x), int(p.y)), ok != 0
}

// Snapshot window geometry before the capture overlay exists. DWM bounds omit
// invisible resize borders and use the same physical pixels as the capture.
func desktopWindows() []image.Rectangle {
	snipEnumMu.Lock()
	defer snipEnumMu.Unlock()
	shell, _, _ := snipGetShellWindow.Call()
	desktop, _, _ := snipGetDesktopWindow.Call()
	state := windowEnumeration{shell: shell, desktop: desktop, dwm: snipGetWindowAttribute.Find() == nil}
	snipEnumState = &state
	defer func() { snipEnumState = nil }()
	snipEnumWindows.Call(snipWindowCallback, 0)
	return state.windows
}

func appendSnipWindow(hwnd, _ uintptr) uintptr {
	state := snipEnumState
	if state == nil || len(state.windows) >= 512 {
		return 0
	}
	if hwnd == state.shell || hwnd == state.desktop {
		return 1
	}
	if visible, _, _ := snipIsVisible.Call(hwnd); visible == 0 {
		return 1
	}
	if iconic, _, _ := snipIsIconic.Call(hwnd); iconic != 0 {
		return 1
	}
	var r snipRect
	extended := false
	if state.dwm {
		var cloaked uint32
		if result, _, _ := snipGetWindowAttribute.Call(hwnd, snipCloaked, uintptr(unsafe.Pointer(&cloaked)), 4); int32(result) >= 0 && cloaked != 0 {
			return 1
		}
		result, _, _ := snipGetWindowAttribute.Call(hwnd, snipExtendedFrameBounds, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r))
		extended = int32(result) >= 0
	}
	if !extended {
		if ok, _, _ := snipGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
			return 1
		}
	}
	if r.right-r.left >= 32 && r.bottom-r.top >= 24 {
		state.windows = append(state.windows, image.Rect(int(r.left), int(r.top), int(r.right), int(r.bottom)))
	}
	return 1
}

// Viva creates the frameless HWND on the primary display before applying the
// requested logical origin. Convert the target's physical anchor with that
// initial DPI, then let fullscreen acquire the actual monitor and its DPI.
func overlayPlacement(display capture.Display) (width, height int, origin ui.Point) {
	initial := float64(1)
	if snipInitialDPI.Find() == nil {
		if dpi, _, _ := snipInitialDPI.Call(); dpi != 0 {
			initial = float64(dpi) / 96
		}
	}
	work := display.Work.Intersect(display.Bounds)
	if work.Empty() {
		work = display.Bounds
	}
	scale := max(initial, display.Scale())
	return max(1, int(float64(work.Dx())/scale)), max(1, int(float64(work.Dy())/scale)),
		ui.Point{X: float32(float64(work.Min.X) / initial), Y: float32(float64(work.Min.Y) / initial)}
}
