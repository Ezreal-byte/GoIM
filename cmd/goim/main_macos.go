//go:build darwin && !ios

package main

import "runtime"

func init() {
	// AppKit must start on the process main thread. Keep main on its startup
	// thread while it opens the database and starts the networking backend.
	runtime.LockOSThread()
}
