//go:build windows

package main

import (
	"os"
	"testing"

	"goim/internal/capture"
)

func TestNativeWindowSnapProbe(t *testing.T) {
	if os.Getenv("GOIM_SNIP_NATIVE_TEST") == "" {
		t.Skip("requires an interactive Windows desktop")
	}
	displays, err := capture.Displays()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := desktopCursor(); !ok {
		t.Fatal("could not locate the native pointer")
	}
	windows := desktopWindows()
	visible := 0
	for _, window := range windows {
		for _, display := range displays {
			if !window.Intersect(display.Bounds).Empty() {
				visible++
				break
			}
		}
	}
	if visible == 0 {
		t.Fatal("no on-screen window bounds from the Windows desktop")
	}
	t.Logf("discovered %d on-screen window bounds across %d display(s)", visible, len(displays))
}
