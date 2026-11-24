//go:build darwin && !ios

package main

import "runtime"

func init() {
	// The editor must start AppKit on the process main thread after loading
	// the input image or capturing the screen.
	runtime.LockOSThread()
}
