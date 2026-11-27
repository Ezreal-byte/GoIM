package desktop

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

type savedWindowFrame struct {
	Width, Height int
	Origin        *gfx.Point `json:",omitempty"`
}

// Owned by the application thread. Keep a floating frame per window type;
// moving/resizing is frequent, so only write the final frame when closing.
type windowLayoutStore struct {
	path   string
	frames map[string]savedWindowFrame
}

func loadWindowGeometry(directory string) windowLayoutStore {
	if directory == "" {
		return windowLayoutStore{}
	}
	g := windowLayoutStore{path: filepath.Join(directory, "window-layout.json")}
	if raw, err := os.ReadFile(g.path); err == nil {
		if json.Unmarshal(raw, &g.frames) != nil {
			g.frames = nil
		}
	}
	return g
}

func (g *windowLayoutStore) rememberVoiceInvitation(cfg *app.Config) {
	kind := "voice-invitation"
	if runtime.GOOS == "windows" {
		// Older builds (including v2 before Viva's reportFrame fix) saved
		// positions divided by the active monitor's DPI, drifting on every open.
		kind = "voice-invitation-windows-v3"
	}
	width, height := cfg.Width, cfg.Height
	g.remember(kind, cfg)
	cfg.Width, cfg.Height = width, height
}

func (g *windowLayoutStore) remember(kind string, cfg *app.Config) {
	frame := g.frames[kind]
	if frame.Width > 0 && frame.Height > 0 {
		cfg.Width, cfg.Height = max(cfg.MinWidth, frame.Width), max(cfg.MinHeight, frame.Height)
		cfg.Origin = frame.Origin
	} else {
		frame = savedWindowFrame{Width: cfg.Width, Height: cfg.Height, Origin: cfg.Origin}
	}
	var positioned, changed, maximized, fullscreen bool
	maximize, full := cfg.OnMaximize, cfg.OnFullscreen
	move, resize := cfg.OnFrameChange, cfg.OnResize
	closed := cfg.OnClosed
	cfg.OnMaximize = func(active bool) {
		maximized = active
		if maximize != nil {
			maximize(active)
		}
	}
	cfg.OnFullscreen = func(active bool) {
		fullscreen = active
		if full != nil {
			full(active)
		}
	}
	cfg.OnFrameChange = func(r gfx.Rect) {
		if r.Width > 0 && r.Height > 0 && !maximized && !fullscreen {
			point := gfx.Point{X: r.X, Y: r.Y}
			frame = savedWindowFrame{
				Width: int(math.Round(float64(r.Width))), Height: int(math.Round(float64(r.Height))),
				Origin: &point,
			}
			positioned, changed = true, true
		}
		if move != nil {
			move(r)
		}
	}
	cfg.OnResize = func(size gfx.Size) {
		// Wayland supplies content sizes but leaves placement to the compositor.
		if !positioned && !maximized && !fullscreen && size.Width > 0 && size.Height > 0 {
			frame.Width, frame.Height = int(math.Round(float64(size.Width))), int(math.Round(float64(size.Height)))
			changed = true
		}
		if resize != nil {
			resize(size)
		}
	}
	cfg.OnClosed = func() {
		if changed {
			if g.frames == nil {
				g.frames = make(map[string]savedWindowFrame)
			}
			g.frames[kind] = frame
			g.save()
		}
		if closed != nil {
			closed()
		}
	}
}

func (g *windowLayoutStore) save() {
	if g.path == "" {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(g.path), ".goim-layout-*")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(g.frames)
	if closeErr := f.Close(); err == nil && closeErr == nil {
		_ = os.Rename(f.Name(), g.path)
	}
}
