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
			avatar := writeShellTestPNG(t, "profile-crop.png", 2048, 1365)
			if err := session.Driver().QueueOpenFiles([]string{avatar}, nil); err != nil {
				t.Fatal(err)
			}
			shellQAClick(t, session, shell.ids.profileAvatar)
			shellQAShot(t, session, directory, filepath.Join("profile-avatar-picker-"+test.name+".png"))
			shellQAClick(t, session, shell.ids.profileChooseImage)
			if err := session.Expect("text", "裁剪图片", "within", "5000"); err != nil {
				t.Fatal(err)
			}
			if err := session.Expect("state", "button:保存", "enabled", "within", "5000"); err != nil {
				t.Fatal(err)
			}
			if err := session.Expect("layout", "clean"); err != nil {
				t.Fatal(err)
			}
			shellQAShot(t, session, directory, filepath.Join("profile-avatar-editor-"+test.name+".png"))
			shellQAClick(t, session, shell.ids.profileCropConfirm)
			if err := session.Expect("text", "个人资料", "within", "5000"); err != nil {
				t.Fatal(err)
			}
			if err := session.Expect("layout", "clean"); err != nil {
				t.Fatal(err)
			}
			shellQAShot(t, session, directory, filepath.Join("profile-avatar-round-"+test.name+".png"))
		})
	}
}

func TestGroupAvatarEditorScreenshotQA(t *testing.T) {
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
			state.Groups[0].Avatar = writeShellTestPNG(t, "current-group-avatar.png", 512, 512)
			shell, session := openShellQA(t, state, 1000, 700)
			avatar := writeShellTestPNG(t, "group-crop.png", 2048, 1365)
			if err := session.Driver().QueueOpenFiles([]string{avatar}, nil); err != nil {
				t.Fatal(err)
			}

			shellQAClick(t, session, shell.ids.chatHeaderMembers)
			shellQAClick(t, session, shell.ids.memberAvatar)
			if err := session.Expect("text", "更换群头像", "within", "5000"); err != nil {
				t.Fatal(err)
			}
			if err := session.Expect("state", "button:保存", "enabled", "within", "5000"); err != nil {
				t.Fatal(err)
			}
			shellQAShot(t, session, directory, filepath.Join("group-avatar-editor-"+test.name+".png"))
			if err := session.Expect("layout", "clean"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
