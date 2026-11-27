package core

import (
	"goim/internal/domain"
	"testing"
)

func TestScreenshotWindowPreferencePersists(t *testing.T) {
	dir := t.TempDir()
	n := testNodeAt(t, "截图设置", dir)
	if n.Snapshot().Settings.KeepWindowOnScreenshot {
		t.Fatal("new settings must hide windows by default")
	}
	if err := n.execute(domain.Command{Kind: domain.SetSetting, Setting: "keep_window_on_screenshot", Bool: true}); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := testNodeAt(t, "截图设置", dir)
	if !reopened.Snapshot().Settings.KeepWindowOnScreenshot {
		t.Fatal("screenshot preference was not restored")
	}
}
