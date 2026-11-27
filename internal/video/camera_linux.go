//go:build linux

package video

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	v4l2BufTypeVideoCapture = 1
	v4l2MemoryMmap          = 1
	v4l2FieldAny            = 0
	v4l2CapVideoCapture     = 0x00000001
	v4l2CapStreaming        = 0x04000000
	v4l2PixFmtYUYV          = uint32('Y') | uint32('U')<<8 | uint32('Y')<<16 | uint32('V')<<24
	v4l2PixFmtMJPEG         = uint32('M') | uint32('J')<<8 | uint32('P')<<16 | uint32('G')<<24

	iocWrite = 1
	iocRead  = 2
)

func v4l2IOC(dir, nr uintptr, size uintptr) uintptr {
	return (dir << 30) | (size << 16) | (uintptr('V') << 8) | nr
}

var (
	vidiocQueryCap  = v4l2IOC(iocRead, 0, unsafe.Sizeof(v4l2Capability{}))
	vidiocSFmt      = v4l2IOC(iocRead|iocWrite, 5, unsafe.Sizeof(v4l2Format{}))
	vidiocReqBufs   = v4l2IOC(iocRead|iocWrite, 8, unsafe.Sizeof(v4l2RequestBuffers{}))
	vidiocQueryBuf  = v4l2IOC(iocRead|iocWrite, 9, unsafe.Sizeof(v4l2Buffer{}))
	vidiocQBuf      = v4l2IOC(iocRead|iocWrite, 15, unsafe.Sizeof(v4l2Buffer{}))
	vidiocDQBuf     = v4l2IOC(iocRead|iocWrite, 17, unsafe.Sizeof(v4l2Buffer{}))
	vidiocStreamOn  = v4l2IOC(iocWrite, 18, unsafe.Sizeof(uint32(0)))
	vidiocStreamOff = v4l2IOC(iocWrite, 19, unsafe.Sizeof(uint32(0)))
)

type v4l2Capability struct {
	Driver       [16]byte
	Card         [32]byte
	BusInfo      [32]byte
	Version      uint32
	Capabilities uint32
	DeviceCaps   uint32
	Reserved     [3]uint32
}

type v4l2PixFormat struct {
	Width        uint32
	Height       uint32
	PixelFormat  uint32
	Field        uint32
	BytesPerLine uint32
	SizeImage    uint32
	Colorspace   uint32
	Priv         uint32
	Flags        uint32
	YcbcrEnc     uint32
	Quantization uint32
	XferFunc     uint32
}

type v4l2Format struct {
	Type uint32
	_    uint32 // padding on 64-bit for 8-byte aligned union
	Raw  [200]byte
}

type v4l2RequestBuffers struct {
	Count    uint32
	Type     uint32
	Memory   uint32
	Reserved [2]uint32
}

type v4l2Timecode struct {
	Type     uint32
	Flags    uint32
	Frames   uint8
	Seconds  uint8
	Minutes  uint8
	Hours    uint8
	Userbits [4]uint8
}

type v4l2Buffer struct {
	Index     uint32
	Type      uint32
	BytesUsed uint32
	Flags     uint32
	Field     uint32
	_         uint32
	Timestamp unix.Timeval
	Timecode  v4l2Timecode
	Sequence  uint32
	Memory    uint32
	Offset    uint32
	_         uint32
	Length    uint32
	Reserved2 uint32
	Reserved  uint32
}

