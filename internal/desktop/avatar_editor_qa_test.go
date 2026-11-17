package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfileAvatarEditorScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name, theme string
	}{
		{name: "light"},
		{name: "dark", theme: "dark"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := shellTestState(t)
			state.Settings.Theme = test.theme
			shell, session := openShellQA(t, state, 1000, 700)
			avatar := writeShellTestPNG(t, "profile-crop.png", 640, 420)
			if err := session.Driver().QueueOpenFiles([]string{avatar}, nil); err != nil {
				t.Fatal(err)
			}
			shellQAClick(t, session, shell.ids.profileAvatar)
			shellQAShot(t, session, directory, filepath.Join("profile-avatar-picker-"+test.name+".png"))
			shellQAClick(t, session, shell.ids.profileChooseImage)
			if err := session.Expect("text", "调整头像"); err != nil {
				t.Fatal(err)
			}
			if err := session.Expect("layout", "clean"); err != nil {
				t.Fatal(err)
			}
			shellQAShot(t, session, directory, filepath.Join("profile-avatar-editor-"+test.name+".png"))
		})
	}
}
