//go:build windows

package voice

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	waveMapper       = ^uintptr(0)
	waveFormatPCM    = 1
	waveCallbackFunc = 0x00030000
	waveInData       = 0x3c0
	waveOutDone      = 0x3bd
	waveHeaderDone   = 0x1
	waveBuffers      = 4
)

type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	Size           uint16
}

type waveHeader struct {
	Data          *byte
	Length        uint32
	BytesRecorded uint32
	User          uintptr
	Flags         uint32
	Loops         uint32
	Next          *waveHeader
	Reserved      uintptr
}

type waveInCaps struct {
	ManufacturerID uint16
	ProductID      uint16
	DriverVersion  uint32
	Name           [32]uint16
	Formats        uint32
	Channels       uint16
	Reserved       uint16
}

type waveOutCaps struct {
	ManufacturerID uint16
	ProductID      uint16
	DriverVersion  uint32
	Name           [32]uint16
	Formats        uint32
	Channels       uint16
	Reserved       uint16
	Support        uint32
}

var (
	winmm = windows.NewLazySystemDLL("winmm.dll")

	waveInGetNumDevs      = winmm.NewProc("waveInGetNumDevs")
	waveInGetDevCapsW     = winmm.NewProc("waveInGetDevCapsW")
	waveInGetErrorTextW   = winmm.NewProc("waveInGetErrorTextW")
	waveInOpen            = winmm.NewProc("waveInOpen")
	waveInPrepareHeader   = winmm.NewProc("waveInPrepareHeader")
	waveInUnprepareHeader = winmm.NewProc("waveInUnprepareHeader")
	waveInAddBuffer       = winmm.NewProc("waveInAddBuffer")
	waveInStart           = winmm.NewProc("waveInStart")
	waveInStop            = winmm.NewProc("waveInStop")
	waveInReset           = winmm.NewProc("waveInReset")
	waveInClose           = winmm.NewProc("waveInClose")

	waveOutGetNumDevs      = winmm.NewProc("waveOutGetNumDevs")
	waveOutGetDevCapsW     = winmm.NewProc("waveOutGetDevCapsW")
	waveOutGetErrorTextW   = winmm.NewProc("waveOutGetErrorTextW")
	waveOutOpen            = winmm.NewProc("waveOutOpen")
	waveOutPrepareHeader   = winmm.NewProc("waveOutPrepareHeader")
	waveOutUnprepareHeader = winmm.NewProc("waveOutUnprepareHeader")
	waveOutWrite           = winmm.NewProc("waveOutWrite")
	waveOutReset           = winmm.NewProc("waveOutReset")
	waveOutClose           = winmm.NewProc("waveOutClose")
	waveInCallbackPointer  = windows.NewCallback(waveInProc)
	waveOutCallbackPointer = windows.NewCallback(waveOutProc)
	waveContexts           sync.Map
	waveNextContext        atomic.Uintptr
)

func nextWaveContext(value any) uintptr {
	id := waveNextContext.Add(1)
	waveContexts.Store(id, value)
	return id
}

func dropWaveContext(id uintptr) {
	waveContexts.Delete(id)
}

func waveInProc(_, message, instance uintptr, header *waveHeader, _ uintptr) uintptr {
	value, ok := waveContexts.Load(instance)
	if !ok {
		return 0
	}
	capture, ok := value.(*windowsCapture)
	if !ok || capture.closed.Load() {
		return 0
	}
	switch uint32(message) {
	case waveInData:
		if header == nil || header.BytesRecorded == 0 || header.Data == nil {
			return 0
		}
		pcm := append([]byte(nil), unsafe.Slice(header.Data, int(header.BytesRecorded))...)
		capture.deliver(pcm)
		header.BytesRecorded = 0
		header.Length = uint32(len(capture.buffers[0]))
		header.Flags &^= waveHeaderDone
		if !capture.closed.Load() {
			waveInAddBuffer.Call(capture.handle, uintptr(unsafe.Pointer(header)), unsafe.Sizeof(waveHeader{}))
		}
	}
	return 0
}

type windowsCapture struct {
	handle    uintptr
	contextID uintptr
	headers   [waveBuffers]waveHeader
	buffers   [waveBuffers][]byte
	pinner    runtime.Pinner
	data      chan []byte
	done      chan struct{}
	pending   []byte
	closed    atomic.Bool
	closeOnce sync.Once
}

func (c *windowsCapture) deliver(pcm []byte) {
	select {
	case c.data <- pcm:
	default:
		select {
		case <-c.data:
		default:
		}
		select {
		case c.data <- pcm:
		default:
		}
	}
}

