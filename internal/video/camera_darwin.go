//go:build darwin && cgo

package video

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AVFoundation -framework CoreMedia -framework CoreVideo -framework Foundation

#import <AVFoundation/AVFoundation.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	char *id;
	char *name;
} goim_video_device;

static int goim_video_enumerate(goim_video_device **out, int *count) {
	@autoreleasepool {
		NSMutableArray<AVCaptureDeviceType> *types = [NSMutableArray arrayWithObject:AVCaptureDeviceTypeBuiltInWideAngleCamera];
#if defined(__MAC_OS_X_VERSION_MAX_ALLOWED) && __MAC_OS_X_VERSION_MAX_ALLOWED >= 140000
		if (@available(macOS 14.0, *)) {
			[types addObject:AVCaptureDeviceTypeExternal];
		}
#else
		[types addObject:AVCaptureDeviceTypeExternalUnknown];
#endif
		AVCaptureDeviceDiscoverySession *session = [AVCaptureDeviceDiscoverySession
			discoverySessionWithDeviceTypes:types mediaType:AVMediaTypeVideo position:AVCaptureDevicePositionUnspecified];
		NSArray<AVCaptureDevice *> *devices = session.devices;
		int n = (int)devices.count;
		*count = n;
		if (n == 0) {
			*out = NULL;
			return 0;
		}
		goim_video_device *arr = (goim_video_device *)calloc((size_t)n, sizeof(goim_video_device));
		for (int i = 0; i < n; i++) {
			AVCaptureDevice *dev = devices[i];
			const char *uid = dev.uniqueID.UTF8String ?: "";
			const char *name = dev.localizedName.UTF8String ?: "Camera";
			arr[i].id = strdup(uid);
			arr[i].name = strdup(name);
		}
		*out = arr;
		return 0;
	}
}

static void goim_video_free_devices(goim_video_device *arr, int count) {
	if (!arr) return;
	for (int i = 0; i < count; i++) {
		free(arr[i].id);
		free(arr[i].name);
	}
	free(arr);
}

@interface GoIMVideoCapture : NSObject <AVCaptureVideoDataOutputSampleBufferDelegate> {
	AVCaptureSession *_session;
	dispatch_queue_t _queue;
	NSLock *_lock;
	unsigned char *_bgra;
	int _width;
	int _height;
	uint64_t _seq;
}
- (instancetype)initWithDeviceID:(const char *)deviceID errorCode:(int *)errCode;
- (int)copyFrame:(unsigned char **)outBuf width:(int *)outW height:(int *)outH afterSeq:(uint64_t *)seq;
- (void)stopCapture;
@end

@implementation GoIMVideoCapture

- (instancetype)initWithDeviceID:(const char *)deviceID errorCode:(int *)errCode {
	self = [super init];
	if (!self) {
		*errCode = -1;
		return nil;
	}
	_lock = [[NSLock alloc] init];
	AVCaptureDevice *device = nil;
	if (deviceID != NULL && deviceID[0] != '\0') {
		NSString *uid = [NSString stringWithUTF8String:deviceID];
		device = [AVCaptureDevice deviceWithUniqueID:uid];
	}
	if (device == nil) {
		device = [AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeVideo];
	}
	if (device == nil) {
		*errCode = -2;
		return nil;
	}
	NSError *error = nil;
	AVCaptureDeviceInput *input = [AVCaptureDeviceInput deviceInputWithDevice:device error:&error];
	if (input == nil || error != nil) {
		*errCode = -3;
		return nil;
	}
	_session = [[AVCaptureSession alloc] init];
	if ([_session canSetSessionPreset:AVCaptureSessionPreset640x480]) {
		_session.sessionPreset = AVCaptureSessionPreset640x480;
	}
	if (![_session canAddInput:input]) {
		*errCode = -3;
		return nil;
	}
	[_session addInput:input];

	AVCaptureVideoDataOutput *output = [[AVCaptureVideoDataOutput alloc] init];
	output.videoSettings = @{ (id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_32BGRA) };
	output.alwaysDiscardsLateVideoFrames = YES;
	_queue = dispatch_queue_create("dev.goim.video.capture", DISPATCH_QUEUE_SERIAL);
	[output setSampleBufferDelegate:self queue:_queue];
	if (![_session canAddOutput:output]) {
		*errCode = -3;
		return nil;
	}
	[_session addOutput:output];
	[_session startRunning];
	*errCode = 0;
	return self;
}

