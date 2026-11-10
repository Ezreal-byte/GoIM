//go:build !windows

package main

import (
	"github.com/vivaui/viva/ui"
	"goim/internal/capture"
)

func overlayPlacement(display capture.Display) (int, int, ui.Point) {
	// macOS bounds are screen points. The backing bitmap may be twice as
	// large, so placement must not be derived from its dimensions.
	return display.Bounds.Dx(), display.Bounds.Dy(), ui.Point{X: float32(display.Bounds.Min.X), Y: float32(display.Bounds.Min.Y)}
}