func v4l2Ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	for {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

func enumerate() ([]Device, error) {
	paths, _ := filepath.Glob("/dev/video*")
	sort.Strings(paths)
	var devices []Device
	for _, path := range paths {
		fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		var capInfo v4l2Capability
		err = v4l2Ioctl(fd, vidiocQueryCap, unsafe.Pointer(&capInfo))
		_ = unix.Close(fd)
		if err != nil {
			continue
		}
		caps := capInfo.Capabilities
		if capInfo.DeviceCaps != 0 {
			caps = capInfo.DeviceCaps
		}
		if caps&(v4l2CapVideoCapture|v4l2CapStreaming) != (v4l2CapVideoCapture | v4l2CapStreaming) {
			continue
		}
		name := strings.TrimSpace(cString(capInfo.Card[:]))
		if name == "" {
			name = filepath.Base(path)
		}
		devices = append(devices, Device{ID: path, Name: name, Default: len(devices) == 0})
	}
	return devices, nil
}

type linuxCapture struct {
	fd        int
	width     int
	height    int
	pixFmt    uint32
	buffers   [][]byte
	done      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool
	lastRead  time.Time
}

func openCapture(deviceID string) (Capture, error) {
	path := deviceID
	if path == "" {
		devices, err := enumerate()
		if err != nil {
			return nil, err
		}
		if len(devices) == 0 {
			return nil, ErrNoDevice
		}
		path = devices[0].ID
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoDevice, err)
	}
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDevice, err)
	}
	var pix v4l2PixFormat
	var chosenFmt uint32
	for _, candidate := range []uint32{v4l2PixFmtMJPEG, v4l2PixFmtYUYV} {
		var format v4l2Format
		format.Type = v4l2BufTypeVideoCapture
		p := (*v4l2PixFormat)(unsafe.Pointer(&format.Raw[0]))
		p.Width = 640
		p.Height = 360
		p.PixelFormat = candidate
		p.Field = v4l2FieldAny
		if err := v4l2Ioctl(fd, vidiocSFmt, unsafe.Pointer(&format)); err == nil && (p.PixelFormat == v4l2PixFmtMJPEG || p.PixelFormat == v4l2PixFmtYUYV) {
			pix = *p
			chosenFmt = p.PixelFormat
			break
		}
	}
	if chosenFmt == 0 || pix.Width == 0 || pix.Height == 0 {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("%w: unsupported pixel format", ErrDevice)
	}
	req := v4l2RequestBuffers{Count: 3, Type: v4l2BufTypeVideoCapture, Memory: v4l2MemoryMmap}
	if err := v4l2Ioctl(fd, vidiocReqBufs, unsafe.Pointer(&req)); err != nil || req.Count == 0 {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("%w: reqbufs failed: %v", ErrDevice, err)
	}
	buffers := make([][]byte, 0, req.Count)
	cleanupBuffers := func() {
		for _, b := range buffers {
			_ = unix.Munmap(b)
		}
		_ = unix.Close(fd)
	}
	for i := uint32(0); i < req.Count; i++ {
		buf := v4l2Buffer{Type: v4l2BufTypeVideoCapture, Memory: v4l2MemoryMmap, Index: i}
		if err := v4l2Ioctl(fd, vidiocQueryBuf, unsafe.Pointer(&buf)); err != nil {
			cleanupBuffers()
			return nil, fmt.Errorf("%w: querybuf failed: %v", ErrDevice, err)
		}
		mapped, err := unix.Mmap(fd, int64(buf.Offset), int(buf.Length), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			cleanupBuffers()
			return nil, fmt.Errorf("%w: mmap failed: %v", ErrDevice, err)
		}
		buffers = append(buffers, mapped)
		if err := v4l2Ioctl(fd, vidiocQBuf, unsafe.Pointer(&buf)); err != nil {
			cleanupBuffers()
			return nil, fmt.Errorf("%w: qbuf failed: %v", ErrDevice, err)
		}
	}
	streamType := uint32(v4l2BufTypeVideoCapture)
	if err := v4l2Ioctl(fd, vidiocStreamOn, unsafe.Pointer(&streamType)); err != nil {
		cleanupBuffers()
		return nil, fmt.Errorf("%w: streamon failed: %v", ErrDevice, err)
	}
	return &linuxCapture{
		fd: fd, width: int(pix.Width), height: int(pix.Height), pixFmt: chosenFmt,
		buffers: buffers, done: make(chan struct{}),
	}, nil
}

func (c *linuxCapture) ReadFrame(ctx context.Context) (Frame, error) {
	for {
		if c.closed.Load() {
			return Frame{}, ErrClosed
		}
		if err := ctx.Err(); err != nil {
			return Frame{}, err
		}
		fds := []unix.PollFd{{Fd: int32(c.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 100)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return Frame{}, err
		}
		if n == 0 {
			continue
		}
		buf := v4l2Buffer{Type: v4l2BufTypeVideoCapture, Memory: v4l2MemoryMmap}
		if err := v4l2Ioctl(c.fd, vidiocDQBuf, unsafe.Pointer(&buf)); err != nil {
			if err == syscall.EAGAIN {
				continue
			}
			return Frame{}, err
		}
		var out Frame
		var frameErr error
		if int(buf.Index) < len(c.buffers) && int(buf.BytesUsed) <= len(c.buffers[buf.Index]) {
			raw := c.buffers[buf.Index][:buf.BytesUsed]
			if c.pixFmt == v4l2PixFmtMJPEG {
				out, frameErr = DecodeJPEG(raw)
			} else {
				dst := make([]byte, c.width*c.height*4)
				YUY2ToBGRA(dst, raw, c.width, c.height)
				out = Frame{Width: c.width, Height: c.height, BGRA: dst}
			}
		}
		_ = v4l2Ioctl(c.fd, vidiocQBuf, unsafe.Pointer(&buf))
		if frameErr != nil || len(out.BGRA) == 0 {
			continue
		}
		now := time.Now()
		if !c.lastRead.IsZero() && now.Sub(c.lastRead) < 50*time.Millisecond {
			continue
		}
		c.lastRead = now
		return out, nil
	}
}

func (c *linuxCapture) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.done)
		streamType := uint32(v4l2BufTypeVideoCapture)
		_ = v4l2Ioctl(c.fd, vidiocStreamOff, unsafe.Pointer(&streamType))
		for _, b := range c.buffers {
			_ = unix.Munmap(b)
		}
		_ = unix.Close(c.fd)
	})
	return nil
}
