//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	clipboardGMEM         moveableMemory = 0x0002
	clipboardGMEMZeroInit moveableMemory = 0x0040
)

type moveableMemory uintptr

var (
	clipboardUser32 = windows.NewLazySystemDLL("user32.dll")
	clipboardKernel = windows.NewLazySystemDLL("kernel32.dll")

	openClipboard           = clipboardUser32.NewProc("OpenClipboard")
	closeClipboard          = clipboardUser32.NewProc("CloseClipboard")
	emptyClipboard          = clipboardUser32.NewProc("EmptyClipboard")
	setClipboardData        = clipboardUser32.NewProc("SetClipboardData")
	registerClipboardFormat = clipboardUser32.NewProc("RegisterClipboardFormatW")
	globalAlloc             = clipboardKernel.NewProc("GlobalAlloc")
	globalLock              = clipboardKernel.NewProc("GlobalLock")
	globalUnlock            = clipboardKernel.NewProc("GlobalUnlock")
	globalFree              = clipboardKernel.NewProc("GlobalFree")
	rtlMoveMemory           = clipboardKernel.NewProc("RtlMoveMemory")
)

func setClipboardPNG(_ string, data []byte) error {
	if err := validateClipboardImage(data); err != nil {
		return err
	}
	formatName, err := syscall.UTF16PtrFromString("PNG")
	if err != nil {
		return err
	}
	format, _, callErr := registerClipboardFormat.Call(uintptr(unsafe.Pointer(formatName)))
	if format == 0 {
		return windowsCallError("register PNG clipboard format", callErr)
	}
	handle, _, callErr := globalAlloc.Call(uintptr(clipboardGMEM|clipboardGMEMZeroInit), uintptr(len(data)))
	if handle == 0 {
		return windowsCallError("allocate clipboard image", callErr)
	}
	transferred := false
	defer func() {
		if !transferred {
			globalFree.Call(handle)
		}
	}()
	memory, _, callErr := globalLock.Call(handle)
	if memory == 0 {
		return windowsCallError("lock clipboard image", callErr)
	}
	if len(data) > 0 {
		rtlMoveMemory.Call(memory, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	}
	globalUnlock.Call(handle)

	if opened, _, callErr := openClipboard.Call(0); opened == 0 {
		return windowsCallError("open clipboard", callErr)
	}
	defer closeClipboard.Call()
	if emptied, _, callErr := emptyClipboard.Call(); emptied == 0 {
		return windowsCallError("empty clipboard", callErr)
	}
	if stored, _, callErr := setClipboardData.Call(format, handle); stored == 0 {
		return windowsCallError("write clipboard image", callErr)
	}
	transferred = true
	return nil
}

func windowsCallError(operation string, callErr error) error {
	if callErr == nil || errors.Is(callErr, syscall.Errno(0)) {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s: %w", operation, callErr)
}
