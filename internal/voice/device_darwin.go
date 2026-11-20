//go:build darwin

package voice

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	audioFourCCLPCM         = 0x6c70636d
	audioLinearPCMFloatFlag = 1 << 0
	audioLinearPCMSigned    = 1 << 2
	audioLinearPCMPacked    = 1 << 3

	audioPropertyMainElement uint32 = 0
	audioPropertyGlobal      uint32 = 0x676c6f62 // glob
	audioPropertyInput       uint32 = 0x696e7074 // inpt
	audioPropertyOutput      uint32 = 0x6f757470 // outp
	audioDevicesSelector     uint32 = 0x64657623 // dev#
	audioStreamsSelector     uint32 = 0x73746d23 // stm#
	audioDeviceNameSelector  uint32 = 0x6c6e616d // lnam
	audioDeviceUIDSelector   uint32 = 0x75696420 // uid
	audioDefaultInput        uint32 = 0x64496e20 // dIn
	audioDefaultOutput       uint32 = 0x644f7574 // dOut
	audioQueueDeviceProperty uint32 = 0x61716364 // aqcd
)

type audioPropertyAddress struct {
	Selector uint32
	Scope    uint32
	Element  uint32
}

type audioStreamBasicDescription struct {
	SampleRate       float64
	FormatID         uint32
	FormatFlags      uint32
	BytesPerPacket   uint32
	FramesPerPacket  uint32
	BytesPerFrame    uint32
	ChannelsPerFrame uint32
	BitsPerChannel   uint32
	Reserved         uint32
}

type audioQueueBuffer struct {
	Capacity                  uint32
	_                         uint32
	Data                      unsafe.Pointer
	ByteSize                  uint32
	_                         uint32
	UserData                  unsafe.Pointer
	PacketDescriptionCapacity uint32
	_                         uint32
	PacketDescriptions        unsafe.Pointer
	PacketDescriptionCount    uint32
	_                         uint32
}

type coreAudioAPI struct {
	getDataSize  func(object uint32, address *audioPropertyAddress, qualifierSize uint32, qualifier unsafe.Pointer, size *uint32) int32
	getData      func(object uint32, address *audioPropertyAddress, qualifierSize uint32, qualifier unsafe.Pointer, size *uint32, data unsafe.Pointer) int32
	cfGetString  func(value uintptr, buffer *byte, length int64, encoding uint32) uint8
	cfRelease    func(value uintptr)
	queueNewIn   func(format *audioStreamBasicDescription, callback, userData, runLoop, mode uintptr, flags uint32, queue *uintptr) int32
	queueNewOut  func(format *audioStreamBasicDescription, callback, userData, runLoop, mode uintptr, flags uint32, queue *uintptr) int32
	queueAlloc   func(queue uintptr, bytes uint32, buffer *unsafe.Pointer) int32
	queueQueue   func(queue, buffer uintptr, packetCount uint32, packets uintptr) int32
	queueStart   func(queue uintptr, time uintptr) int32
	queueStop    func(queue uintptr, immediate uint8) int32
	queueDispose func(queue uintptr, immediate uint8) int32
	queueSet     func(queue uintptr, property uint32, data unsafe.Pointer, size uint32) int32
}

var (
	coreAudioOnce    sync.Once
	coreAudio        coreAudioAPI
	coreAudioProblem string
	inputCallback    = purego.NewCallback(audioInputCallback)
	outputCallback   = purego.NewCallback(audioOutputCallback)
	audioContexts    sync.Map
	audioNextContext atomic.Uintptr
)

