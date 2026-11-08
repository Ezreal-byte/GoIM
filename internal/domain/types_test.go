package domain

import "testing"

func TestEnsureDefaultShortcutsAddsOnlyMissingActions(t *testing.T) {
	settings := Settings{DownloadDir: "downloads", Shortcuts: []Shortcut{
		{Action: "会话内搜索", Keys: "Ctrl+Alt+F", Scope: "应用内"},
		{Action: ShortcutScreenshot, Keys: "", Scope: "全局"},
	}}
	settings.EnsureDefaultShortcuts()
	if settings.Shortcuts[0].Keys != "Ctrl+Alt+F" {
		t.Fatal("existing custom shortcut was overwritten")
	}
	screenshotRows := 0
	createGroup := false
	for _, shortcut := range settings.Shortcuts {
		if shortcut.Action == ShortcutScreenshot {
			screenshotRows++
			if shortcut.Keys != "" {
				t.Fatal("cleared screenshot shortcut was unexpectedly restored")
			}
		}
		if shortcut.Action == "创建群聊" {
			createGroup = true
		}
	}
	if screenshotRows != 1 || !createGroup {
		t.Fatalf("migrated shortcuts = %+v", settings.Shortcuts)
	}
}
