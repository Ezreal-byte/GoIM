package systemauth

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	roInitMultithreaded = 1
	rpcChangedMode      = 0x80010106

	userConsentVerifierAvailable   = 0
	userConsentVerifierDeviceError = 1

	userConsentVerified          = 0
	userConsentDeviceNotPresent  = 1
	userConsentNotConfigured     = 2
	userConsentDisabledByPolicy  = 3
	userConsentDeviceBusy        = 4
	userConsentRetriesExhausted  = 5
	userConsentCanceled          = 6
	asyncStatusCompleted         = 1
	asyncStatusCanceled          = 2
	asyncStatusError             = 3
	comMethodQueryInterface      = 0
	comMethodRelease             = 2
	comMethodCheckAvailability   = 6
	comMethodRequestVerification = 7
	comMethodAsyncStatus         = 7
	comMethodAsyncErrorCode      = 8
	comMethodAsyncResults        = 8
	comMethodRequestForWindow    = 6
	verificationPoll             = 25 * time.Millisecond
	runtimeClassUserConsent      = "Windows.Security.Credentials.UI.UserConsentVerifier"
)

var (
	combase = windows.NewLazySystemDLL("combase.dll")
	user32  = windows.NewLazySystemDLL("user32.dll")

	roInitialize           = combase.NewProc("RoInitialize")
	roUninitialize         = combase.NewProc("RoUninitialize")
	roGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	windowsCreateString    = combase.NewProc("WindowsCreateString")
	windowsDeleteString    = combase.NewProc("WindowsDeleteString")

	getForegroundWindow      = user32.NewProc("GetForegroundWindow")
	getWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	enumWindows              = user32.NewProc("EnumWindows")
	isWindowVisible          = user32.NewProc("IsWindowVisible")
	getWindowRect            = user32.NewProc("GetWindowRect")

	iidUserConsentVerifierInterop = windows.GUID{
		Data1: 0x39e050c3, Data2: 0x4e74, Data3: 0x441a,
		Data4: [8]byte{0x8d, 0xc0, 0xb8, 0x11, 0x04, 0xdf, 0x94, 0x9c},
	}
	iidUserConsentVerifierStatics = windows.GUID{
		Data1: 0xaf4f3f91, Data2: 0x564c, Data3: 0x4ddc,
		Data4: [8]byte{0xb8, 0xb5, 0x97, 0x34, 0x47, 0x62, 0x7c, 0x65},
	}
	iidAsyncUserConsentResult = windows.GUID{
		Data1: 0xfd596ffd, Data2: 0x2318, Data3: 0x558f,
		Data4: [8]byte{0x9d, 0xbe, 0xd2, 0x1d, 0xf4, 0x37, 0x64, 0xa5},
	}
	iidAsyncInfo = windows.GUID{
		Data1: 0x00000036, Data2: 0x0000, Data3: 0x0000,
		Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
)

type nativeRect struct {
	Left, Top, Right, Bottom int32
}

func description() string {
	return "Windows Hello（PIN、指纹或人脸）"
}

func verify(reason string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	initialized, err := initializeWinRT()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if initialized {
		defer roUninitialize.Call()
	}
	owner, err := foregroundOwnerWindow()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return requestVerification(owner, reason)
}

func requestVerification(owner uintptr, reason string) error {
	err := requestVerificationForWindow(owner, reason)
	if !errors.Is(err, ErrUnavailable) {
		return err
	}
	return requestVerificationWithoutWindow(reason)
}

// RequestVerificationForWindowAsync is only available on Windows 11. Older
// builds get a best-effort fallback to the original WinRT method, which may
// reject Win32 desktop callers.
func requestVerificationForWindow(owner uintptr, reason string) error {
	factory, err := activationFactory(runtimeClassUserConsent, &iidUserConsentVerifierInterop)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer releaseCOM(factory)

	message, err := newHString(reason)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer windowsDeleteString.Call(message)

	var operation uintptr
	hr := callCOM(factory, comMethodRequestForWindow, owner, message,
		uintptr(unsafe.Pointer(&iidAsyncUserConsentResult)), uintptr(unsafe.Pointer(&operation)))
	runtime.KeepAlive(&iidAsyncUserConsentResult)
	if failed(hr) || operation == 0 {
		return fmt.Errorf("%w: RequestVerificationForWindowAsync failed (HRESULT 0x%08X)", ErrUnavailable, uint32(hr))
	}
	operationObject := comPointer(operation)
	defer releaseCOM(operationObject)
	return verificationResult(operationObject)
}

func requestVerificationWithoutWindow(reason string) error {
	factory, err := activationFactory(runtimeClassUserConsent, &iidUserConsentVerifierStatics)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer releaseCOM(factory)

	message, err := newHString(reason)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer windowsDeleteString.Call(message)

	var operation uintptr
	hr := callCOM(factory, comMethodRequestVerification, message,
		uintptr(unsafe.Pointer(&iidAsyncUserConsentResult)), uintptr(unsafe.Pointer(&operation)))
	runtime.KeepAlive(&iidAsyncUserConsentResult)
	if failed(hr) || operation == 0 {
		return fmt.Errorf("%w: RequestVerificationAsync failed (HRESULT 0x%08X)", ErrUnavailable, uint32(hr))
	}
	operationObject := comPointer(operation)
	defer releaseCOM(operationObject)
	return verificationResult(operationObject)
}

func verificationResult(operation unsafe.Pointer) error {
	var result int32
	if err := waitAsyncResult(operation, unsafe.Pointer(&result)); err != nil {
		return err
	}
	switch result {
	case userConsentVerified:
		return nil
	case userConsentCanceled:
		return ErrCanceled
	case userConsentDeviceNotPresent, userConsentNotConfigured:
		return ErrNotConfigured
	case userConsentDisabledByPolicy:
		return ErrDisabledByPolicy
	case userConsentDeviceBusy:
		return ErrBusy
	case userConsentRetriesExhausted:
		return ErrRetriesExhausted
	default:
		return ErrFailed
	}
}

// queryAvailability lets a diagnostic or opt-in native test ask Windows
// whether Hello is ready without showing a prompt.
func queryAvailability() (int32, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	initialized, err := initializeWinRT()
	if err != nil {
		return userConsentVerifierDeviceError, err
	}
	if initialized {
		defer roUninitialize.Call()
	}
	factory, err := activationFactory(runtimeClassUserConsent, &iidUserConsentVerifierStatics)
	if err != nil {
		return userConsentVerifierDeviceError, err
	}
	defer releaseCOM(factory)

	var operation uintptr
	hr := callCOM(factory, comMethodCheckAvailability, uintptr(unsafe.Pointer(&operation)))
	if failed(hr) || operation == 0 {
		return userConsentVerifierDeviceError, fmt.Errorf("CheckAvailabilityAsync failed (HRESULT 0x%08X)", uint32(hr))
	}
	operationObject := comPointer(operation)
	defer releaseCOM(operationObject)

	var availability int32
	if err := waitAsyncResult(operationObject, unsafe.Pointer(&availability)); err != nil {
		return userConsentVerifierDeviceError, err
	}
	return availability, nil
}

func initializeWinRT() (bool, error) {
	hr, _, _ := roInitialize.Call(roInitMultithreaded)
	if failed(hr) && uint32(hr) != rpcChangedMode {
		return false, fmt.Errorf("RoInitialize failed (HRESULT 0x%08X)", uint32(hr))
	}
	return !failed(hr), nil
}

func activationFactory(class string, iid *windows.GUID) (unsafe.Pointer, error) {
	name, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer windowsDeleteString.Call(name)

	var factory uintptr
	hr, _, _ := roGetActivationFactory.Call(name, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&factory)))
	runtime.KeepAlive(iid)
	if failed(hr) || factory == 0 {
		return nil, fmt.Errorf("RoGetActivationFactory(%s) failed (HRESULT 0x%08X)", class, uint32(hr))
	}
	return comPointer(factory), nil
}

