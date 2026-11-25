package desktop

import (
	"testing"

	"goim/internal/domain"
)

func TestRemoteDesktopViewerCursorMotionKeepsVideoAndTexture(t *testing.T) {
	view, _, _ := remoteDesktopTestViewer(t, false, false)
	viewer := view.viewer
	frameVersion := viewer.bitmap.Version
	cursor := domain.RemoteDesktopCursor{X: -1, Y: 16, Width: 1, Height: 1, BGRA: []byte{255, 255, 255, 255}}
	viewer.pendingCursor.Store(&cursor)
	viewer.nextFrame()
	cursorVersion := viewer.cursorBitmap.Version
	if viewer.cursorX != -1 || viewer.cursorY != 16 || !bitmapReady(viewer.cursorBitmap) {
		t.Fatal("the cursor did not use the independent bitmap")
	}

	moved := cursor
	moved.X, moved.Y = 41, 25
	viewer.pendingCursor.Store(&moved)
	viewer.nextFrame()
	if viewer.cursorX != 41 || viewer.cursorY != 25 ||
		viewer.cursorBitmap.Version != cursorVersion || viewer.bitmap.Version != frameVersion {
		t.Fatal("cursor-only motion did not simply move the existing texture")
	}

	viewer.pendingCursor.Store(&domain.RemoteDesktopCursor{})
	viewer.nextFrame()
	if bitmapReady(viewer.cursorBitmap) || viewer.bitmap.Version != frameVersion {
		t.Fatal("hiding the cursor changed the video frame")
	}
}
