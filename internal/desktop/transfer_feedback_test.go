package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"goim/internal/desktop/fileglyph"
	"goim/internal/domain"
	"goim/internal/media"
)

func TestTransferMediaPreviewUsesSelectedLocalCopy(t *testing.T) {
	state := shellTestState(t)
	path := addViewerImage(t, &state, "photo", "图片.png", time.Now(), 24)
	video, err := filepath.Abs("../media/testdata/clip.mp4")
	if err != nil {
		t.Fatal(err)
	}
	state.Settings.VoiceSpeakerVolume = 0
	state.Transfers = []domain.Transfer{
		{ID: "incoming", FileID: "shared-project-entry", Name: "图片.png", Direction: "receive", Status: "completed", FromShare: true, Path: path},
		{ID: "outgoing", FileID: "photo", Name: "图片.png", Direction: "send", Status: "offered"},
		{ID: "video", Name: "网页接收.mp4", WebSender: "browser", Direction: "receive", Status: "completed", Path: video},
		{ID: "pending", Name: "未完成.png", Direction: "receive", Status: "transferring", Path: path},
		{ID: "folder", Name: "目录.png", ShareFolder: true, Direction: "receive", Status: "completed", Path: t.TempDir()},
	}
	h := newShellTestHarness(t, state)
	h.clickID(h.ui.ids.navTransfers)
	for _, entry := range h.ui.panels.transferRows {
		id := entry.transfer.ID
		if id == "folder" || id == "pending" {
			if entry.preview || !entry.ids.preview.IsZero() {
				t.Fatalf("transfer %q should not be previewable", id)
			}
			continue
		}
		if !entry.preview {
			t.Fatalf("missing preview for transfer %q", id)
		}
		if id == "video" {
			h.clickID(entry.ids.open)
		} else {
			h.clickID(entry.ids.preview)
		}
		if !h.ui.imageViewerOpen() || h.ui.section != sectionTransfers || h.ui.viewerFile().ID != id {
			t.Fatalf("preview did not open for transfer %q", id)
		}
		if id == "video" {
			if h.ui.viewerPath() != video || h.ui.surface.viewer.video == nil {
				t.Fatal("transfer did not open the video player")
			}
			if _, err := os.Stat(media.HelperPath()); err == nil {
				h.waitFor(func() bool { return h.ui.surface.viewer.video.Snapshot().Frame.Number > 1 })
			}
		} else {
			if h.ui.viewerPath() != path {
				t.Fatalf("wrong image source: %q", h.ui.viewerPath())
			}
			h.waitFor(func() bool { return bitmapReady(h.ui.fileBitmap(path)) })
		}
		h.clickID(h.ui.surface.ids.viewerClose)
	}
}

func TestTransferListUsesAttachmentMetadataForIcons(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Files = []domain.File{
		{ID: "folder", Name: "存档.zip", Folder: true},
		{ID: "sound", Name: "录音", MIME: "audio/ogg"},
	}
	state.Transfers = []domain.Transfer{
		{ID: "directory-transfer", FileID: "folder", Name: "存档.zip"},
		{ID: "audio-transfer", FileID: "sound", Name: "录音"},
		{ID: "old-transfer", FileID: "missing", Name: "历史文档.pdf"},
	}
	h := panelTestNewHarness(t, panelModuleTransfers, state)
	want := map[string]*gfx.Icon{"directory-transfer": &fileglyph.Folder, "audio-transfer": &fileglyph.Audio, "old-transfer": &fileglyph.Pdf}
	for _, row := range h.panels.transferRows {
		if row.icon != want[row.transfer.ID] {
			t.Errorf("incorrect transfer icon for %q", row.transfer.ID)
		}
		delete(want, row.transfer.ID)
	}
	if len(want) > 0 {
		t.Fatalf("missing transfer rows: %v", want)
	}
}

func TestTransferRateAndInterruptedMenu(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Transfers = []domain.Transfer{
		{ID: "live", Name: "active.bin", Direction: "receive", Status: "transferring", Size: 10000, Done: 1000, Speed: 2048},
		{ID: "stopped", Name: "interrupted.bin", Direction: "receive", Status: "failed", Size: 10000, Done: 1000},
	}
	h := panelTestNewHarness(t, panelModuleTransfers, state)
	if !h.hasText(transferRateLabel(state.Transfers[0])) {
		t.Fatal("active transfer rate is missing from the table")
	}
	h.clickLabelRole(h.panels.transferLabels[panelTransferFinished], gfx.RoleTab)
	entry := h.panels.transferFinished[0]
	h.click(entry.ids.more)
	if h.hasText("清理传输临时文件") || !h.hasText("复制文件名") {
		t.Fatal("interrupted transfer menu still exposes manual cleanup or did not open")
	}
}