func (c *windowsCapture) Read(pcm []byte) (int, error) {
	if len(pcm) == 0 {
		return 0, nil
	}
	for len(c.pending) == 0 {
		select {
		case frame := <-c.data:
			c.pending = frame
		case <-c.done:
			return 0, ErrClosed
		}
	}
	n := copy(pcm, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

func (c *windowsCapture) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.done)
		if c.handle != 0 {
			waveInStop.Call(c.handle)
			waveInReset.Call(c.handle)
			for i := range c.headers {
				waveInUnprepareHeader.Call(c.handle, uintptr(unsafe.Pointer(&c.headers[i])), unsafe.Sizeof(waveHeader{}))
			}
			waveInClose.Call(c.handle)
			c.handle = 0
		}
		dropWaveContext(c.contextID)
		c.pinner.Unpin()
	})
	return nil
}

type windowsPlayback struct {
	handle    uintptr
	contextID uintptr
	headers   [waveBuffers]waveHeader
	buffers   [waveBuffers][]byte
	pinner    runtime.Pinner
	free      chan int
	done      chan struct{}
	closed    atomic.Bool
	closeOnce sync.Once
}

func waveOutProc(_, message, instance uintptr, header *waveHeader, _ uintptr) uintptr {
	value, ok := waveContexts.Load(instance)
	if !ok {
		return 0
	}
	playback, ok := value.(*windowsPlayback)
	if !ok || playback.closed.Load() || uint32(message) != waveOutDone {
		return 0
	}
	base := uintptr(unsafe.Pointer(&playback.headers[0]))
	address := uintptr(unsafe.Pointer(header))
	size := unsafe.Sizeof(waveHeader{})
	if address < base || address >= base+uintptr(waveBuffers)*size {
		return 0
	}
	offset := address - base
	if offset%size != 0 {
		return 0
	}
	select {
	case playback.free <- int(offset / size):
	default:
	}
	return 0
}

func (p *windowsPlayback) Write(pcm []byte) (int, error) {
	written := 0
	for len(pcm) > 0 {
		var index int
		select {
		case index = <-p.free:
		case <-p.done:
			return written, ErrClosed
		}
		if p.closed.Load() {
			return written, ErrClosed
		}
		n := min(len(pcm), len(p.buffers[index]))
		copy(p.buffers[index], pcm[:n])
		header := &p.headers[index]
		header.Length = uint32(n)
		header.BytesRecorded = 0
		header.Flags &^= waveHeaderDone
		result, _, _ := waveOutWrite.Call(p.handle, uintptr(unsafe.Pointer(header)), unsafe.Sizeof(waveHeader{}))
		if result != 0 {
			select {
			case p.free <- index:
			default:
			}
			return written, fmt.Errorf("%w: %s", ErrDevice, waveError(result, false))
		}
		pcm, written = pcm[n:], written+n
	}
	return written, nil
}

func (p *windowsPlayback) Close() error {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		close(p.done)
		if p.handle != 0 {
			waveOutReset.Call(p.handle)
			for i := range p.headers {
				waveOutUnprepareHeader.Call(p.handle, uintptr(unsafe.Pointer(&p.headers[i])), unsafe.Sizeof(waveHeader{}))
			}
			waveOutClose.Call(p.handle)
			p.handle = 0
		}
		dropWaveContext(p.contextID)
		p.pinner.Unpin()
	})
	return nil
}

func enumerate() Devices {
	var devices Devices
	for i, count := 0, waveInDeviceCount(); i < count; i++ {
		var caps waveInCaps
		if result, _, _ := waveInGetDevCapsW.Call(uintptr(i), uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps)); result != 0 {
			continue
		}
		name := strings.TrimSpace(windows.UTF16ToString(caps.Name[:]))
		if name == "" {
			name = fmt.Sprintf("麦克风 %d", i+1)
		}
		devices.Microphones = append(devices.Microphones, Device{ID: "wavein:" + strconv.Itoa(i), Name: name})
	}
	for i, count := 0, waveOutDeviceCount(); i < count; i++ {
		var caps waveOutCaps
		if result, _, _ := waveOutGetDevCapsW.Call(uintptr(i), uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps)); result != 0 {
			continue
		}
		name := strings.TrimSpace(windows.UTF16ToString(caps.Name[:]))
		if name == "" {
			name = fmt.Sprintf("扬声器 %d", i+1)
		}
		devices.Speakers = append(devices.Speakers, Device{ID: "waveout:" + strconv.Itoa(i), Name: name})
	}
	return devices
}