func macAudio() (*coreAudioAPI, error) {
	coreAudioOnce.Do(func() {
		const flags = purego.RTLD_NOW | purego.RTLD_GLOBAL
		coreFoundation, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", flags)
		if err != nil {
			coreAudioProblem = err.Error()
			return
		}
		coreAudioLibrary, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", flags)
		if err != nil {
			coreAudioProblem = err.Error()
			return
		}
		audioToolbox, err := purego.Dlopen("/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", flags)
		if err != nil {
			coreAudioProblem = err.Error()
			return
		}
		purego.RegisterLibFunc(&coreAudio.getDataSize, coreAudioLibrary, "AudioObjectGetPropertyDataSize")
		purego.RegisterLibFunc(&coreAudio.getData, coreAudioLibrary, "AudioObjectGetPropertyData")
		purego.RegisterLibFunc(&coreAudio.cfGetString, coreFoundation, "CFStringGetCString")
		purego.RegisterLibFunc(&coreAudio.cfRelease, coreFoundation, "CFRelease")
		purego.RegisterLibFunc(&coreAudio.queueNewIn, audioToolbox, "AudioQueueNewInput")
		purego.RegisterLibFunc(&coreAudio.queueNewOut, audioToolbox, "AudioQueueNewOutput")
		purego.RegisterLibFunc(&coreAudio.queueAlloc, audioToolbox, "AudioQueueAllocateBuffer")
		purego.RegisterLibFunc(&coreAudio.queueQueue, audioToolbox, "AudioQueueEnqueueBuffer")
		purego.RegisterLibFunc(&coreAudio.queueStart, audioToolbox, "AudioQueueStart")
		purego.RegisterLibFunc(&coreAudio.queueStop, audioToolbox, "AudioQueueStop")
		purego.RegisterLibFunc(&coreAudio.queueDispose, audioToolbox, "AudioQueueDispose")
		purego.RegisterLibFunc(&coreAudio.queueSet, audioToolbox, "AudioQueueSetProperty")
	})
	if coreAudioProblem != "" {
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, coreAudioProblem)
	}
	return &coreAudio, nil
}

func newAudioContext(value any) uintptr {
	id := audioNextContext.Add(1)
	audioContexts.Store(id, value)
	return id
}

func dropAudioContext(id uintptr) {
	audioContexts.Delete(id)
}

func audioInputCallback(userData, _ uintptr, buffer *audioQueueBuffer, _ uintptr, _ uint32, _ uintptr) {
	value, ok := audioContexts.Load(userData)
	if !ok {
		return
	}
	capture, ok := value.(*darwinCapture)
	if !ok || capture.closed.Load() || buffer == nil || buffer.ByteSize == 0 || buffer.Data == nil {
		return
	}
	capture.deliver(append([]byte(nil), unsafe.Slice((*byte)(buffer.Data), int(buffer.ByteSize))...))
	buffer.ByteSize = 0
	if !capture.closed.Load() {
		capture.api.queueQueue(capture.queue, uintptr(unsafe.Pointer(buffer)), 0, 0)
	}
}

func audioOutputCallback(userData, _ uintptr, buffer *audioQueueBuffer) {
	value, ok := audioContexts.Load(userData)
	if !ok || buffer == nil {
		return
	}
	playback, ok := value.(*darwinPlayback)
	if !ok || playback.closed.Load() {
		return
	}
	address := unsafe.Pointer(buffer)
	for index, pointer := range playback.buffers {
		if pointer == address {
			select {
			case playback.free <- index:
			default:
			}
			return
		}
	}
}

func audioFormat() audioStreamBasicDescription {
	return audioStreamBasicDescription{
		SampleRate:       SampleRate,
		FormatID:         audioFourCCLPCM,
		FormatFlags:      audioLinearPCMSigned | audioLinearPCMPacked,
		BytesPerPacket:   2,
		FramesPerPacket:  1,
		BytesPerFrame:    2,
		ChannelsPerFrame: 1,
		BitsPerChannel:   16,
	}
}

func enumerate() Devices {
	api, err := macAudio()
	if err != nil {
		return Devices{Problem: err.Error()}
	}
	address := audioPropertyAddress{Selector: audioDevicesSelector, Scope: audioPropertyGlobal, Element: audioPropertyMainElement}
	var size uint32
	if status := api.getDataSize(1, &address, 0, nil, &size); status != 0 || size < 4 {
		return Devices{Problem: fmt.Sprintf("无法读取 CoreAudio 设备列表: %d", status)}
	}
	ids := make([]uint32, size/4)
	if status := api.getData(1, &address, 0, nil, &size, unsafe.Pointer(&ids[0])); status != 0 {
		return Devices{Problem: fmt.Sprintf("无法读取 CoreAudio 设备列表: %d", status)}
	}
	defaultIn, _ := macDefaultDevice(api, audioDefaultInput)
	defaultOut, _ := macDefaultDevice(api, audioDefaultOutput)
	var devices Devices
	for _, id := range ids {
		input := macSupports(api, id, audioPropertyInput)
		output := macSupports(api, id, audioPropertyOutput)
		if !input && !output {
			continue
		}
		name := macDeviceName(api, id)
		if name == "" {
			name = "CoreAudio 设备 " + strconv.FormatUint(uint64(id), 10)
		}
		device := Device{ID: "coreaudio:" + strconv.FormatUint(uint64(id), 10), Name: name}
		if input {
			value := device
			value.Default = id == defaultIn
			devices.Microphones = append(devices.Microphones, value)
		}
		if output {
			value := device
			value.Default = id == defaultOut
			devices.Speakers = append(devices.Speakers, value)
		}
	}
	return devices
}

