//go:build windows

package capture

import (
	"fmt"
	"image"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")
	shcore = syscall.NewLazyDLL("shcore.dll")

	procGetDC                       = user32.NewProc("GetDC")
	procReleaseDC                   = user32.NewProc("ReleaseDC")
	procEnumDisplayMonitors         = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW             = user32.NewProc("GetMonitorInfoW")
	procEnumDisplaySettingsW        = user32.NewProc("EnumDisplaySettingsW")
	procSetProcessDPIAware          = user32.NewProc("SetProcessDPIAware")
	procSetProcessDpiAwarenessCtx   = user32.NewProc("SetProcessDpiAwarenessContext")
	procCreateCompatibleDC          = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection            = gdi32.NewProc("CreateDIBSection")
	procSelectObject                = gdi32.NewProc("SelectObject")
	procBitBlt                      = gdi32.NewProc("BitBlt")
	procDeleteObject                = gdi32.NewProc("DeleteObject")
	procDeleteDC                    = gdi32.NewProc("DeleteDC")
	procGetDpiForMonitor            = shcore.NewProc("GetDpiForMonitor")
	procSetProcessDpiAwarenessShc   = shcore.NewProc("SetProcessDpiAwareness")
	ensurePhysicalCoordinatesOnce   sync.Once
	monitorEnumerationMutex         sync.Mutex
	activeMonitorEnumerationContext *monitorEnumerationContext
	monitorEnumerationCallbackPtr   = syscall.NewCallback(monitorEnumerationCallback)
)

const (
	monitorInfoPrimary      = 1
	enumCurrentSettings     = ^uintptr(0)
	dpiAwarenessPerMonitor2 = ^uintptr(3)
	dibRGBColors            = 0
	sourceCopy              = 0x00CC0020
)

type windowsRect struct {
	Left, Top, Right, Bottom int32
}

type monitorInfoEx struct {
	Size    uint32
	Monitor windowsRect
	Work    windowsRect
	Flags   uint32
	Device  [32]uint16
}

type devModeW struct {
	DeviceName         [32]uint16
	SpecVersion        uint16
	DriverVersion      uint16
	Size               uint16
	DriverExtra        uint16
	Fields             uint32
	PositionX          int32
	PositionY          int32
	DisplayOrientation uint32
	DisplayFixedOutput uint32
	Color              int16
	Duplex             int16
	YResolution        int16
	TTOption           int16
	Collate            int16
	FormName           [32]uint16
	LogPixels          uint16
	BitsPerPel         uint32
	PelsWidth          uint32
	PelsHeight         uint32
	DisplayFlags       uint32
	DisplayFrequency   uint32
	ICMMethod          uint32
	ICMIntent          uint32
	MediaType          uint32
	DitherType         uint32
	Reserved1          uint32
	Reserved2          uint32
	PanningWidth       uint32
	PanningHeight      uint32
}

type monitorEnumerationContext struct {
	displays []Display
	err      error
}

func platformDisplays() ([]Display, error) {
	ensurePhysicalCoordinates()
	monitorEnumerationMutex.Lock()
	defer monitorEnumerationMutex.Unlock()

	context := &monitorEnumerationContext{}
	activeMonitorEnumerationContext = context
	defer func() {
		activeMonitorEnumerationContext = nil
	}()
	ok, _, callErr := procEnumDisplayMonitors.Call(
		0,
		0,
		monitorEnumerationCallbackPtr,
		0,
	)
	if ok == 0 {
		if context.err != nil {
			return nil, context.err
		}
		return nil, fmt.Errorf("capture: EnumDisplayMonitors: %w", callErr)
	}
	if context.err != nil {
		return nil, context.err
	}
	if len(context.displays) == 0 {
		return nil, ErrNoDisplays
	}
	return context.displays, nil
}

func monitorEnumerationCallback(monitor, _, _, _ uintptr) uintptr {
	context := activeMonitorEnumerationContext
	if context == nil {
		return 0
	}
	var info monitorInfoEx
	info.Size = uint32(unsafe.Sizeof(info))
	if ok, _, callErr := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
		context.err = fmt.Errorf("capture: GetMonitorInfoW: %w", callErr)
		return 0
	}
	bounds := image.Rect(
		int(info.Monitor.Left),
		int(info.Monitor.Top),
		int(info.Monitor.Right),
		int(info.Monitor.Bottom),
	)
	if physical := displaySettingsBounds(&info); !physical.Empty() {
		bounds = physical
	}
	context.displays = append(context.displays, Display{
		Index:  len(context.displays),
		Name:   syscall.UTF16ToString(info.Device[:]),
		Bounds: bounds,
		Work: image.Rect(
			int(info.Work.Left),
			int(info.Work.Top),
			int(info.Work.Right),
			int(info.Work.Bottom),
		),
		Primary: info.Flags&monitorInfoPrimary != 0,
		DPI:     monitorDPI(monitor),
	})
	return 1
}

