package desktop

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestDetachedViewerLoadsFull4KScreenshotAndKeepsChatThumbnailSmall(t *testing.T) {
	state := shellTestState(t)
	path := filepath.Join(t.TempDir(), "4k.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 3840, 2160))
	img.SetRGBA(2000, 1000, color.RGBA{R: 50, G: 90, B: 200, A: 255})
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	file := domain.File{ID: "photo-4k", Name: "4k.png", MIME: "image/png", InlineImage: true, ConversationID: "conv-group", SenderID: "peer-a", SourcePath: path, CreatedAt: time.Now()}
	state.Files = append(state.Files, file)
	state.Messages = append(state.Messages, domain.Message{
		ID: "4k-message", ConversationID: "conv-group", FileID: file.ID, SenderID: "peer-a", Kind: "image", CreatedAt: file.CreatedAt,
	})
	h := newShellTestHarness(t, state)
	h.ui.presentViewer = func() {}
	h.ui.openImageViewer(file.ID)
	viewer := &h.ui.chatSurface().viewer
	// Full-resolution decode and rotation are much slower under the race detector.
	const mediaTimeout = 20 * time.Second
	h.waitForWithin(mediaTimeout, func() bool { return bitmapReady(viewer.original) && bitmapReady(h.ui.fileBitmap(path)) })
	if viewer.original.Width != 3840 || viewer.original.Height != 2160 {
		t.Fatalf("viewer decoded %d × %d", viewer.original.Width, viewer.original.Height)
	}
	pixel := 1000*viewer.original.Stride + 2000*4
	if got := viewer.original.Pix[pixel : pixel+4]; got[0] != 200 || got[1] != 90 || got[2] != 50 || got[3] != 255 {
		t.Fatalf("viewer pixel: %v", got)
	}
	if thumb := h.ui.fileBitmap(path); thumb.Width > 512 || thumb.Height > 512 {
		t.Fatal("original leaked into the conversation thumbnail cache")
	}
	h.ui.rotateImageViewer(1)
	h.waitForWithin(mediaTimeout, func() bool { return bitmapReady(viewer.original) })
	if viewer.original.Width != 2160 || viewer.original.Height != 3840 {
		t.Fatal("rotating selected the low-resolution thumbnail")
	}
	h.ui.closeImageViewer()
	if bitmapReady(viewer.original) {
		t.Fatal("closing the preview retained the full-size bitmap")
	}
}
