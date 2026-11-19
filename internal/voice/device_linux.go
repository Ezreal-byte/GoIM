//go:build linux && (amd64 || arm64)

package voice

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	alsaPlayback      = 0
	alsaCapture       = 1
	alsaAccessRWInter = 3
	alsaFormatS16LE   = 2
	alsaEAGAIN        = -11
	alsaRetries       = 4
)

type alsaAPI struct {
	open         func(pcm *uintptr, name *byte, stream, mode int32) int32
	close        func(pcm uintptr) int32
	drop         func(pcm uintptr) int32
	prepare      func(pcm uintptr) int32
	recover      func(pcm uintptr, err, silent int32) int32
	readi        func(pcm uintptr, buf unsafe.Pointer, frames uintptr) int64
	writei       func(pcm uintptr, buf unsafe.Pointer, frames uintptr) int64
	nonblock     func(pcm uintptr, nonblock int32) int32
	strerror     func(err int32) *byte
	paramsMalloc func(params *uintptr) int32
	paramsFree   func(params uintptr)
	paramsAny    func(pcm, params uintptr) int32
	setAccess    func(pcm, params uintptr, access int32) int32
	setFormat    func(pcm, params uintptr, format int32) int32
	setChannels  func(pcm, params uintptr, channels uint32) int32
	setRate      func(pcm, params uintptr, rate uint32, dir int32) int32
	setRateNear  func(pcm, params uintptr, rate *uint32, dir *int32) int32
	periodNear   func(pcm, params uintptr, frames *uint64, dir *int32) int32
	bufferNear   func(pcm, params uintptr, frames *uint64) int32
	params       func(pcm, params uintptr) int32
	nameHint     func(card int32, iface *byte, hints *unsafe.Pointer) int32
	nameGetHint  func(hint uintptr, id *byte) *byte
	nameFreeHint func(hints uintptr) int32
	free         func(pointer uintptr)
}

func openALSA() (*alsaAPI, uintptr, error) {
	var (
		library uintptr
		err     error
	)
	for _, name := range []string{"libasound.so.2", "libasound.so"} {
		library, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err == nil {
			break
		}
	}
	if library == 0 {
		return nil, 0, fmt.Errorf("%w: 无法加载 ALSA: %v", ErrUnsupported, err)
	}
	api := &alsaAPI{}
	purego.RegisterLibFunc(&api.open, library, "snd_pcm_open")
	purego.RegisterLibFunc(&api.close, library, "snd_pcm_close")
	purego.RegisterLibFunc(&api.drop, library, "snd_pcm_drop")
	purego.RegisterLibFunc(&api.prepare, library, "snd_pcm_prepare")
	purego.RegisterLibFunc(&api.recover, library, "snd_pcm_recover")
	purego.RegisterLibFunc(&api.readi, library, "snd_pcm_readi")
	purego.RegisterLibFunc(&api.writei, library, "snd_pcm_writei")
	purego.RegisterLibFunc(&api.nonblock, library, "snd_pcm_nonblock")
	purego.RegisterLibFunc(&api.strerror, library, "snd_strerror")
	purego.RegisterLibFunc(&api.paramsMalloc, library, "snd_pcm_hw_params_malloc")
	purego.RegisterLibFunc(&api.paramsFree, library, "snd_pcm_hw_params_free")
	purego.RegisterLibFunc(&api.paramsAny, library, "snd_pcm_hw_params_any")
	purego.RegisterLibFunc(&api.setAccess, library, "snd_pcm_hw_params_set_access")
	purego.RegisterLibFunc(&api.setFormat, library, "snd_pcm_hw_params_set_format")
	purego.RegisterLibFunc(&api.setChannels, library, "snd_pcm_hw_params_set_channels")
	purego.RegisterLibFunc(&api.setRate, library, "snd_pcm_hw_params_set_rate")
	purego.RegisterLibFunc(&api.setRateNear, library, "snd_pcm_hw_params_set_rate_near")
	purego.RegisterLibFunc(&api.periodNear, library, "snd_pcm_hw_params_set_period_size_near")
	purego.RegisterLibFunc(&api.bufferNear, library, "snd_pcm_hw_params_set_buffer_size_near")
	purego.RegisterLibFunc(&api.params, library, "snd_pcm_hw_params")
	purego.RegisterLibFunc(&api.nameHint, library, "snd_device_name_hint")
	purego.RegisterLibFunc(&api.nameGetHint, library, "snd_device_name_get_hint")
	purego.RegisterLibFunc(&api.nameFreeHint, library, "snd_device_name_free_hint")
	if libc, libcErr := purego.Dlopen("libc.so.6", purego.RTLD_NOW|purego.RTLD_LOCAL); libcErr == nil {
		purego.RegisterLibFunc(&api.free, libc, "free")
	}
	return api, library, nil
}

