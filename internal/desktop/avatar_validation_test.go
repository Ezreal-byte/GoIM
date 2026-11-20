package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAvatarSelectionRejectsInvalidImagesWithoutOpeningEditor(t *testing.T) {
	for _, group := range []bool{false, true} {
		for _, kind := range []string{"format", "bytes", "pixels", "damaged", "missing"} {
			t.Run(kind+map[bool]string{true: "/group", false: "/profile"}[group], func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "avatar.png")
				want := "无法读取图片"
				switch kind {
				case "format":
					if err := os.WriteFile(path, []byte("not an image"), 0600); err != nil {
						t.Fatal(err)
					}
					want = "不支持的图片格式"
				case "bytes":
					path = writeExtraTestPNG(t, "large.png")
					if err := os.Truncate(path, maxImageFileBytes+1); err != nil {
						t.Fatal(err)
					}
					want = "图片文件太大"
				case "pixels":
					path = writePNGHeader(t, 100000, 100000)
					want = "图片尺寸太大"
				case "damaged":
					path = writePNGHeader(t, 128, 128)
					want = "图片已损坏"
				}
				h := newShellTestHarnessAtSize(t, shellTestState(t), 1000, 700)
				if err := h.driver.QueueOpenFiles([]string{path}, nil); err != nil {
					t.Fatal(err)
				}
				previous := h.ui.profile.avatar
				if group {
					h.clickID(h.ui.ids.chatHeaderMembers)
					h.clickID(h.ui.ids.memberAvatar)
				} else {
					h.clickID(h.ui.ids.profileAvatar)
					previous = h.ui.profile.avatar
					h.clickID(h.ui.ids.profileChooseImage)
				}
				h.waitFor(func() bool { return !h.ui.profile.crop.pending && !h.ui.groupAvatar.pending })
				if h.ui.profile.crop.open || h.ui.groupAvatar.open || h.ui.profile.avatar != previous {
					t.Fatal("invalid selection opened the cropper or changed the avatar")
				}
				if !strings.Contains(h.ui.toasts[0].message.Description, want) {
					t.Fatalf("notice = %q, want %q", h.ui.toasts[0].message.Description, want)
				}
			})
		}
	}
}

func TestAvatarSelectionSupersedesPendingThumbnail(t *testing.T) {
	path := writeShellTestPNG(t, "selected.png", 1024, 700)
	h := newShellTestHarnessAtSize(t, shellTestState(t), 1000, 700)
	started, release := make(chan struct{}), make(chan struct{})
	h.ui.media.setBeforeJob(func(job mediaJob) {
		if job.path == path && job.imageLimit == 192 {
			close(started)
			<-release
		}
	})
	h.ui.avatarImagePaths[path] = true
	h.ui.ensureImage(path)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("thumbnail did not start")
	}
	if err := h.driver.QueueOpenFiles([]string{path}, nil); err != nil {
		close(release)
		t.Fatal(err)
	}
	h.clickID(h.ui.ids.profileAvatar)
	h.clickID(h.ui.ids.profileChooseImage)
	close(release)
	h.waitFor(func() bool { return h.ui.profile.crop.open })
	if bitmap := h.ui.imageCache[path]; bitmap.Width != 1024 || bitmap.Height != 700 {
		t.Fatalf("editor used the thumbnail instead of validated source: %dx%d", bitmap.Width, bitmap.Height)
	}
}
