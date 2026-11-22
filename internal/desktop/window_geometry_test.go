package desktop

import (
	"runtime"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestWindowLayoutRestoresFloatingFrameAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	store := loadWindowGeometry(dir)
	var resized, moved, maximized, fullscreen, closed int
	config := app.Config{
		Width: 600, Height: 500,
		OnResize: func(gfx.Size) { resized++ }, OnFrameChange: func(gfx.Rect) { moved++ },
		OnMaximize: func(bool) { maximized++ }, OnFullscreen: func(bool) { fullscreen++ },
		OnClosed: func() { closed++ },
	}
	store.remember("history", &config)
	config.OnResize(gfx.Size{Width: 740, Height: 560})
	config.OnFrameChange(gfx.Rect{X: -124, Y: 70, Width: 740, Height: 560})
	config.OnMaximize(true)
	config.OnResize(gfx.Size{Width: 1600, Height: 1200})
	config.OnFrameChange(gfx.Rect{Width: 1600, Height: 1200})
	config.OnMaximize(false)
	config.OnFullscreen(true)
	config.OnFrameChange(gfx.Rect{Width: 1920, Height: 1440})
	config.OnClosed()
	if resized != 2 || moved != 3 || maximized != 2 || fullscreen != 1 || closed != 1 {
		t.Fatalf("lost window callback: resize=%d frame=%d maximize=%d fullscreen=%d close=%d", resized, moved, maximized, fullscreen, closed)
	}
	next := loadWindowGeometry(dir)
	history := app.Config{Width: 400, Height: 300}
	next.remember("history", &history)
	if history.Width != 740 || history.Height != 560 || history.Origin == nil || *history.Origin != (gfx.Point{X: -124, Y: 70}) {
		t.Fatalf("unexpected history frame: %dx%d at %v", history.Width, history.Height, history.Origin)
	}
	members := app.Config{Width: 440, Height: 620}
	next.remember("members", &members)
	if members.Width != 440 || members.Height != 620 || members.Origin != nil {
		t.Fatal("history placement leaked into members window")
	}
}

func TestWindowLayoutStoresCompositorSizeWithoutInventingPosition(t *testing.T) {
	dir := t.TempDir()
	store := loadWindowGeometry(dir)
	cfg := app.Config{Width: 440, Height: 600}
	store.remember("members", &cfg)
	cfg.OnResize(gfx.Size{Width: 500, Height: 620})
	cfg.OnMaximize(true)
	cfg.OnResize(gfx.Size{Width: 1600, Height: 1000})
	cfg.OnClosed()
	next := loadWindowGeometry(dir)
	cfg = app.Config{Width: 440, Height: 600, MinWidth: 540}
	next.remember("members", &cfg)
	if cfg.Width != 540 || cfg.Height != 620 || cfg.Origin != nil {
		t.Fatalf("restored compositor frame: %dx%d at %v", cfg.Width, cfg.Height, cfg.Origin)
	}
}

func TestVoiceInvitationRestoresCurrentCoordinatesAndKeepsItsSize(t *testing.T) {
	dir := t.TempDir()
	store := loadWindowGeometry(dir)
	legacy := app.Config{Width: 500, Height: 150}
	store.remember("voice-invitation", &legacy)
	legacy.OnFrameChange(gfx.Rect{X: -250, Y: 150, Width: 500, Height: 150})
	legacy.OnClosed()

	store = loadWindowGeometry(dir)
	config := app.Config{Width: 336, Height: 136}
	store.rememberVoiceInvitation(&config)
	if runtime.GOOS == "windows" {
		if config.Origin != nil {
			t.Fatalf("legacy scaled Windows position was reused: %+v", config.Origin)
		}
	} else if config.Origin == nil || *config.Origin != (gfx.Point{X: -250, Y: 150}) {
		t.Fatalf("existing notification position was lost: %+v", config.Origin)
	}

	config.OnFrameChange(gfx.Rect{X: -750, Y: 400, Width: 336, Height: 136})
	config.OnClosed()
	store = loadWindowGeometry(dir)
	next := app.Config{Width: 320, Height: 120}
	store.rememberVoiceInvitation(&next)
	if next.Origin == nil || *next.Origin != (gfx.Point{X: -750, Y: 400}) || next.Width != 320 || next.Height != 120 {
		t.Fatalf("restored notification = %dx%d at %+v", next.Width, next.Height, next.Origin)
	}
}