func macDefaultDevice(api *coreAudioAPI, selector uint32) (uint32, bool) {
	address := audioPropertyAddress{Selector: selector, Scope: audioPropertyGlobal, Element: audioPropertyMainElement}
	var id uint32
	size := uint32(4)
	if api.getData(1, &address, 0, nil, &size, unsafe.Pointer(&id)) != 0 {
		return 0, false
	}
	return id, id != 0
}

func macSupports(api *coreAudioAPI, id, scope uint32) bool {
	address := audioPropertyAddress{Selector: audioStreamsSelector, Scope: scope, Element: audioPropertyMainElement}
	var size uint32
	return api.getDataSize(id, &address, 0, nil, &size) == 0 && size > 0
}

func macDeviceName(api *coreAudioAPI, id uint32) string {
	address := audioPropertyAddress{Selector: audioDeviceNameSelector, Scope: audioPropertyGlobal, Element: audioPropertyMainElement}
	var reference uintptr
	size := uint32(unsafe.Sizeof(reference))
	if api.getData(id, &address, 0, nil, &size, unsafe.Pointer(&reference)) != 0 || reference == 0 {
		return ""
	}
	defer api.cfRelease(reference)
	var buffer [512]byte
	if api.cfGetString(reference, &buffer[0], int64(len(buffer)), 0x08000100) == 0 { // UTF-8
		return ""
	}
	return strings.TrimSpace(goString(&buffer[0]))
}

type darwinCapture struct {
	api       *coreAudioAPI
	queue     uintptr
	contextID uintptr
	buffers   []unsafe.Pointer
	data      chan []byte
	done      chan struct{}
	pending   []byte
	closed    atomic.Bool
	closeOnce sync.Once
}

func openCapture(id string) (Capture, error) {
	api, err := macAudio()
	if err != nil {
		return nil, err
	}
	device, err := parseCoreAudioDevice(id)
	if err != nil {
		return nil, err
	}
	capture := &darwinCapture{api: api, data: make(chan []byte, 16), done: make(chan struct{})}
	format := audioFormat()
	capture.contextID = newAudioContext(capture)
	status := api.queueNewIn(&format, inputCallback, capture.contextID, 0, 0, 0, &capture.queue)
	if status != 0 {
		dropAudioContext(capture.contextID)
		return nil, fmt.Errorf("%w: 无法创建麦克风队列: OSStatus %d", ErrDevice, status)
	}
	if err := setAudioQueueDevice(api, capture.queue, device); err != nil {
		capture.Close()
		return nil, err
	}
	for range 4 {
		var buffer unsafe.Pointer
		if status := api.queueAlloc(capture.queue, PCMFrameBytes, &buffer); status != 0 {
			capture.Close()
			return nil, fmt.Errorf("%w: 无法分配麦克风缓冲: OSStatus %d", ErrDevice, status)
		}
		capture.buffers = append(capture.buffers, buffer)
		if status := api.queueQueue(capture.queue, uintptr(buffer), 0, 0); status != 0 {
			capture.Close()
			return nil, fmt.Errorf("%w: 无法启动麦克风缓冲: OSStatus %d", ErrDevice, status)
		}
	}
	if status := api.queueStart(capture.queue, 0); status != 0 {
		capture.Close()
		return nil, fmt.Errorf("%w: 无法启动麦克风: OSStatus %d", ErrDevice, status)
	}
	return capture, nil
}

func (c *darwinCapture) deliver(pcm []byte) {
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

func (c *darwinCapture) Read(pcm []byte) (int, error) {
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

func (c *darwinCapture) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.done)
		if c.queue != 0 {
			c.api.queueStop(c.queue, 1)
			c.api.queueDispose(c.queue, 1)
			c.queue = 0
		}
		dropAudioContext(c.contextID)
	})
	return nil
}