func openCapture(id string) (Capture, error) {
	device, err := parseWaveDevice(id, "wavein", waveInDeviceCount())
	if err != nil {
		return nil, err
	}
	capture := &windowsCapture{data: make(chan []byte, 16), done: make(chan struct{})}
	format := streamFormat()
	capture.contextID = nextWaveContext(capture)
	result, _, _ := waveInOpen.Call(
		uintptr(unsafe.Pointer(&capture.handle)),
		device,
		uintptr(unsafe.Pointer(&format)),
		waveInCallbackPointer,
		capture.contextID,
		waveCallbackFunc,
	)
	runtime.KeepAlive(&format)
	if result != 0 {
		dropWaveContext(capture.contextID)
		return nil, fmt.Errorf("%w: %s", ErrDevice, waveError(result, true))
	}
	for i := range capture.buffers {
		capture.buffers[i] = make([]byte, PCMFrameBytes)
		capture.headers[i].Data = &capture.buffers[i][0]
		capture.headers[i].Length = uint32(len(capture.buffers[i]))
		capture.pinner.Pin(&capture.headers[i])
		capture.pinner.Pin(&capture.buffers[i][0])
		if result, _, _ := waveInPrepareHeader.Call(capture.handle, uintptr(unsafe.Pointer(&capture.headers[i])), unsafe.Sizeof(waveHeader{})); result != 0 {
			capture.Close()
			return nil, fmt.Errorf("%w: %s", ErrDevice, waveError(result, true))
		}
		if result, _, _ := waveInAddBuffer.Call(capture.handle, uintptr(unsafe.Pointer(&capture.headers[i])), unsafe.Sizeof(waveHeader{})); result != 0 {
			capture.Close()
			return nil, fmt.Errorf("%w: %s", ErrDevice, waveError(result, true))
		}
	}
	if result, _, _ := waveInStart.Call(capture.handle); result != 0 {
		capture.Close()
		return nil, fmt.Errorf("%w: %s", ErrDevice, waveError(result, true))
	}
	return capture, nil
}

func openPlayback(id string) (Playback, error) {
	device, err := parseWaveDevice(id, "waveout", waveOutDeviceCount())
	if err != nil {
		return nil, err
	}
	playback := &windowsPlayback{
		free: make(chan int, waveBuffers), done: make(chan struct{}),
	}
	format := streamFormat()
	playback.contextID = nextWaveContext(playback)
	result, _, _ := waveOutOpen.Call(
		uintptr(unsafe.Pointer(&playback.handle)),
		device,
		uintptr(unsafe.Pointer(&format)),
		waveOutCallbackPointer,
		playback.contextID,
		waveCallbackFunc,
	)
	runtime.KeepAlive(&format)
	if result != 0 {
		dropWaveContext(playback.contextID)
		return nil, fmt.Errorf("%w: %s", ErrDevice, waveError(result, false))
	}
	for i := range playback.buffers {
		playback.buffers[i] = make([]byte, PCMFrameBytes)
		playback.headers[i].Data = &playback.buffers[i][0]
		playback.headers[i].Length = uint32(len(playback.buffers[i]))
		playback.pinner.Pin(&playback.headers[i])
		playback.pinner.Pin(&playback.buffers[i][0])
		if result, _, _ := waveOutPrepareHeader.Call(playback.handle, uintptr(unsafe.Pointer(&playback.headers[i])), unsafe.Sizeof(waveHeader{})); result != 0 {
			playback.Close()
			return nil, fmt.Errorf("%w: %s", ErrDevice, waveError(result, false))
		}
		playback.free <- i
	}
	return playback, nil
}

func parseWaveDevice(id, prefix string, count int) (uintptr, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == "default" {
		return waveMapper, nil
	}
	value, ok := strings.CutPrefix(id, prefix+":")
	if !ok {
		return 0, fmt.Errorf("%w: 未知的设备标识", ErrDevice)
	}
	index, err := strconv.Atoi(value)
	if err != nil || index < 0 || index >= count {
		return 0, fmt.Errorf("%w: 设备已移除", ErrDevice)
	}
	return uintptr(index), nil
}

func streamFormat() waveFormatEx {
	format := waveFormatEx{
		FormatTag: waveFormatPCM, Channels: 1, SamplesPerSec: SampleRate,
		BlockAlign: 2, BitsPerSample: 16,
	}
	format.AvgBytesPerSec = format.SamplesPerSec * uint32(format.BlockAlign)
	return format
}

func waveInDeviceCount() int {
	count, _, _ := waveInGetNumDevs.Call()
	return int(count)
}

func waveOutDeviceCount() int {
	count, _, _ := waveOutGetNumDevs.Call()
	return int(count)
}

func waveError(code uintptr, input bool) string {
	var text [256]uint16
	if input {
		waveInGetErrorTextW.Call(code, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
	} else {
		waveOutGetErrorTextW.Call(code, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
	}
	message := strings.TrimSpace(windows.UTF16ToString(text[:]))
	if message == "" {
		message = fmt.Sprintf("wave error %d", code)
	}
	return message
}