type alsaStream struct {
	api     *alsaAPI
	library uintptr
	pcm     uintptr
	closed  atomic.Bool
	mu      sync.Mutex
}

func enumerate() Devices {
	api, library, err := openALSA()
	if err != nil {
		return Devices{Problem: err.Error()}
	}
	defer purego.Dlclose(library)
	iface := []byte("pcm\x00")
	var hints unsafe.Pointer
	if status := api.nameHint(-1, &iface[0], &hints); status < 0 || hints == nil {
		return Devices{Problem: "无法枚举 ALSA 音频设备: " + alsaError(api, status)}
	}
	defer api.nameFreeHint(uintptr(hints))
	seenIn, seenOut := map[string]bool{}, map[string]bool{}
	var devices Devices
	for index := 0; ; index++ {
		hint := *(*uintptr)(unsafe.Add(hints, index*int(unsafe.Sizeof(uintptr(0)))))
		if hint == 0 {
			break
		}
		name, description, io := alsaHint(api, hint)
		if name == "" || name == "default" || name == "null" {
			continue
		}
		label := name
		if first, _, ok := strings.Cut(description, "\n"); ok {
			description = first
		}
		description = strings.TrimSpace(description)
		if description != "" && !strings.EqualFold(description, name) {
			label += " — " + description
		}
		if (io == "" || io == "Input") && !seenIn[name] {
			seenIn[name] = true
			devices.Microphones = append(devices.Microphones, Device{ID: name, Name: label})
		}
		if (io == "" || io == "Output") && !seenOut[name] {
			seenOut[name] = true
			devices.Speakers = append(devices.Speakers, Device{ID: name, Name: label})
		}
	}
	return devices
}

func alsaHint(api *alsaAPI, hint uintptr) (name, description, io string) {
	get := func(key string) (string, uintptr) {
		raw := []byte(key + "\x00")
		pointer := api.nameGetHint(hint, &raw[0])
		if pointer == nil {
			return "", 0
		}
		return goString(pointer), uintptr(unsafe.Pointer(pointer))
	}
	name, namePointer := get("NAME")
	description, descriptionPointer := get("DESC")
	io, ioPointer := get("IOID")
	if api.free != nil {
		for _, pointer := range []uintptr{namePointer, descriptionPointer, ioPointer} {
			if pointer != 0 {
				api.free(pointer)
			}
		}
	}
	return name, description, io
}

func openCapture(id string) (Capture, error) {
	return openALSAStream(id, alsaCapture)
}

func openPlayback(id string) (Playback, error) {
	return openALSAStream(id, alsaPlayback)
}

func openALSAStream(id string, stream int32) (*alsaStream, error) {
	api, library, err := openALSA()
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(id)
	if name == "" || name == "default" {
		name = "default"
	}
	pcmName := append([]byte(name), 0)
	var pcm uintptr
	if status := api.open(&pcm, &pcmName[0], stream, 0); status < 0 {
		purego.Dlclose(library)
		return nil, fmt.Errorf("%w: 打开音频设备失败: %s", ErrDevice, alsaError(api, status))
	}
	if err := configureALSA(api, pcm); err != nil {
		api.close(pcm)
		purego.Dlclose(library)
		return nil, err
	}
	if status := api.nonblock(pcm, 1); status < 0 {
		api.close(pcm)
		purego.Dlclose(library)
		return nil, fmt.Errorf("%w: 音频设备不支持非阻塞模式: %s", ErrDevice, alsaError(api, status))
	}
	return &alsaStream{api: api, library: library, pcm: pcm}, nil
}

