//go:build windows

package video

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	coinitMultithreaded            = 0x0
	mfVersion                      = 0x00020070 // Vista+ MF_VERSION
	mfStartupLite                  = 0x1
	mfSourceReaderFirstVideoStream = 0xFFFFFFFC
	mfSourceReaderFlagEndOfStream  = 0x00000002
	mfSourceReaderFlagError        = 0x00000001
	mfSourceReaderFlagStreamTick   = 0x00000100
	rpcEChangedMode                = 0x80010106
	defaultTargetFrameInterval     = 50 * time.Millisecond // ~20 fps max
	defaultPreferredWidth          = 640
	defaultPreferredHeight         = 360
)

var (
	ole32       = windows.NewLazySystemDLL("ole32.dll")
	mfplat      = windows.NewLazySystemDLL("mfplat.dll")
	mfDLL       = windows.NewLazySystemDLL("mf.dll")
	mfreadwrite = windows.NewLazySystemDLL("mfreadwrite.dll")

	coInitializeEx                      = ole32.NewProc("CoInitializeEx")
	coUninitialize                      = ole32.NewProc("CoUninitialize")
	coTaskMemFree                       = ole32.NewProc("CoTaskMemFree")
	mfStartup                           = mfplat.NewProc("MFStartup")
	mfShutdown                          = mfplat.NewProc("MFShutdown")
	mfCreateAttributes                  = mfplat.NewProc("MFCreateAttributes")
	mfCreateMediaType                   = mfplat.NewProc("MFCreateMediaType")
	mfEnumDeviceSources                 = mfDLL.NewProc("MFEnumDeviceSources")
	mfCreateSourceReaderFromMediaSource = mfreadwrite.NewProc("MFCreateSourceReaderFromMediaSource")

	// Media Foundation GUIDs
	guidMFDevSourceType          = windows.GUID{Data1: 0xC60AC5FE, Data2: 0x252A, Data3: 0x478F, Data4: [8]byte{0xA0, 0xEF, 0xBC, 0x8F, 0xA5, 0xF7, 0xCA, 0xD3}}
	guidMFDevSourceTypeVidCap    = windows.GUID{Data1: 0x8AC3587A, Data2: 0x4AE7, Data3: 0x42D8, Data4: [8]byte{0x99, 0xE0, 0x0A, 0x60, 0x13, 0xEE, 0xF9, 0x0F}}
	guidMFDevSourceFriendlyName  = windows.GUID{Data1: 0x60D0E559, Data2: 0x52F8, Data3: 0x4FA2, Data4: [8]byte{0xBB, 0xCE, 0xAC, 0xDB, 0x34, 0xA8, 0xEC, 0x01}}
	guidMFDevSourceVidCapSymlink = windows.GUID{Data1: 0x58F0AAD8, Data2: 0x22BF, Data3: 0x4F8A, Data4: [8]byte{0xBB, 0x3D, 0xD2, 0xC4, 0x97, 0x8C, 0x6E, 0x2F}}
	guidMFSourceReaderVideoProc  = windows.GUID{Data1: 0xFB394F3D, Data2: 0xCCF1, Data3: 0x42EE, Data4: [8]byte{0xBB, 0xB3, 0xF9, 0xB8, 0x45, 0xD5, 0x68, 0x1D}}
	guidMFMediaSourceIID         = windows.GUID{Data1: 0x279A808D, Data2: 0xAEC7, Data3: 0x40C8, Data4: [8]byte{0x9C, 0x6B, 0xA6, 0xB4, 0x92, 0xC7, 0x8A, 0x66}}
	guidMFMTMajorType            = windows.GUID{Data1: 0x48EBA18E, Data2: 0xF8C9, Data3: 0x4687, Data4: [8]byte{0xBF, 0x11, 0x0A, 0x74, 0xC9, 0xF9, 0x6A, 0x8F}}
	guidMFMTSubtype              = windows.GUID{Data1: 0xF7E34C9A, Data2: 0x42E8, Data3: 0x4714, Data4: [8]byte{0xB7, 0x4B, 0xCB, 0x29, 0xD7, 0x2C, 0x35, 0xE5}}
	guidMFMTFrameSize            = windows.GUID{Data1: 0x1652C33D, Data2: 0xD6B2, Data3: 0x4012, Data4: [8]byte{0xB8, 0x34, 0x72, 0x03, 0x08, 0x49, 0xA3, 0x7D}}
	guidMFMTDefaultStride        = windows.GUID{Data1: 0x644B4E48, Data2: 0x1E02, Data3: 0x4516, Data4: [8]byte{0xB0, 0xEB, 0xC0, 0x1C, 0xA9, 0xD4, 0x9A, 0xC6}}
	guidMFMediaTypeVideo         = windows.GUID{Data1: 0x73646976, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	guidMFVideoFormatRGB32       = windows.GUID{Data1: 0x00000016, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	guidMFVideoFormatNV12        = windows.GUID{Data1: 0x3231564E, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	guidMFVideoFormatYUY2        = windows.GUID{Data1: 0x32595559, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
)

type comObject struct {
	vtbl *uintptr
}

func (o *comObject) method(index uintptr) uintptr {
	return *(*uintptr)(unsafe.Pointer(uintptr(unsafe.Pointer(o.vtbl)) + index*unsafe.Sizeof(uintptr(0))))
}

func (o *comObject) release() {
	if o == nil {
		return
	}
	_, _, _ = syscall.SyscallN(o.method(2), uintptr(unsafe.Pointer(o)))
}

// IMFAttributes vtable offsets (IUnknown: 0..2, IMFAttributes: 3..32)
// GetUINT32 = 7, GetUINT64 = 8, GetGUID = 10, GetAllocatedString = 14,
// SetUINT32 = 21, SetUINT64 = 22, SetGUID = 24
func mfAttrSetGUID(attr *comObject, key, value *windows.GUID) error {
	hr, _, _ := syscall.SyscallN(attr.method(24), uintptr(unsafe.Pointer(attr)), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(value)))
	return checkHR("SetGUID", hr)
}

func mfAttrSetUINT32(attr *comObject, key *windows.GUID, value uint32) error {
	hr, _, _ := syscall.SyscallN(attr.method(21), uintptr(unsafe.Pointer(attr)), uintptr(unsafe.Pointer(key)), uintptr(value))
	return checkHR("SetUINT32", hr)
}

func mfAttrSetUINT64(attr *comObject, key *windows.GUID, value uint64) error {
	hr, _, _ := syscall.SyscallN(attr.method(22), uintptr(unsafe.Pointer(attr)), uintptr(unsafe.Pointer(key)), uintptr(value))
	return checkHR("SetUINT64", hr)
}

func mfAttrGetUINT32(attr *comObject, key *windows.GUID) (uint32, error) {
	var val uint32
	hr, _, _ := syscall.SyscallN(attr.method(7), uintptr(unsafe.Pointer(attr)), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&val)))
	return val, checkHR("GetUINT32", hr)
}

func mfAttrGetUINT64(attr *comObject, key *windows.GUID) (uint64, error) {
	var val uint64
	hr, _, _ := syscall.SyscallN(attr.method(8), uintptr(unsafe.Pointer(attr)), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&val)))
	return val, checkHR("GetUINT64", hr)
}

