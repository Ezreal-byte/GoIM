package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	vivaheadless "github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func TestChatLayoutScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_CHAT_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_CHAT_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	run := func(theme string, width, height int, suffix string) {
		state := shellTestState(t)
		state.Settings.Theme = theme
		imagePath := addViewerImage(t, &state, "file-image-viewer", "聊天截图.png", time.Now(), 512)
		secondPath := addViewerImage(t, &state, "file-image-viewer-2", "第二张.png", time.Now().Add(time.Minute), 384)
		backend := newShellTestBackend(state)
		shell := New(backend)
		for index, path := range []string{imagePath, secondPath} {
			if bitmap, ok := decodeBitmap(path, 512); ok {
				bitmap.ID = uint64(index + 1)
				bitmap.Version = 1
				shell.imageCache[path] = bitmap
			}
		}
		config := chatSurfaceConfig(shell)
		config.Width, config.Height = width, height
		session, err := vivaheadless.Open(config, nil, vivaheadless.Options{Scale: 1})
		if err != nil {
			shell.Close()
			t.Fatal(err)
		}
		t.Cleanup(session.Close)
		click := func(id ui.ID) {
			t.Helper()
			for _, hit := range session.Snapshot().Hits {
				if hit.ID == id {
					if err := session.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
					return
				}
			}
			t.Fatalf("control %v is not on screen", id)
		}
		shot := func(name string) {
			t.Helper()
			if err := session.Wait(700 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if err := session.Screenshot(filepath.Join(directory, name)); err != nil {
				t.Fatal(err)
			}
		}

		shot("chat-layout-" + suffix + ".png")
		historyPos := shell.chatSurface().history.Pos
		history, ok := findSessionHit(session.Snapshot().Hits, shell.chatSurface().ids.historySplitter)
		if !ok {
			t.Fatal("history splitter is missing")
		}
		historyX := history.Bounds.X + history.Bounds.Width/2
		historyY := history.Bounds.Y + history.Bounds.Height/2
		if err := session.Press(historyX, historyY); err != nil {
			t.Fatal(err)
		}
		if err := session.Move(historyX, historyY-110); err != nil {
			t.Fatal(err)
		}
		if err := session.Release(historyX, historyY-110); err != nil {
			t.Fatal(err)
		}
		shot("chat-layout-history-" + suffix + ".png")
		history, ok = findSessionHit(session.Snapshot().Hits, shell.chatSurface().ids.historySplitter)
		if !ok {
			t.Fatal("history splitter disappeared after its drag")
		}
		historyX = history.Bounds.X + history.Bounds.Width/2
		historyY = history.Bounds.Y + history.Bounds.Height/2
		if err := session.Press(historyX, historyY); err != nil {
			t.Fatal(err)
		}
		restoreY := historyY + historyPos - shell.chatSurface().history.Pos
		if err := session.Move(historyX, restoreY); err != nil {
			t.Fatal(err)
		}
		if err := session.Release(historyX, restoreY); err != nil {
			t.Fatal(err)
		}

		splitter, ok := findSessionHit(session.Snapshot().Hits, shell.chatSurface().ids.sidebarSplitter)
		if !ok {
			t.Fatal("sidebar splitter is missing")
		}
		x := splitter.Bounds.X + splitter.Bounds.Width/2
		y := splitter.Bounds.Y + splitter.Bounds.Height/2
		if err := session.Press(x, y); err != nil {
			t.Fatal(err)
		}
		if err := session.Move(x+90, y); err != nil {
			t.Fatal(err)
		}
		if err := session.Release(x+90, y); err != nil {
			t.Fatal(err)
		}
		shot("chat-layout-resized-" + suffix + ".png")

		click(shell.ids.chatHeaderFile)
		shot("chat-files-" + suffix + ".png")
		click(shell.fileRowIDs["file-1"])
		shot("chat-file-detail-" + suffix + ".png")
		click(shell.ids.fileClose)
		click(shell.fileButtonIDs["file-image-viewer"])
		shot("chat-image-viewer-" + suffix + ".png")
		click(shell.chatSurface().ids.viewerNext)
		shot("chat-image-viewer-next-" + suffix + ".png")
		click(shell.chatSurface().ids.viewerActual)
		shot("chat-image-viewer-actual-" + suffix + ".png")
		shell.Close()
	}

	run("light", 1440, 960, "light")
	run("dark", 1440, 960, "dark")
	run("light", 1000, 700, "compact-light")
}

func findSessionHit(hits []ui.Hit, id ui.ID) (ui.Hit, bool) {
	for _, hit := range hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return ui.Hit{}, false
}