func displaySettingsBounds(info *monitorInfoEx) image.Rectangle {
	var mode devModeW
	mode.Size = uint16(unsafe.Sizeof(mode))
	ok, _, _ := procEnumDisplaySettingsW.Call(
		uintptr(unsafe.Pointer(&info.Device[0])),
		enumCurrentSettings,
		uintptr(unsafe.Pointer(&mode)),
	)
	if ok == 0 || mode.PelsWidth == 0 || mode.PelsHeight == 0 {
		return image.Rectangle{}
	}
	return image.Rect(
		int(mode.PositionX),
		int(mode.PositionY),
		int(mode.PositionX)+int(mode.PelsWidth),
		int(mode.PositionY)+int(mode.PelsHeight),
	)
}

func monitorDPI(monitor uintptr) uint32 {
	if procGetDpiForMonitor.Find() != nil {
		return 0
	}
	var x, y uint32
	result, _, _ := procGetDpiForMonitor.Call(
		monitor,
		0, // MDT_EFFECTIVE_DPI
		uintptr(unsafe.Pointer(&x)),
		uintptr(unsafe.Pointer(&y)),
	)
	if int32(result) < 0 {
		return 0
	}
	if x != 0 {
		return x
	}
	return y
}

func platformCapture(bounds image.Rectangle) (*image.RGBA, error) {
	ensurePhysicalCoordinates()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, ErrInvalidBounds
	}
	if width > 1<<30 || height > 1<<30 {
		return nil, ErrInvalidBounds
	}

	screen, _, callErr := procGetDC.Call(0)
	if screen == 0 {
		return nil, fmt.Errorf("capture: GetDC: %w", callErr)
	}
	defer procReleaseDC.Call(0, screen)

	memory, _, callErr := procCreateCompatibleDC.Call(screen)
	if memory == 0 {
		return nil, fmt.Errorf("capture: CreateCompatibleDC: %w", callErr)
	}
	defer procDeleteDC.Call(memory)

	var info bitmapInfo
	info.Header.Size = uint32(unsafe.Sizeof(info.Header))
	info.Header.Width = int32(width)
	info.Header.Height = -int32(height)
	info.Header.Planes = 1
	info.Header.BitCount = 32
	info.Header.Compression = 0 // BI_RGB
	info.Header.SizeImage = uint32(width * height * 4)
	var bits unsafe.Pointer
	bitmap, _, callErr := procCreateDIBSection.Call(
		screen,
		uintptr(unsafe.Pointer(&info)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if bitmap == 0 || bits == nil {
		return nil, fmt.Errorf("capture: CreateDIBSection: %w", callErr)
	}
	defer procDeleteObject.Call(bitmap)

	previous, _, callErr := procSelectObject.Call(memory, bitmap)
	if previous == 0 {
		return nil, fmt.Errorf("capture: SelectObject: %w", callErr)
	}
	defer procSelectObject.Call(memory, previous)

	copied, _, callErr := procBitBlt.Call(
		memory,
		0,
		0,
		uintptr(width),
		uintptr(height),
		screen,
		uintptr(int32(bounds.Min.X)),
		uintptr(int32(bounds.Min.Y)),
		sourceCopy,
	)
	if copied == 0 {
		return nil, fmt.Errorf("capture: BitBlt: %w", callErr)
	}

	pixels := unsafe.Slice((*byte)(bits), width*height*4)
	output := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		source := pixels[y*width*4:]
		target := output.Pix[y*output.Stride:]
		for x := 0; x < width; x++ {
			sourceOffset := x * 4
			targetOffset := x * 4
			target[targetOffset+0] = source[sourceOffset+2]
			target[targetOffset+1] = source[sourceOffset+1]
			target[targetOffset+2] = source[sourceOffset+0]
			target[targetOffset+3] = 255
		}
	}
	return output, nil
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ColorsUsed    uint32
	Important     uint32
}

type rgbQuad struct {
	Blue, Green, Red, Reserved byte
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]rgbQuad
}

// ensurePhysicalCoordinates asks Windows for a per-monitor DPI-aware process
// before any window is created. Failure is normally because Viva or the host
// process already selected the same awareness; both are safe to ignore.
func ensurePhysicalCoordinates() {
	ensurePhysicalCoordinatesOnce.Do(func() {
		if procSetProcessDpiAwarenessCtx.Find() == nil {
			if ok, _, _ := procSetProcessDpiAwarenessCtx.Call(dpiAwarenessPerMonitor2); ok != 0 {
				return
			}
		}
		if procSetProcessDpiAwarenessShc.Find() == nil {
			if result, _, _ := procSetProcessDpiAwarenessShc.Call(2); int32(result) == 0 {
				return
			}
		}
		if procSetProcessDPIAware.Find() == nil {
			procSetProcessDPIAware.Call()
		}
	})
}
