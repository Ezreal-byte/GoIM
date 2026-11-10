//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation -framework Foundation -framework ScreenCaptureKit
#include <CoreGraphics/CoreGraphics.h>
#include <ScreenCaptureKit/ScreenCaptureKit.h>
#include <dispatch/dispatch.h>
#include <dlfcn.h>
#include <math.h>

static size_t snip_windows(CGRect *rects, size_t cap) {
    CFArrayRef list = CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID);
    if (!list) return 0;
    size_t count = 0;
    for (CFIndex i = 0; i < CFArrayGetCount(list) && count < cap; i++) {
        CFDictionaryRef info = (CFDictionaryRef)CFArrayGetValueAtIndex(list, i);
        CFNumberRef layer = (CFNumberRef)CFDictionaryGetValue(info, kCGWindowLayer);
        int level = 0;
        if (!layer || !CFNumberGetValue(layer, kCFNumberIntType, &level) || level != 0) continue;
        CFDictionaryRef value = (CFDictionaryRef)CFDictionaryGetValue(info, kCGWindowBounds);
        CGRect rect;
        if (value && CGRectMakeWithDictionaryRepresentation(value, &rect) &&
            rect.size.width >= 32 && rect.size.height >= 24) rects[count++] = rect;
    }
    CFRelease(list);
    return count;
}

static int snip_cursor(CGPoint *point) {
    CGEventRef event = CGEventCreate(NULL);
    if (!event) return 0;
    *point = CGEventGetLocation(event);
    CFRelease(event);
    return 1;
}

static CGImageRef snip_capture_display(CGDirectDisplayID id) {
    if (@available(macOS 14.0, *)) {
        dispatch_semaphore_t ready = dispatch_semaphore_create(0);
        __block CGImageRef picture = NULL;
        [SCShareableContent getShareableContentWithCompletionHandler:^(SCShareableContent *content, NSError *error) {
            SCDisplay *display = nil;
            for (SCDisplay *candidate in content.displays) {
                if (candidate.displayID == id) { display = candidate; break; }
            }
            if (error || !display) { dispatch_semaphore_signal(ready); return; }
            SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:display excludingWindows:@[]];
            SCStreamConfiguration *config = [[SCStreamConfiguration alloc] init];
            CGRect bounds = CGDisplayBounds(id);
            CGFloat scale = filter.pointPixelScale;
            config.width = llround(bounds.size.width * scale);
            config.height = llround(bounds.size.height * scale);
            config.showsCursor = NO;
            [SCScreenshotManager captureImageWithFilter:filter configuration:config
                completionHandler:^(CGImageRef image, NSError *captureError) {
                    if (!captureError && image) picture = CGImageRetain(image);
                    [config release];
                    [filter release];
                    dispatch_semaphore_signal(ready);
                }];
        }];
        dispatch_semaphore_wait(ready, DISPATCH_TIME_FOREVER);
        dispatch_release(ready);
        return picture;
    }
    // Resolve the legacy symbol only on older systems; newer SDKs no longer
    // allow direct references to CGDisplayCreateImage.
    CGImageRef (*legacy)(CGDirectDisplayID) = dlsym(RTLD_DEFAULT, "CGDisplayCreateImage");
    return legacy ? legacy(id) : NULL;
}

static int snip_missing_image(CGImageRef picture) { return picture == NULL; }

static int snip_read_pixels(CGImageRef picture, unsigned char *pixels, size_t width, size_t height) {
    CGColorSpaceRef srgb = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
    if (!srgb) return 0;
    CGContextRef context = CGBitmapContextCreate(pixels, width, height, 8, width*4, srgb,
        kCGImageAlphaPremultipliedLast | kCGBitmapByteOrder32Big);
    CGColorSpaceRelease(srgb);
    if (!context) return 0;
    CGContextSetBlendMode(context, kCGBlendModeCopy);
    CGContextSetInterpolationQuality(context, kCGInterpolationNone);
    CGContextDrawImage(context, CGRectMake(0, 0, width, height), picture);
    CGContextRelease(context);
    return 1;
}
*/
import "C"

import (
	"errors"
	"image"
	"math"
	"unsafe"

	"goim/internal/capture"
)

func desktopCursor() (image.Point, bool) {
	var p C.CGPoint
	ok := C.snip_cursor(&p) != 0
	return image.Pt(int(math.Floor(float64(p.x))), int(math.Floor(float64(p.y)))), ok
}

func desktopWindows() []image.Rectangle {
	var rects [512]C.CGRect
	count := int(C.snip_windows(&rects[0], C.size_t(len(rects))))
	windows := make([]image.Rectangle, 0, count)
	for _, r := range rects[:count] {
		windows = append(windows, image.Rect(
			int(math.Floor(float64(r.origin.x))), int(math.Floor(float64(r.origin.y))),
			int(math.Ceil(float64(r.origin.x+r.size.width))), int(math.Ceil(float64(r.origin.y+r.size.height))),
		))
	}
	return windows
}

func captureSelectionDisplay(index int) (*capture.Desktop, error) {
	displays, err := capture.Displays()
	if err != nil {
		return nil, err
	}
	var selected capture.Display
	for _, d := range displays {
		if d.Index == index {
			selected = d
			break
		}
	}
	if selected.Bounds.Empty() {
		return nil, capture.ErrInvalidBounds
	}
	var ids [64]C.CGDirectDisplayID
	var count C.uint32_t
	if C.CGGetActiveDisplayList(C.uint32_t(len(ids)), &ids[0], &count) != C.kCGErrorSuccess {
		return nil, capture.ErrNoDisplays
	}
	var id C.CGDirectDisplayID
	for _, candidate := range ids[:int(count)] {
		bounds := C.CGDisplayBounds(candidate)
		if image.Rect(int(bounds.origin.x), int(bounds.origin.y),
			int(bounds.origin.x+bounds.size.width), int(bounds.origin.y+bounds.size.height)) == selected.Bounds {
			id = candidate
			break
		}
	}
	if id == 0 {
		return nil, capture.ErrNoDisplays
	}
	picture := C.snip_capture_display(id)
	if C.snip_missing_image(picture) != 0 {
		return nil, errors.New("could not capture display; check Screen Recording permission")
	}
	defer C.CGImageRelease(picture)
	w, h := int(C.CGImageGetWidth(picture)), int(C.CGImageGetHeight(picture))
	if w <= 0 || h <= 0 || int64(w)*int64(h) > maxInputPixels {
		return nil, capture.ErrTooLarge
	}
	pixels := image.NewRGBA(image.Rect(0, 0, w, h))
	if C.snip_read_pixels(picture, (*C.uchar)(unsafe.Pointer(&pixels.Pix[0])), C.size_t(w), C.size_t(h)) == 0 {
		return nil, errors.New("could not read original display pixels")
	}
	// Keep point coordinates for window placement and snapping. The image has
	// all backing pixels, which the crop and export work in directly.
	return &capture.Desktop{Image: pixels, Bounds: selected.Bounds, Displays: []capture.Display{selected}}, nil
}