func newHString(value string) (uintptr, error) {
	units, err := windows.UTF16FromString(value)
	if err != nil {
		return 0, err
	}
	var result uintptr
	hr, _, _ := windowsCreateString.Call(
		uintptr(unsafe.Pointer(&units[0])), uintptr(len(units)-1), uintptr(unsafe.Pointer(&result)))
	runtime.KeepAlive(units)
	if failed(hr) || result == 0 {
		return 0, fmt.Errorf("WindowsCreateString failed (HRESULT 0x%08X)", uint32(hr))
	}
	return result, nil
}

func waitAsyncResult(operation unsafe.Pointer, result unsafe.Pointer) error {
	var info uintptr
	hr := callCOM(operation, comMethodQueryInterface,
		uintptr(unsafe.Pointer(&iidAsyncInfo)), uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(&iidAsyncInfo)
	if failed(hr) || info == 0 {
		return fmt.Errorf("%w: IAsyncInfo query failed (HRESULT 0x%08X)", ErrUnavailable, uint32(hr))
	}
	infoObject := comPointer(info)
	defer releaseCOM(infoObject)

	for {
		var status int32
		hr = callCOM(infoObject, comMethodAsyncStatus, uintptr(unsafe.Pointer(&status)))
		if failed(hr) {
			return fmt.Errorf("%w: async status failed (HRESULT 0x%08X)", ErrUnavailable, uint32(hr))
		}
		switch status {
		case 0:
			time.Sleep(verificationPoll)
		case asyncStatusCompleted:
			hr = callCOM(operation, comMethodAsyncResults, uintptr(result))
			if failed(hr) {
				return fmt.Errorf("%w: async result failed (HRESULT 0x%08X)", ErrUnavailable, uint32(hr))
			}
			return nil
		case asyncStatusCanceled:
			return ErrCanceled
		case asyncStatusError:
			var code int32
			_ = callCOM(infoObject, comMethodAsyncErrorCode, uintptr(unsafe.Pointer(&code)))
			return fmt.Errorf("%w (HRESULT 0x%08X)", ErrUnavailable, uint32(code))
		default:
			return fmt.Errorf("%w: unknown async status %d", ErrUnavailable, status)
		}
	}
}

func releaseCOM(object unsafe.Pointer) {
	if object != nil {
		callCOM(object, comMethodRelease)
	}
}

func comPointer(value uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&value))
}

