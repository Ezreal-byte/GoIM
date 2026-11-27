package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
	"goim/internal/i18n"
	"goim/internal/video"
)

func TestTextMenuLabelsFollowLanguage(t *testing.T) {
	defer i18n.SetLanguage("zh-CN")

	state := shellTestState(t)
	state.Settings.Language = "zh-CN"
	h := newShellTestHarness(t, state)
	cfg := h.ui.Config()
	if cfg.TextMenu == nil {
		t.Fatal("Config.TextMenu is nil")
	}
	if cfg.TextMenu.Cut != "剪切" || cfg.TextMenu.Copy != "复制" || cfg.TextMenu.Paste != "粘贴" || cfg.TextMenu.SelectAll != "全选" {
		t.Fatalf("zh-CN TextMenu = %+v", *cfg.TextMenu)
	}

	state.Settings.Language = "en"
	state.Revision++
	h.publish(state)
	if cfg.TextMenu.Cut != "Cut" || cfg.TextMenu.Copy != "Copy" || cfg.TextMenu.Paste != "Paste" || cfg.TextMenu.SelectAll != "Select all" {
		t.Fatalf("en TextMenu = %+v", *cfg.TextMenu)
	}

	detached := newDetachedConversationView(h.backend, "conv-group")
	defer detached.Close()
	if detached.DetachedConfig().TextMenu == nil || detached.DetachedConfig().TextMenu.Paste != "Paste" {
		t.Fatalf("detached TextMenu = %+v", detached.DetachedConfig().TextMenu)
	}

	hist := newHistoryWindowView(h.backend, "conv-group")
	defer hist.Close()
	if hist.Config().TextMenu == nil || hist.Config().TextMenu.Copy != "Copy" {
		t.Fatalf("history TextMenu = %+v", hist.Config().TextMenu)
	}

	pr := newPrintWindowView(h.backend, "")
	defer pr.Close()
	if pr.Config().TextMenu == nil || pr.Config().TextMenu.Cut != "Cut" {
		t.Fatalf("print TextMenu = %+v", pr.Config().TextMenu)
	}
}

func TestAutoPasteScreenshotStagesPNGIntoComposer(t *testing.T) {
	state := shellTestState(t)
	h := newShellTestHarness(t, state)
	cacheDir := t.TempDir()
	h.ui.clipboardCacheDir = func() (string, error) { return cacheDir, nil }

	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{R: 20, G: 120, B: 220, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	rt := &desktopRuntime{ui: h.ui, backend: h.backend}
	if !rt.autoPasteScreenshot(h.ui, buf.Bytes()) {
		t.Fatal("autoPasteScreenshot did not stage captured PNG into active composer")
	}
	draft := h.ui.mixedDraft(h.ui.selected)
	if draft == nil || len(draft.files) != 1 {
		t.Fatalf("expected 1 staged screenshot file in mixed draft, got %+v", draft)
	}
	if _, err := os.Stat(draft.files[0].file.SourcePath); err != nil {
		t.Fatalf("staged screenshot file missing on disk: %v", err)
	}
	if filepath.Ext(draft.files[0].file.SourcePath) != ".png" {
		t.Fatalf("staged screenshot extension = %q, want .png", filepath.Ext(draft.files[0].file.SourcePath))
	}

	// Must not auto-paste when locked.
	state.Locked = true
	state.Revision++
	h.publish(state)
	if rt.autoPasteScreenshot(h.ui, buf.Bytes()) {
		t.Fatal("autoPasteScreenshot staged screenshot while locked")
	}
}

func TestFileDropHoverClearsWhenLeavingChatRegion(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	box := h.ui.composerBox.Rect()
	ok, err := h.driver.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: box.X + 20, Y: box.Y + 20})
	if err != nil || !ok || !h.ui.dragging {
		t.Fatalf("hover over chat: ok=%v dragging=%v err=%v", ok, h.ui.dragging, err)
	}
	// Move back over the sidebar (outside lastChat): overlay must dismiss.
	ok, err = h.driver.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: 20, Y: box.Y + 20})
	if err != nil || ok || h.ui.dragging {
		t.Fatalf("hover outside chat did not clear dragging: ok=%v dragging=%v err=%v", ok, h.ui.dragging, err)
	}
}

type videoNoCamTestBackend struct {
	*shellTestBackend
	videoCh chan []byte
}

func (b *videoNoCamTestBackend) SendVideoFrame(string, []byte) error { return nil }
func (b *videoNoCamTestBackend) VideoFrames(string) <-chan []byte    { return b.videoCh }

func TestVideoCallShowsRemoteFramesWhenLocalPeerHasNoCamera(t *testing.T) {
	state := shellTestState(t)
	state.VoiceCall = domain.VoiceCall{
		ID:          "video-nocam",
		PeerID:      "peer-alice",
		PeerName:    "Alice",
		Video:       true,
		Direction:   domain.VoiceCallIncoming,
		Status:      domain.VoiceCallActive,
		StartedAt:   time.Now().Add(-5 * time.Second),
		ConnectedAt: time.Now().Add(-3 * time.Second),
	}
	vb := &videoNoCamTestBackend{
		shellTestBackend: newShellTestBackend(state),
		videoCh:          make(chan []byte, 4),
	}
	media := newVideoCallMedia(vb, func() {})
	media.openCamera = func(string) (video.Capture, error) {
		return nil, video.ErrNoDevice
	}
	defer media.close()

	media.sync(state.VoiceCall)

	// Push a valid JPEG frame from the remote peer who HAS a camera.
	bgra := make([]byte, 32*24*4)
	for i := 0; i < len(bgra); i += 4 {
		bgra[i], bgra[i+1], bgra[i+2], bgra[i+3] = 200, 120, 40, 255
	}
	encoded, err := video.EncodeJPEG(video.Frame{Width: 32, Height: 24, BGRA: bgra}, 32, 24, 75)
	if err != nil {
		t.Fatal(err)
	}
	vb.videoCh <- encoded

	deadline := time.Now().Add(2 * time.Second)
	var remoteBmp gfx.Bitmap
	for time.Now().Before(deadline) {
		remoteBmp, _ = media.pullFrames()
		if bitmapReady(remoteBmp) && media.cameraProblem() != "" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !bitmapReady(remoteBmp) {
		t.Fatal("remote video frame was not decoded when local camera is absent")
	}
	if got := media.cameraProblem(); got != i18n.T("未检测到可用摄像头") {
		t.Fatalf("cameraProblem = %q, want %q", got, i18n.T("未检测到可用摄像头"))
	}
}
