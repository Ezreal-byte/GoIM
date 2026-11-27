package desktop

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
	"goim/internal/i18n"
	"goim/internal/video"
)

const (
	videoRemoteBitmapID uint64 = 9001
	videoLocalBitmapID  uint64 = 9002
)

// videoCallMedia coordinates local camera capture and remote JPEG decoding for
// one live video call window. All heavy work runs off the UI paint thread.
type videoCallMedia struct {
	mu           sync.Mutex
	backend      domain.Backend
	wake         func()
	openCamera   func(string) (video.Capture, error)
	closed       bool
	callID       string
	cameraID     string
	cameraOff    bool
	active       bool
	captureStop  context.CancelFunc
	receiveStop  context.CancelFunc
	cameraError  string
	localPending atomic.Pointer[video.Frame]
	remotePend   atomic.Pointer[video.Frame]
	localBitmap  gfx.Bitmap
	remoteBitmap gfx.Bitmap
}

func newVideoCallMedia(backend domain.Backend, wake func()) *videoCallMedia {
	return &videoCallMedia{
		backend:    backend,
		wake:       wake,
		openCamera: video.OpenCapture,
	}
}

func (m *videoCallMedia) sync(call domain.VoiceCall) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	if !call.Live() || !call.Video {
		m.stopAllLocked()
		return
	}

	active := call.Status == domain.VoiceCallActive
	callChanged := m.callID != call.ID
	if callChanged {
		m.stopAllLocked()
		m.callID = call.ID
		m.cameraError = ""
	}

	// Local camera lifecycle: run during outgoing ringing (self preview) and active call when camera is on.
	wantCapture := !call.CameraOff && (active || call.Direction == domain.VoiceCallOutgoing)
	cameraChanged := m.cameraID != call.Camera
	if !wantCapture || cameraChanged {
		m.stopCaptureLocked()
		if !wantCapture {
			m.localPending.Store(nil)
			m.localBitmap = gfx.Bitmap{}
			m.cameraError = ""
		}
	}
	m.cameraOff = call.CameraOff
	m.cameraID = call.Camera
	m.active = active

	if wantCapture && m.captureStop == nil {
		ctx, cancel := context.WithCancel(context.Background())
		m.captureStop = cancel
		go m.captureLoop(ctx, call.ID, call.Camera)
	}

	// Remote frame receive lifecycle: only active once connected.
	if active && m.receiveStop == nil {
		if vb, ok := m.backend.(domain.VideoCallBackend); ok {
			if ch := vb.VideoFrames(call.ID); ch != nil {
				ctx, cancel := context.WithCancel(context.Background())
				m.receiveStop = cancel
				go m.receiveLoop(ctx, ch)
			}
		}
	}
}

func (m *videoCallMedia) captureLoop(ctx context.Context, callID, deviceID string) {
	open := m.openCamera
	if open == nil {
		open = video.OpenCapture
	}
	capDev, err := open(deviceID)
	if err != nil {
		m.mu.Lock()
		if m.callID == callID && !m.cameraOff {
			m.cameraError = videoCameraErrorText(err)
		}
		m.mu.Unlock()
		m.notify()
		return
	}
	defer capDev.Close()

	m.mu.Lock()
	if m.callID == callID {
		m.cameraError = ""
	}
	m.mu.Unlock()

	vb, _ := m.backend.(domain.VideoCallBackend)
	var lastEncode time.Time
	for {
		if ctx.Err() != nil {
			return
		}
		frame, err := capDev.ReadFrame(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			m.mu.Lock()
			if m.callID == callID && !m.cameraOff {
				m.cameraError = videoCameraErrorText(err)
			}
			m.mu.Unlock()
			m.notify()
			return
		}
		if frame.Width <= 0 || frame.Height <= 0 || len(frame.BGRA) < frame.Width*frame.Height*4 {
			continue
		}
		frameCopy := frame
		m.localPending.Store(&frameCopy)
		m.notify()

		m.mu.Lock()
		shouldSend := m.callID == callID && m.active && !m.cameraOff
		m.mu.Unlock()
		if shouldSend && vb != nil && (lastEncode.IsZero() || time.Since(lastEncode) >= 60*time.Millisecond) {
			if encoded, err := video.EncodeJPEG(frameCopy, video.DefaultMaxWidth, video.DefaultMaxHeight, video.DefaultQuality); err == nil && len(encoded) <= domain.VideoMaxFrameBytes {
				lastEncode = time.Now()
				_ = vb.SendVideoFrame(callID, encoded)
			}
		}
	}
}

func (m *videoCallMedia) receiveLoop(ctx context.Context, frames <-chan []byte) {
	for {
		select {
		case <-ctx.Done():
			return
		case encoded, ok := <-frames:
			if !ok {
				return
			}
			decoded, err := video.DecodeJPEG(encoded)
			if err != nil || decoded.Width <= 0 || decoded.Height <= 0 {
				continue
			}
			m.remotePend.Store(&decoded)
			m.notify()
		}
	}
}

func (m *videoCallMedia) pullFrames() (remote, local gfx.Bitmap) {
	if m == nil {
		return gfx.Bitmap{}, gfx.Bitmap{}
	}
	if frame := m.remotePend.Swap(nil); frame != nil && frame.Width > 0 && frame.Height > 0 && len(frame.BGRA) == frame.Width*frame.Height*4 {
		m.remoteBitmap = gfx.Bitmap{
			ID: videoRemoteBitmapID, Version: m.remoteBitmap.Version + 1,
			Pix: frame.BGRA, Width: frame.Width, Height: frame.Height, Stride: frame.Width * 4,
		}
	}
	if frame := m.localPending.Swap(nil); frame != nil && frame.Width > 0 && frame.Height > 0 && len(frame.BGRA) == frame.Width*frame.Height*4 {
		m.localBitmap = gfx.Bitmap{
			ID: videoLocalBitmapID, Version: m.localBitmap.Version + 1,
			Pix: frame.BGRA, Width: frame.Width, Height: frame.Height, Stride: frame.Width * 4,
		}
	}
	return m.remoteBitmap, m.localBitmap
}

func (m *videoCallMedia) cameraProblem() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cameraError
}

func (m *videoCallMedia) stopCaptureLocked() {
	if m.captureStop != nil {
		m.captureStop()
		m.captureStop = nil
	}
}

func (m *videoCallMedia) stopAllLocked() {
	m.stopCaptureLocked()
	if m.receiveStop != nil {
		m.receiveStop()
		m.receiveStop = nil
	}
	m.callID = ""
	m.cameraID = ""
	m.active = false
	m.cameraError = ""
	m.localPending.Store(nil)
	m.remotePend.Store(nil)
	m.localBitmap = gfx.Bitmap{}
	m.remoteBitmap = gfx.Bitmap{}
}

func (m *videoCallMedia) close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	m.stopAllLocked()
	m.mu.Unlock()
}

func (m *videoCallMedia) notify() {
	if m.wake != nil {
		m.wake()
	}
}

func videoCameraErrorText(err error) string {
	switch err {
	case video.ErrNoDevice:
		return i18n.T("未检测到可用摄像头")
	case video.ErrUnsupported:
		return i18n.T("当前环境不支持摄像头采集")
	default:
		return i18n.T("摄像头暂时不可用")
	}
}
