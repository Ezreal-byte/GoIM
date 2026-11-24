//go:build darwin && !ios

package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
)

func TestMain(m *testing.M) {
	if err := checkStartupThread(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func checkStartupThread() error {
	system, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return fmt.Errorf("open libSystem for the startup thread check: %w", err)
	}
	defer purego.Dlclose(system)
	address, err := purego.Dlsym(system, "pthread_main_np")
	if err != nil {
		return fmt.Errorf("resolve pthread_main_np: %w", err)
	}
	var isMainThread func() int32
	purego.RegisterFunc(&isMainThread, address)
	if isMainThread() == 0 {
		return errors.New("GoIM must start on the macOS process main thread")
	}

	// TestMain runs on the startup goroutine. Force a scheduler handoff with
	// one P so a worker could take the current OS thread without the lock.
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	workerOnMain := make(chan bool)
	go func() { workerOnMain <- isMainThread() != 0 }()
	if <-workerOnMain || isMainThread() == 0 {
		return errors.New("GoIM must retain the macOS process main thread across startup waits")
	}
	return nil
}
