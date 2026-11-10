//go:build !darwin || !cgo

package main

import "goim/internal/capture"

func captureSelectionDisplay(index int) (*capture.Desktop, error) {
	return capture.CaptureDisplay(index)
}