func configureALSA(api *alsaAPI, pcm uintptr) error {
	var params uintptr
	if status := api.paramsMalloc(&params); status < 0 {
		return fmt.Errorf("%w: 分配 ALSA 参数失败: %s", ErrDevice, alsaError(api, status))
	}
	defer api.paramsFree(params)
	fail := func(step string, status int32) error {
		return fmt.Errorf("%w: %s: %s", ErrDevice, step, alsaError(api, status))
	}
	if status := api.paramsAny(pcm, params); status < 0 {
		return fail("读取设备参数", status)
	}
	if status := api.setAccess(pcm, params, alsaAccessRWInter); status < 0 {
		return fail("设置交错访问模式", status)
	}
	if status := api.setFormat(pcm, params, alsaFormatS16LE); status < 0 {
		return fail("设置 16 位 PCM", status)
	}
	if status := api.setChannels(pcm, params, 1); status < 0 {
		return fail("设置单声道", status)
	}
	if status := api.setRate(pcm, params, SampleRate, 0); status < 0 {
		rate := uint32(SampleRate)
		var dir int32
		if near := api.setRateNear(pcm, params, &rate, &dir); near < 0 || rate != SampleRate {
			return fail("设置 16 kHz 采样率", status)
		}
	}
	// Favor 20 ms periods like the native Windows and CoreAudio backends. These
	// are best-effort: some ALSA plugins reject small buffer requests, and their
	// default remains preferable to failing an otherwise usable device.
	period := uint64(SampleRate * FrameMillis / 1000)
	var periodDir int32
	_ = api.periodNear(pcm, params, &period, &periodDir)
	buffer := period * 4
	_ = api.bufferNear(pcm, params, &buffer)
	if status := api.params(pcm, params); status < 0 {
		return fail("应用设备参数", status)
	}
	if status := api.prepare(pcm); status < 0 {
		return fail("准备设备", status)
	}
	return nil
}

func (s *alsaStream) Read(pcm []byte) (int, error) {
	frames := len(pcm) / 2
	offset := 0
	for offset < frames {
		if s.closed.Load() {
			return offset * 2, ErrClosed
		}
		var read int64
		s.mu.Lock()
		if s.pcm == 0 {
			s.mu.Unlock()
			return offset * 2, ErrClosed
		}
		read = s.api.readi(s.pcm, unsafe.Pointer(&pcm[offset*2]), uintptr(frames-offset))
		s.mu.Unlock()
		switch {
		case read > 0:
			offset += int(read)
		case read == alsaEAGAIN:
			time.Sleep(2 * time.Millisecond)
		case read == 0:
			time.Sleep(time.Millisecond)
		default:
			if err := s.recover(int32(read)); err != nil {
				return offset * 2, err
			}
		}
	}
	return len(pcm), nil
}

func (s *alsaStream) Write(pcm []byte) (int, error) {
	frames := len(pcm) / 2
	offset := 0
	for offset < frames {
		if s.closed.Load() {
			return offset * 2, ErrClosed
		}
		var written int64
		s.mu.Lock()
		if s.pcm == 0 {
			s.mu.Unlock()
			return offset * 2, ErrClosed
		}
		written = s.api.writei(s.pcm, unsafe.Pointer(&pcm[offset*2]), uintptr(frames-offset))
		s.mu.Unlock()
		switch {
		case written > 0:
			offset += int(written)
		case written == alsaEAGAIN:
			time.Sleep(2 * time.Millisecond)
		case written == 0:
			time.Sleep(time.Millisecond)
		default:
			if err := s.recover(int32(written)); err != nil {
				return offset * 2, err
			}
		}
	}
	return len(pcm), nil
}

func (s *alsaStream) recover(status int32) error {
	for range alsaRetries {
		s.mu.Lock()
		if s.pcm == 0 {
			s.mu.Unlock()
			return ErrClosed
		}
		recovered := s.api.recover(s.pcm, status, 1)
		s.mu.Unlock()
		if recovered >= 0 {
			return nil
		}
		status = recovered
	}
	return fmt.Errorf("%w: ALSA 设备无法恢复: %s", ErrDevice, alsaError(s.api, status))
}

func (s *alsaStream) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	s.mu.Lock()
	if s.pcm != 0 {
		s.api.drop(s.pcm)
		s.api.close(s.pcm)
		s.pcm = 0
	}
	s.mu.Unlock()
	if s.library != 0 {
		purego.Dlclose(s.library)
		s.library = 0
	}
	return nil
}

func alsaError(api *alsaAPI, status int32) string {
	if api != nil && api.strerror != nil {
		if message := api.strerror(status); message != nil {
			return goString(message)
		}
	}
	return strconv.Itoa(int(status))
}