//go:uintptrescapes
func callCOM(object unsafe.Pointer, method uintptr, args ...uintptr) uintptr {
	var values [16]uintptr
	if len(args) >= len(values) {
		panic("systemauth: COM call exceeds argument limit")
	}
	values[0] = uintptr(object)
	copy(values[1:], args)
	table := *(*unsafe.Pointer)(object)
	fn := *(*uintptr)(unsafe.Add(table, method*unsafe.Sizeof(uintptr(0))))
	result, _, _ := syscall.SyscallN(fn, values[:len(args)+1]...)
	runtime.KeepAlive(object)
	runtime.KeepAlive(args)
	return result
}

func failed(hr uintptr) bool {
	return int32(uint32(hr)) < 0
}

func foregroundOwnerWindow() (uintptr, error) {
	process := windows.GetCurrentProcessId()
	foreground, _, _ := getForegroundWindow.Call()
	if foreground != 0 && windowProcess(foreground) == process {
		return foreground, nil
	}

	var (
		best      uintptr
		bestArea  int64
		enumError error
		once      sync.Once
	)
	callback := windows.NewCallback(func(window, _ uintptr) uintptr {
		visible, _, _ := isWindowVisible.Call(window)
		if visible == 0 || windowProcess(window) != process {
			return 1
		}
		var rect nativeRect
		ok, _, callErr := getWindowRect.Call(window, uintptr(unsafe.Pointer(&rect)))
		if ok == 0 {
			once.Do(func() { enumError = callErr })
			return 1
		}
		area := int64(rect.Right-rect.Left) * int64(rect.Bottom-rect.Top)
		if area > bestArea {
			best, bestArea = window, area
		}
		return 1
	})
	ok, _, callErr := enumWindows.Call(callback, 0)
	runtime.KeepAlive(callback)
	if ok == 0 {
		return 0, fmt.Errorf("EnumWindows failed: %w", callErr)
	}
	if best == 0 {
		if enumError != nil {
			return 0, enumError
		}
		return 0, fmt.Errorf("GoIM has no visible top-level window")
	}
	return best, nil
}

func windowProcess(window uintptr) uint32 {
	var process uint32
	getWindowThreadProcessID.Call(window, uintptr(unsafe.Pointer(&process)))
	return process
}
