package desktop

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"goim/internal/domain"
)

func TestAppearanceScreenshotQA(t *testing.T) {
	dir := os.Getenv("GOIM_QA_SCREENSHOTS")
	if dir == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to capture the appearance matrix")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"light", "dark"} {
		for _, scheme := range domain.ColorSchemes() {
			t.Run(scheme+"-"+mode, func(t *testing.T) {
				state := shellTestState(t)
				state.Settings.ColorScheme, state.Settings.Theme = scheme, mode
				shell, session := openShellQA(t, state, 1100, 760)
				shellQAClick(t, session, shell.convRowIDs["conv-group"])
				if err := session.Expect("layout", "clean"); err != nil {
					t.Fatal(err)
				}
				shellQAShot(t, session, dir, scheme+"-"+mode+".png")
			})
		}
	}
	const wallpaper = "wallpaper.png"
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			src.SetRGBA(x, y, color.RGBA{R: 32, G: 118, B: 143, A: 255})
		}
	}
	path := filepath.Join(t.TempDir(), wallpaper)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, src); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	state := shellTestState(t)
	state.Settings.Background, state.Settings.BackgroundTransparency = path, 0
	shell, session := openShellQA(t, state, 1100, 760)
	shellQAClick(t, session, shell.convRowIDs["conv-group"])
	shellQAShot(t, session, dir, wallpaper)
	screenshot, err := os.Open(filepath.Join(dir, wallpaper))
	if err != nil {
		t.Fatal(err)
	}
	defer screenshot.Close()
	img, err := png.Decode(screenshot)
	if err != nil {
		t.Fatal(err)
	}
	r := shellQAHit(t, session, shell.ids.chatEditorField).Bounds
	got := color.RGBAModel.Convert(img.At(int(r.X+r.Width/2), int(r.Y+r.Height/2))).(color.RGBA)
	if got != src.RGBAAt(0, 0) {
		t.Fatalf("wallpaper in the editor = %v, want %v", got, src.RGBAAt(0, 0))
	}
	// A full-opacity wallpaper makes a square corner or a missing gutter visible.
	chrome := img.At(2, 2)
	left, top := int(shell.railExtent()), int(shell.lastBody.Y)
	right, bottom := int(shell.lastChat.Right())-1, int(shell.lastChat.Bottom())-1
	for _, pt := range []image.Point{
		{2, top - 1}, {2, top}, {left, top}, {right, top}, {left, bottom}, {right, bottom},
		{img.Bounds().Max.X - 2, (top + bottom) / 2}, {(left + right) / 2, img.Bounds().Max.Y - 2},
	} {
		if img.At(pt.X, pt.Y) != chrome {
			t.Errorf("workspace chrome at %v = %v, want %v", pt, img.At(pt.X, pt.Y), chrome)
		}
	}
	shellQAClick(t, session, shell.ids.navSettings)
	shellQAClick(t, session, shell.panels.ids.settingsCategories.At(int(panelSettingsGeneral)))
	shellQAShot(t, session, dir, "appearance-settings.png")
}