func mfAttrGetGUID(attr *comObject, key *windows.GUID) (windows.GUID, error) {
	var val windows.GUID
	hr, _, _ := syscall.SyscallN(attr.method(10), uintptr(unsafe.Pointer(attr)), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&val)))
	return val, checkHR("GetGUID", hr)
}

func mfAttrGetString(attr *comObject, key *windows.GUID) (string, error) {
	var ptr *uint16
	var length uint32
	hr, _, _ := syscall.SyscallN(attr.method(14), uintptr(unsafe.Pointer(attr)), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&length)))
	if err := checkHR("GetAllocatedString", hr); err != nil {
		return "", err
	}
	if ptr == nil {
		return "", nil
	}
	defer coTaskMemFree.Call(uintptr(unsafe.Pointer(ptr)))
	return windows.UTF16PtrToString(ptr), nil
}

// IMFActivate: inherits IMFAttributes (0..32), ActivateObject = 33
func mfActivateObject(activate *comObject, iid *windows.GUID) (*comObject, error) {
	var obj *comObject
	hr, _, _ := syscall.SyscallN(activate.method(33), uintptr(unsafe.Pointer(activate)), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&obj)))
	return obj, checkHR("ActivateObject", hr)
}

// IMFSourceReader vtable offsets:
// 0..2 IUnknown
// 3 GetStreamSelection, 4 SetStreamSelection, 5 GetNativeMediaType,
// 6 GetCurrentMediaType, 7 SetCurrentMediaType, 8 SetCurrentPosition, 9 ReadSample
func mfReaderSetCurrentMediaType(reader *comObject, streamIndex uint32, mediaType *comObject) error {
	hr, _, _ := syscall.SyscallN(reader.method(7), uintptr(unsafe.Pointer(reader)), uintptr(streamIndex), 0, uintptr(unsafe.Pointer(mediaType)))
	return checkHR("SetCurrentMediaType", hr)
}