type darwinPlayback struct {
	writeMu   sync.Mutex
	api       *coreAudioAPI
	queue     uintptr
	contextID uintptr
	buffers   []unsafe.Pointer
	free      chan int
	done      chan struct{}
	closed    atomic.Bool
	closeOnce sync.Once
}

func openPlayback(id string) (Playback, error) {
	api, err := macAudio()
	if err != nil {
		return nil, err
	}
	device, err := parseCoreAudioDevice(id)
	if err != nil {
		return nil, err
	}
	playback := &darwinPlayback{api: api, free: make(chan int, 4), done: make(chan struct{})}
	format := audioFormat()
	playback.contextID = newAudioContext(playback)
	status := api.queueNewOut(&format, outputCallback, playback.contextID, 0, 0, 0, &playback.queue)
	if status != 0 {
		dropAudioContext(playback.contextID)
		return nil, fmt.Errorf("%w: 无法创建扬声器队列: OSStatus %d", ErrDevice, status)
	}
	if err := setAudioQueueDevice(api, playback.queue, device); err != nil {
		playback.Close()
		return nil, err
	}
	for i := range 4 {
		var buffer unsafe.Pointer
		if status := api.queueAlloc(playback.queue, PCMFrameBytes, &buffer); status != 0 {
			playback.Close()
			return nil, fmt.Errorf("%w: 无法分配扬声器缓冲: OSStatus %d", ErrDevice, status)
		}
		playback.buffers = append(playback.buffers, buffer)
		playback.free <- i
	}
	if status := api.queueStart(playback.queue, 0); status != 0 {
		playback.Close()
		return nil, fmt.Errorf("%w: 无法启动扬声器: OSStatus %d", ErrDevice, status)
	}
	return playback, nil
}

func (p *darwinPlayback) Write(pcm []byte) (int, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
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
		buffer := (*audioQueueBuffer)(p.buffers[index])
		size := min(len(pcm), int(buffer.Capacity))
		if size == 0 || buffer.Data == nil {
			return written, fmt.Errorf("%w: 扬声器缓冲为空", ErrDevice)
		}
		copy(unsafe.Slice((*byte)(buffer.Data), size), pcm[:size])
		buffer.ByteSize = uint32(size)
		if status := p.api.queueQueue(p.queue, uintptr(p.buffers[index]), 0, 0); status != 0 {
			return written, fmt.Errorf("%w: 无法播放语音: OSStatus %d", ErrDevice, status)
		}
		pcm, written = pcm[size:], written+size
	}
	return written, nil
}

func (p *darwinPlayback) Close() error {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		close(p.done)
		p.writeMu.Lock()
		defer p.writeMu.Unlock()
		if p.queue != 0 {
			p.api.queueStop(p.queue, 1)
			p.api.queueDispose(p.queue, 1)
			p.queue = 0
		}
		dropAudioContext(p.contextID)
	})
	return nil
}

func parseCoreAudioDevice(id string) (uint32, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == "default" {
		return 0, nil
	}
	value, ok := strings.CutPrefix(id, "coreaudio:")
	if !ok {
		return 0, fmt.Errorf("%w: 未知的设备标识", ErrDevice)
	}
	device, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: 无效的设备标识", ErrDevice)
	}
	return uint32(device), nil
}

func setAudioQueueDevice(api *coreAudioAPI, queue uintptr, device uint32) error {
	if device == 0 {
		return nil
	}
	address := audioPropertyAddress{Selector: audioDeviceUIDSelector, Scope: audioPropertyGlobal, Element: audioPropertyMainElement}
	var uid uintptr
	size := uint32(unsafe.Sizeof(uid))
	if status := api.getData(device, &address, 0, nil, &size, unsafe.Pointer(&uid)); status != 0 || uid == 0 {
		return fmt.Errorf("%w: 无法读取音频设备标识: OSStatus %d", ErrDevice, status)
	}
	defer api.cfRelease(uid)
	if status := api.queueSet(queue, audioQueueDeviceProperty, unsafe.Pointer(&uid), uint32(unsafe.Sizeof(uid))); status != 0 {
		return fmt.Errorf("%w: 无法选择音频设备: OSStatus %d", ErrDevice, status)
	}
	return nil
}

var _ = audioLinearPCMFloatFlag
var _ = audioInputCallback
var _ = audioOutputCallback