- (void)captureOutput:(AVCaptureOutput *)output
didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer
       fromConnection:(AVCaptureConnection *)connection {
	CVImageBufferRef pixelBuffer = CMSampleBufferGetImageBuffer(sampleBuffer);
	if (pixelBuffer == NULL) return;
	CVPixelBufferLockBaseAddress(pixelBuffer, kCVPixelBufferLock_ReadOnly);
	int w = (int)CVPixelBufferGetWidth(pixelBuffer);
	int h = (int)CVPixelBufferGetHeight(pixelBuffer);
	size_t stride = CVPixelBufferGetBytesPerRow(pixelBuffer);
	unsigned char *base = (unsigned char *)CVPixelBufferGetBaseAddress(pixelBuffer);
	if (base != NULL && w > 0 && h > 0) {
		size_t rowBytes = (size_t)w * 4;
		[_lock lock];
		if (_width != w || _height != h || _bgra == NULL) {
			free(_bgra);
			_bgra = (unsigned char *)malloc(rowBytes * (size_t)h);
			_width = w;
			_height = h;
		}
		if (_bgra != NULL) {
			for (int y = 0; y < h; y++) {
				memcpy(_bgra + (size_t)y * rowBytes, base + (size_t)y * stride, rowBytes);
			}
			_seq++;
		}
		[_lock unlock];
	}
	CVPixelBufferUnlockBaseAddress(pixelBuffer, kCVPixelBufferLock_ReadOnly);
}

- (int)copyFrame:(unsigned char **)outBuf width:(int *)outW height:(int *)outH afterSeq:(uint64_t *)seq {
	[_lock lock];
	if (_bgra == NULL || _seq == *seq || _width <= 0 || _height <= 0) {
		[_lock unlock];
		return 0;
	}
	size_t total = (size_t)_width * (size_t)_height * 4;
	unsigned char *copy = (unsigned char *)malloc(total);
	if (copy != NULL) {
		memcpy(copy, _bgra, total);
	}
	*outBuf = copy;
	*outW = _width;
	*outH = _height;
	*seq = _seq;
	[_lock unlock];
	return copy != NULL ? 1 : 0;
}

- (void)stopCapture {
	if (_session != nil) {
		[_session stopRunning];
		_session = nil;
	}
	[_lock lock];
	free(_bgra);
	_bgra = NULL;
	[_lock unlock];
}

@end

static void *goim_video_open(const char *deviceID, int *errCode) {
	@autoreleasepool {
		GoIMVideoCapture *cap = [[GoIMVideoCapture alloc] initWithDeviceID:deviceID errorCode:errCode];
		if (!cap) return NULL;
		return (__bridge_retained void *)cap;
	}
}

static int goim_video_read(void *handle, unsigned char **outBuf, int *outW, int *outH, uint64_t *seq) {
	@autoreleasepool {
		GoIMVideoCapture *cap = (__bridge GoIMVideoCapture *)handle;
		return [cap copyFrame:outBuf width:outW height:outH afterSeq:seq];
	}
}

static void goim_video_close(void *handle) {
	@autoreleasepool {
		if (!handle) return;
		GoIMVideoCapture *cap = (__bridge_transfer GoIMVideoCapture *)handle;
		[cap stopCapture];
	}
}
*/
import "C"

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

func enumerate() ([]Device, error) {
	var raw *C.goim_video_device
	var count C.int
	C.goim_video_enumerate(&raw, &count)
	defer C.goim_video_free_devices(raw, count)
	if count <= 0 || raw == nil {
		return nil, nil
	}
	slice := unsafe.Slice(raw, int(count))
	devices := make([]Device, 0, len(slice))
	for i, d := range slice {
		devices = append(devices, Device{
			ID:      C.GoString(d.id),
			Name:    C.GoString(d.name),
			Default: i == 0,
		})
	}
	return devices, nil
}

type darwinCapture struct {
	handle    unsafe.Pointer
	seq       C.uint64_t
	closeOnce sync.Once
	closed    atomic.Bool
}

func openCapture(deviceID string) (Capture, error) {
	var cID *C.char
	if deviceID != "" {
		cID = C.CString(deviceID)
		defer C.free(unsafe.Pointer(cID))
	}
	var errCode C.int
	handle := C.goim_video_open(cID, &errCode)
	if handle == nil {
		if errCode == -2 {
			return nil, ErrNoDevice
		}
		return nil, ErrDevice
	}
	return &darwinCapture{handle: handle}, nil
}

func (c *darwinCapture) ReadFrame(ctx context.Context) (Frame, error) {
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		if c.closed.Load() {
			return Frame{}, ErrClosed
		}
		var buf *C.uchar
		var w, h C.int
		if C.goim_video_read(c.handle, &buf, &w, &h, &c.seq) != 0 && buf != nil {
			n := int(w) * int(h) * 4
			bgra := C.GoBytes(unsafe.Pointer(buf), C.int(n))
			C.free(unsafe.Pointer(buf))
			return Frame{Width: int(w), Height: int(h), BGRA: bgra}, nil
		}
		select {
		case <-ctx.Done():
			return Frame{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *darwinCapture) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		C.goim_video_close(c.handle)
		c.handle = nil
	})
	return nil
}