func mfReaderGetCurrentMediaType(reader *comObject, streamIndex uint32) (*comObject, error) {
	var mt *comObject
	hr, _, _ := syscall.SyscallN(reader.method(6), uintptr(unsafe.Pointer(reader)), uintptr(streamIndex), uintptr(unsafe.Pointer(&mt)))
	return mt, checkHR("GetCurrentMediaType", hr)
}

func mfReaderReadSample(reader *comObject, streamIndex uint32) (flags uint32, sample *comObject, err error) {
	var actualIndex uint32
	var timestamp int64
	hr, _, _ := syscall.SyscallN(reader.method(9),
		uintptr(unsafe.Pointer(reader)),
		uintptr(streamIndex),
		0,
		uintptr(unsafe.Pointer(&actualIndex)),
		uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&timestamp)),
		uintptr(unsafe.Pointer(&sample)),
	)
	return flags, sample, checkHR("ReadSample", hr)
}

// IMFSample inherits IMFAttributes (0..32): ConvertToContiguousBuffer = 43
func mfSampleConvertToContiguousBuffer(sample *comObject) (*comObject, error) {
	var buf *comObject
	hr, _, _ := syscall.SyscallN(sample.method(43), uintptr(unsafe.Pointer(sample)), uintptr(unsafe.Pointer(&buf)))
	return buf, checkHR("ConvertToContiguousBuffer", hr)
}

// IMFMediaBuffer: Lock = 3, Unlock = 4
func mfBufferLock(buf *comObject) ([]byte, error) {
	var ptr *byte
	var maxLen, curLen uint32
	hr, _, _ := syscall.SyscallN(buf.method(3), uintptr(unsafe.Pointer(buf)), uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&curLen)))
	if err := checkHR("Lock", hr); err != nil {
		return nil, err
	}
	if ptr == nil || curLen == 0 {
		return nil, nil
	}
	return unsafe.Slice(ptr, curLen), nil
}

func mfBufferUnlock(buf *comObject) {
	_, _, _ = syscall.SyscallN(buf.method(4), uintptr(unsafe.Pointer(buf)))
}

func checkHR(op string, hr uintptr) error {
	code := int32(hr)
	if code >= 0 {
		return nil
	}
	return fmt.Errorf("video: %s failed (HRESULT 0x%08x)", op, uint32(code))
}

