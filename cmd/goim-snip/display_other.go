//go:build !windows && !linux && (!darwin || !cgo)

package main

import "image"

func desktopCursor() (image.Point, bool) { return image.Point{}, false }
func desktopWindows() []image.Rectangle  { return nil }
