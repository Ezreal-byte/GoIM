package desktop

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"goim/internal/shotlet"
)

func TestScreenshotFallbackRequiresAnUnavailableHelper(t *testing.T) {
	directory := t.TempDir()
	helper := filepath.Join(directory, "capture-helper")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	build := exec.Command("go", "build", "-o", helper, "../shotlet/testdata/helper")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, out)
	}
	for _, test := range []struct {
		name                        string
		broken, canceled            bool
		result                      int
		fallback, completed, failed bool
	}{
		{name: "shotlet completes", completed: true},
		{name: "cannot load helper", broken: true, fallback: true, completed: true},
		{name: "user cancels", result: 2},
		{name: "saved inside shotlet", result: 4},
		{name: "permission refused", result: 3, failed: true},
		{name: "capture error", result: 1, failed: true},
		{name: "application exits", canceled: true, failed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "output.png")
			if err := os.WriteFile(output, nil, 0600); err != nil {
				t.Fatal(err)
			}
			marker := output + ".fallback"
			t.Setenv("GOIM_SHOTLET_TEST_FALLBACK", marker)
			t.Setenv("GOIM_SHOTLET_TEST_RESULT", strconv.Itoa(test.result))
			broken := ""
			if test.broken {
				broken = "1"
			}
			t.Setenv("GOIM_SHOTLET_TEST_BROKEN", broken)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.canceled {
				cancel()
			}
			completed, err := (screenshotTool{path: helper, shotlet: true, fallback: helper}).run(ctx, output, "light")
			if completed != test.completed || (err != nil) != test.failed {
				t.Fatalf("capture = %v, %v", completed, err)
			}
			_, fallbackErr := os.Stat(marker)
			if (fallbackErr == nil) != test.fallback {
				t.Fatalf("fallback used = %v", fallbackErr == nil)
			}
			if completed {
				if _, err := readScreenshot(output); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestScreenshotToolPathHonorsOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goim-snip-test")
	if err := os.WriteFile(path, []byte("tool"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOIM_SNIP_PATH", path)
	got, err := resolveScreenshotTool()
	if err != nil {
		t.Fatal(err)
	}
	if got.path != path || got.shotlet {
		t.Fatalf("screenshot tool = %+v, want the built-in tool at %q", got, path)
	}
}

func TestPackagedScreenshotToolSelectsSupportedHelper(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "GoIM")
	builtin := filepath.Join(directory, "goim-snip")
	if runtime.GOOS == "windows" {
		executable += ".exe"
		builtin += ".exe"
	}
	if runtime.GOOS == "darwin" {
		directory = filepath.Join(directory, "GoIM.app", "Contents", "MacOS")
		executable = filepath.Join(directory, "GoIM")
		builtin = filepath.Join(directory, "goim-snip")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(builtin, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	want := screenshotTool{path: builtin}
	if got, err := packagedScreenshotTool(executable); err != nil || got != want {
		t.Fatalf("fallback tool = %+v, %v; want %+v", got, err, want)
	}
	if helper := shotlet.HelperPath(executable); helper != "" {
		if err := os.MkdirAll(filepath.Dir(helper), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(helper, nil, 0o700); err != nil {
			t.Fatal(err)
		}
		if shotlet.Supported() {
			want = screenshotTool{path: helper, shotlet: true, fallback: builtin}
		}
	}
	if got, err := packagedScreenshotTool(executable); err != nil || got != want {
		t.Fatalf("packaged tool = %+v, %v; want %+v", got, err, want)
	}
}

func TestReadScreenshotValidatesStrictPNG(t *testing.T) {
	var encoded bytes.Buffer
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.NRGBA{R: 20, G: 40, B: 60, A: 255})
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := readScreenshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, encoded.Bytes()) {
		t.Fatal("screenshot bytes changed while reading")
	}
	if err := os.WriteFile(path, []byte("not png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readScreenshot(path); err == nil {
		t.Fatal("invalid screenshot content was accepted")
	}
}