func initCOMAndMF() (cleanup func(), err error) {
	if err := mfplat.Load(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	if err := mfDLL.Load(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	if err := mfreadwrite.Load(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	hr, _, _ := coInitializeEx.Call(0, coinitMultithreaded)
	comInitialized := int32(hr) >= 0
	if !comInitialized && uint32(hr) != rpcEChangedMode {
		return nil, checkHR("CoInitializeEx", hr)
	}
	hr, _, _ = mfStartup.Call(mfVersion, mfStartupLite)
	if err := checkHR("MFStartup", hr); err != nil {
		if comInitialized {
			coUninitialize.Call()
		}
		return nil, err
	}
	return func() {
		mfShutdown.Call()
		if comInitialized {
			coUninitialize.Call()
		}
	}, nil
}

func enumActivates() ([]*comObject, error) {
	var attr *comObject
	hr, _, _ := mfCreateAttributes.Call(uintptr(unsafe.Pointer(&attr)), 1)
	if err := checkHR("MFCreateAttributes", hr); err != nil {
		return nil, err
	}
	defer attr.release()
	if err := mfAttrSetGUID(attr, &guidMFDevSourceType, &guidMFDevSourceTypeVidCap); err != nil {
		return nil, err
	}
	var rawPtr **comObject
	var count uint32
	hr, _, _ = mfEnumDeviceSources.Call(uintptr(unsafe.Pointer(attr)), uintptr(unsafe.Pointer(&rawPtr)), uintptr(unsafe.Pointer(&count)))
	if err := checkHR("MFEnumDeviceSources", hr); err != nil {
		return nil, err
	}
	if rawPtr == nil || count == 0 {
		return nil, nil
	}
	defer coTaskMemFree.Call(uintptr(unsafe.Pointer(rawPtr)))
	slice := unsafe.Slice(rawPtr, count)
	out := make([]*comObject, len(slice))
	copy(out, slice)
	return out, nil
}

func enumerate() ([]Device, error) {
	type result struct {
		devices []Device
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		cleanup, err := initCOMAndMF()
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer cleanup()
		activates, err := enumActivates()
		if err != nil {
			ch <- result{err: err}
			return
		}
		devices := make([]Device, 0, len(activates))
		for i, act := range activates {
			name, _ := mfAttrGetString(act, &guidMFDevSourceFriendlyName)
			id, _ := mfAttrGetString(act, &guidMFDevSourceVidCapSymlink)
			act.release()
			if strings.TrimSpace(id) == "" {
				id = fmt.Sprintf("camera-%d", i)
			}
			if strings.TrimSpace(name) == "" {
				name = fmt.Sprintf("Camera %d", i+1)
			}
			devices = append(devices, Device{ID: id, Name: name, Default: i == 0})
		}
		ch <- result{devices: devices}
	}()
	res := <-ch
	return res.devices, res.err
}

type windowsCapture struct {
	frames    chan Frame
	errCh     chan error
	done      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool
}

func openCapture(deviceID string) (Capture, error) {
	c := &windowsCapture{
		frames: make(chan Frame, 1),
		errCh:  make(chan error, 1),
		done:   make(chan struct{}),
	}
	ready := make(chan error, 1)
	go c.run(deviceID, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return c, nil
}

func (c *windowsCapture) run(deviceID string, ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cleanup, err := initCOMAndMF()
	if err != nil {
		ready <- err
		return
	}
	defer cleanup()

	activates, err := enumActivates()
	if err != nil {
		ready <- err
		return
	}
	if len(activates) == 0 {
		ready <- ErrNoDevice
		return
	}
	var chosen *comObject
	for _, act := range activates {
		id, _ := mfAttrGetString(act, &guidMFDevSourceVidCapSymlink)
		if chosen == nil && (deviceID == "" || id == deviceID) {
			chosen = act
			continue
		}
		act.release()
	}
	if chosen == nil {
		ready <- fmt.Errorf("%w: %s", ErrNoDevice, deviceID)
		return
	}
	source, err := mfActivateObject(chosen, &guidMFMediaSourceIID)
	chosen.release()
	if err != nil {
		ready <- fmt.Errorf("%w: %v", ErrDevice, err)
		return
	}
	defer source.release()

	var readerAttr *comObject
	hr, _, _ := mfCreateAttributes.Call(uintptr(unsafe.Pointer(&readerAttr)), 1)
	if err := checkHR("MFCreateAttributes", hr); err != nil {
		ready <- err
		return
	}
	_ = mfAttrSetUINT32(readerAttr, &guidMFSourceReaderVideoProc, 1)
	var reader *comObject
	hr, _, _ = mfCreateSourceReaderFromMediaSource.Call(uintptr(unsafe.Pointer(source)), uintptr(unsafe.Pointer(readerAttr)), uintptr(unsafe.Pointer(&reader)))
	readerAttr.release()
	if err := checkHR("MFCreateSourceReaderFromMediaSource", hr); err != nil {
		ready <- fmt.Errorf("%w: %v", ErrDevice, err)
		return
	}
	defer reader.release()

	configureReaderMediaType(reader)
	width, height, stride, subtype, err := readCurrentFormat(reader)
	if err != nil || width <= 0 || height <= 0 {
		ready <- fmt.Errorf("%w: could not negotiate camera format: %v", ErrDevice, err)
		return
	}
	ready <- nil

	var lastSent time.Time
	for !c.closed.Load() {
		flags, sample, err := mfReaderReadSample(reader, mfSourceReaderFirstVideoStream)
		if c.closed.Load() {
			if sample != nil {
				sample.release()
			}
			return
		}
		if err != nil {
			c.fail(err)
			return
		}
		if flags&mfSourceReaderFlagError != 0 || flags&mfSourceReaderFlagEndOfStream != 0 {
			if sample != nil {
				sample.release()
			}
			c.fail(ErrDevice)
			return
		}
		if flags&mfSourceReaderFlagStreamTick != 0 || sample == nil {
			if sample != nil {
				sample.release()
			}
			continue
		}
		now := time.Now()
		if !lastSent.IsZero() && now.Sub(lastSent) < defaultTargetFrameInterval {
			sample.release()
			continue
		}
		frame, ok := sampleToBGRA(sample, width, height, stride, subtype)
		sample.release()
		if !ok {
			continue
		}
		lastSent = now
		select {
		case <-c.frames:
		default:
		}
		select {
		case c.frames <- frame:
		case <-c.done:
			return
		}
	}
}

func configureReaderMediaType(reader *comObject) {
	candidates := []windows.GUID{guidMFVideoFormatRGB32, guidMFVideoFormatNV12, guidMFVideoFormatYUY2}
	for _, subtype := range candidates {
		var mt *comObject
		hr, _, _ := mfCreateMediaType.Call(uintptr(unsafe.Pointer(&mt)))
		if checkHR("MFCreateMediaType", hr) != nil || mt == nil {
			continue
		}
		_ = mfAttrSetGUID(mt, &guidMFMTMajorType, &guidMFMediaTypeVideo)
		_ = mfAttrSetGUID(mt, &guidMFMTSubtype, &subtype)
		// Try requesting a moderate resolution first, then any resolution in that subtype.
		_ = mfAttrSetUINT64(mt, &guidMFMTFrameSize, (uint64(defaultPreferredWidth)<<32)|uint64(defaultPreferredHeight))
		if mfReaderSetCurrentMediaType(reader, mfSourceReaderFirstVideoStream, mt) == nil {
			mt.release()
			return
		}
		mt.release()
		hr, _, _ = mfCreateMediaType.Call(uintptr(unsafe.Pointer(&mt)))
		if checkHR("MFCreateMediaType", hr) != nil || mt == nil {
			continue
		}
		_ = mfAttrSetGUID(mt, &guidMFMTMajorType, &guidMFMediaTypeVideo)
		_ = mfAttrSetGUID(mt, &guidMFMTSubtype, &subtype)
		if mfReaderSetCurrentMediaType(reader, mfSourceReaderFirstVideoStream, mt) == nil {
			mt.release()
			return
		}
		mt.release()
	}
}

func readCurrentFormat(reader *comObject) (width, height, stride int, subtype windows.GUID, err error) {
	mt, err := mfReaderGetCurrentMediaType(reader, mfSourceReaderFirstVideoStream)
	if err != nil {
		return 0, 0, 0, windows.GUID{}, err
	}
	defer mt.release()
	packed, err := mfAttrGetUINT64(mt, &guidMFMTFrameSize)
	if err != nil {
		return 0, 0, 0, windows.GUID{}, err
	}
	width, height = int(packed>>32), int(packed&0xffffffff)
	subtype, _ = mfAttrGetGUID(mt, &guidMFMTSubtype)
	if rawStride, err := mfAttrGetUINT32(mt, &guidMFMTDefaultStride); err == nil {
		stride = int(int32(rawStride))
	}
	if stride == 0 {
		switch subtype {
		case guidMFVideoFormatRGB32:
			stride = width * 4
		case guidMFVideoFormatYUY2:
			stride = width * 2
		default:
			stride = width
		}
	}
	return width, height, stride, subtype, nil
}

func sampleToBGRA(sample *comObject, width, height, stride int, subtype windows.GUID) (Frame, bool) {
	buf, err := mfSampleConvertToContiguousBuffer(sample)
	if err != nil || buf == nil {
		return Frame{}, false
	}
	defer buf.release()
	raw, err := mfBufferLock(buf)
	if err != nil || len(raw) == 0 {
		return Frame{}, false
	}
	defer mfBufferUnlock(buf)

	dst := make([]byte, width*height*4)
	switch subtype {
	case guidMFVideoFormatRGB32:
		rowBytes := width * 4
		absStride := stride
		bottomUp := false
		if absStride < 0 {
			absStride = -absStride
			bottomUp = true
		}
		if absStride < rowBytes {
			absStride = rowBytes
		}
		if len(raw) < absStride*height {
			return Frame{}, false
		}
		for y := 0; y < height; y++ {
			sy := y
			if bottomUp {
				sy = height - 1 - y
			}
			srcRow := raw[sy*absStride : sy*absStride+rowBytes]
			dstRow := dst[y*rowBytes : (y+1)*rowBytes]
			copy(dstRow, srcRow)
			for i := 3; i < len(dstRow); i += 4 {
				dstRow[i] = 255
			}
		}
		return Frame{Width: width, Height: height, BGRA: dst}, true
	case guidMFVideoFormatYUY2:
		if len(raw) < width*height*2 {
			return Frame{}, false
		}
		YUY2ToBGRA(dst, raw, width, height)
		return Frame{Width: width, Height: height, BGRA: dst}, true
	case guidMFVideoFormatNV12:
		if len(raw) < width*height*3/2 {
			return Frame{}, false
		}
		NV12ToBGRA(dst, raw, width, height)
		return Frame{Width: width, Height: height, BGRA: dst}, true
	default:
		// Attempt JPEG decode if camera emitted MJPEG despite subtype request.
		if len(raw) >= 2 && binary.BigEndian.Uint16(raw[:2]) == 0xFFD8 {
			if decoded, err := DecodeJPEG(raw); err == nil {
				return decoded, true
			}
		}
		return Frame{}, false
	}
}

func (c *windowsCapture) fail(err error) {
	select {
	case c.errCh <- err:
	default:
	}
}

func (c *windowsCapture) ReadFrame(ctx context.Context) (Frame, error) {
	select {
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	case <-c.done:
		return Frame{}, ErrClosed
	case err := <-c.errCh:
		return Frame{}, err
	case frame := <-c.frames:
		return frame, nil
	}
}

func (c *windowsCapture) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.done)
	})
	return nil
}

// Ensure errors package is referenced for wrap checks.
var _ = errors.Is
