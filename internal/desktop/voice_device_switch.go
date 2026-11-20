package desktop

import (
	"io"
	"sync/atomic"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type openedCallDevice[T io.Closer] struct {
	id     string
	stream T
}

type callDevice[T io.Closer] struct {
	active    atomic.Pointer[openedCallDevice[T]]
	requested string
	switching atomic.Pointer[callDeviceSwitch]
}

type callDeviceSwitch struct{ done chan struct{} }

func (d *callDevice[T]) install(next *openedCallDevice[T]) {
	d.requested = next.id
	d.active.Store(next)
}

// A previous driver can disappear while its replacement is opening. Keep the
// media loop alive until the switch settles or the call ends.
func (d *callDevice[T]) waitForReplacement(old *openedCallDevice[T], done <-chan struct{}) bool {
	for pending := d.switching.Load(); pending != nil; pending = d.switching.Load() {
		select {
		case <-pending.done:
		case <-done:
			return false
		}
	}
	return d.active.Load() != old
}

// Start each endpoint independently. If an obsolete selection fails, retry that
// endpoint alone and keep any exclusive device already opened on the other side.
func openInitialDevice[T io.Closer](runtime *voiceAudioRuntime, callID string, generation uint64, requested *string, open func(string) (T, error), message string) *openedCallDevice[T] {
	runtime.mu.Lock()
	for !runtime.closed && runtime.generation == generation && runtime.desiredID == callID {
		id := *requested
		runtime.mu.Unlock()
		stream, err := open(id)
		if err == nil {
			return &openedCallDevice[T]{id: id, stream: stream}
		}
		runtime.mu.Lock()
		if id == *requested {
			runtime.failLocked(callID, generation, nil, i18n.T(message)+err.Error())
			return nil
		}
	}
	runtime.mu.Unlock()
	return nil
}

// Called under runtime.mu. Keep the current endpoint running while the native
// driver opens its replacement. A newer selection or hang-up invalidates it.
func switchCallDevice[T io.Closer](runtime *voiceAudioRuntime, session *voiceAudioSession, device *callDevice[T], id string, open func(string) (T, error), command domain.CommandKind) {
	if device.requested == id {
		return
	}
	device.requested = id
	old := device.active.Load()
	var pending *callDeviceSwitch
	if old.id != id {
		pending = &callDeviceSwitch{done: make(chan struct{})}
	}
	if previous := device.switching.Swap(pending); previous != nil {
		close(previous.done)
	}
	if pending == nil {
		return
	}
	go func() {
		next, err := open(id)
		runtime.mu.Lock()
		if runtime.session != session || device.switching.Load() != pending {
			runtime.mu.Unlock()
			if err == nil {
				_ = next.Close()
			}
			return
		}
		if err == nil {
			device.install(&openedCallDevice[T]{id: id, stream: next})
		}
		device.switching.Store(nil)
		close(pending.done)
		if err == nil {
			runtime.mu.Unlock()
			_ = old.stream.Close()
			return
		}
		runtime.pendingNotice = i18n.T("设备切换失败，继续使用原设备。") + " " + err.Error()
		submit, invalidate := runtime.submit, runtime.invalidate
		runtime.mu.Unlock()
		if submit != nil {
			submit(domain.Command{Kind: command, CallID: session.callID, Text: old.id, ExpectedDevice: &id})
		}
		if invalidate != nil {
			invalidate()
		}
	}()
}
