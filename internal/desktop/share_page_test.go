package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/skip2/go-qrcode"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	vivaheadless "github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func TestShareListUsesNativePickersAndKeepsPasswordOptional(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarness(t, panelModuleShares, state)
	p := h.panels
	if h.hasText("本机文件") || h.hasText("输入文件夹路径") {
		t.Fatal("the old local browser is still in the files page")
	}
	a, b := filepath.Join(t.TempDir(), "资料.pdf"), filepath.Join(t.TempDir(), "清单.xlsx")
	if err := h.driver.QueueOpenFiles([]string{a, b}, nil); err != nil {
		t.Fatal(err)
	}
	h.click(p.ids.sharesAdd)
	h.click(p.ids.sharePickFiles)
	if options, ok := h.driver.LastOpenFilesRequest(); !ok || !options.AllowMultiple {
		t.Fatalf("files picker did not allow selection of more than one file: %+v", options)
	}
	if !p.shareDialog.open || p.shareDialog.folder || len(p.shareDialog.paths) != 2 || len(h.commands) != 0 {
		t.Fatalf("unexpected staged files: %+v", p.shareDialog)
	}
	h.click(p.ids.shareDialogPassword)
	if err := h.driver.Type("sample-password"); err != nil {
		t.Fatal(err)
	}
	h.click(p.ids.shareDialogSubmit)
	if command := h.requireLastCommand(); command.Kind != domain.ShareFiles || len(command.Paths) != 2 ||
		command.Paths[0] != a || command.Paths[1] != b || command.Text != "sample-password" {
		t.Fatalf("share command = %#v", command)
	}
	if err := h.driver.Advance(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}

	folder := t.TempDir()
	if err := h.driver.QueueOpenDirectory(folder, nil); err != nil {
		t.Fatal(err)
	}
	h.click(p.ids.sharesAdd)
	h.click(p.ids.sharePickFolder)
	if _, ok := h.driver.LastOpenDirectoryRequest(); !ok || !p.shareDialog.open || !p.shareDialog.folder {
		t.Fatal("directory picker did not stage a folder")
	}
	h.click(p.ids.shareDialogSubmit)
	if command := h.requireLastCommand(); command.Kind != domain.ShareFiles || len(command.Paths) != 1 ||
		command.Paths[0] != folder || command.Text != "" {
		t.Fatalf("folder share command = %#v", command)
	}
	if err := h.driver.Advance(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	h.clearCommands()
	if err := h.driver.QueueOpenDirectory("", app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	h.click(p.ids.sharesAdd)
	h.click(p.ids.sharePickFolder)
	if p.shareDialog.open || p.sharePickerOpen || len(h.commands) != 0 || p.shareToast.message.Title != "" {
		t.Fatal("canceling the picker had side effects")
	}
	h.requireLayout()
}

func TestShareProjectMenuAndTransientFeedback(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Files = []domain.File{{
		ID: "own", Name: "交付资料", ShareFolder: true, ShareEntryCount: 2, Size: 3200, SenderID: "self",
		Shared: true, ShareURL: "http://192.0.2.1/s/demo", CreatedAt: time.Now(),
	}}
	qr, err := qrcode.Encode(state.Files[0].ShareURL, qrcode.Medium, 224)
	if err != nil {
		t.Fatal(err)
	}
	state.Files[0].ShareQR = qr
	h := panelTestNewHarness(t, panelModuleShares, state)
	p := h.panels
	entry := p.sharesMine[0]
	first, _ := h.findHit(entry.rowID)
	h.click(entry.copyID)
	if h.driver.Clipboard() != entry.file.ShareURL || !h.hasText("分享链接已复制") {
		t.Fatal("copy did not use the clipboard and a toast")
	}
	h.click(p.ids.shareToastDismiss)
	if h.hasText("分享链接已复制") {
		t.Fatal("closing the toast left it visible")
	}
	state.ShareFeedback = domain.ShareFeedback{Sequence: 1, Title: "网页已接收文件", Detail: "交付稿.pdf"}
	h.publish(state)
	if !h.hasText("网页已接收文件") || !h.hasText("交付稿.pdf") {
		t.Fatal("the received file feedback is missing")
	}
	if now, _ := h.findHit(entry.rowID); first.Bounds != now.Bounds {
		t.Fatal("a toast shifted the share list")
	}
	if err := h.driver.Advance(components.ToastQueueTimeout + time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if h.hasText("网页已接收文件") {
		t.Fatalf("the toast did not expire at %v: %+v", h.driver.Now(), p.shareToast)
	}
	state.Revision++
	h.publish(state)
	if h.hasText("网页已接收文件") {
		t.Fatal("an unrelated wake replayed the same toast")
	}
	h.click(entry.moreID)
	if p.shareMenu.project != "own" || !h.hasText("取消分享") || !h.hasText("复制项目链接") {
		t.Fatal("project management menu is missing actions")
	}
	h.click(p.ids.shareMenuQR)
	deadline := time.Now().Add(2 * time.Second)
	for p.shareQR.loading && time.Now().Before(deadline) {
		select {
		case <-h.driver.Wakes():
			if err := h.driver.Pump(); err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Millisecond):
		}
	}
	if !p.shareQR.open || p.shareQR.loading || len(p.shareQR.bitmap.Pix) == 0 {
		t.Fatal("project QR did not decode")
	}
	h.click(p.ids.shareQRDismissButton)
	if err := h.driver.Advance(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	h.click(entry.moreID)
	h.click(p.ids.shareMenuPassword)
	h.click(p.ids.sharePasswordField)
	if err := h.driver.Type("new-password"); err != nil {
		t.Fatal(err)
	}
	h.click(p.ids.sharePasswordSubmit)
	if c := h.requireLastCommand(); c.Kind != domain.SetSharePassword || c.FileID != "own" || c.Text != "new-password" {
		t.Fatalf("password command = %#v", c)
	}
}

func TestShareDeviceMenuCopiesDistinctLinkAndOpensNativeInbox(t *testing.T) {
	state := panelTestDefaultState(t)
	state.DeviceShare = domain.DeviceShare{URL: "http://192.0.2.1/d/device", ReceiveDir: t.TempDir()}
	h := panelTestNewHarness(t, panelModuleShares, state)
	p := h.panels
	h.click(p.ids.deviceShareMenu)
	h.click(p.ids.deviceShareCopy)
	if h.driver.Clipboard() != state.DeviceShare.URL {
		t.Fatal("device menu copied the wrong URL")
	}
	h.click(p.ids.deviceShareMenu)
	h.click(p.ids.deviceShareInbox)
	h.waitMedia()
	if path, ok := h.driver.LastRevealedPath(); !ok || path != state.DeviceShare.ReceiveDir {
		t.Fatalf("inbox was not shown in the system file manager: %q", path)
	}
	h.click(p.ids.deviceShareMenu)
	h.click(p.ids.deviceShareStop)
	if command := h.requireLastCommand(); command.Kind != domain.SetDeviceShare || command.Bool {
		t.Fatalf("unexpected disable command: %#v", command)
	}
}

func filePageSampleState(t *testing.T) domain.State {
	state := panelTestDefaultState(t)
	state.DeviceShare = domain.DeviceShare{URL: "http://192.0.2.1/d/device"}
	state.Peers = []domain.Peer{{ID: "peer", Name: "林晓"}}
	now := time.Date(2026, 11, 10, 10, 24, 0, 0, time.Local)
	state.Files = []domain.File{
		{ID: "delivery", Name: "北岭计划 · 交付资料", Size: 1717986918, ShareEntryCount: 32, ShareFolder: true, SenderID: "self", Shared: true, ShareURL: "http://192.0.2.1/s/delivery", CreatedAt: now},
		{ID: "checklist", Name: "验收清单.xlsx", Size: 25395, SenderID: "self", Shared: true, ShareURL: "http://192.0.2.1/s/checklist", CreatedAt: now.Add(-47 * time.Minute)},
		{ID: "archive", Name: "设计归档", Size: 3650722201, ShareEntryCount: 128, ShareFolder: true, SenderID: "self", Shared: true, ShareProtected: true, ShareURL: "http://192.0.2.1/s/archive", CreatedAt: now.Add(-18 * time.Hour)},
		{ID: "preview", Name: "界面预览.png", Size: 2202009, SenderID: "self", Shared: true, ShareURL: "http://192.0.2.1/s/preview", CreatedAt: now.Add(-23 * time.Hour)},
		{ID: "peer1", Name: "会议记录.pdf", Size: 173001, SenderID: "peer", Shared: true, CreatedAt: now},
		{ID: "peer2", Name: "品牌素材.zip", Size: 1201234, SenderID: "peer", Shared: true, CreatedAt: now.Add(-time.Hour)},
	}
	state.Files[0].ShareQR, _ = qrcode.Encode(state.Files[0].ShareURL, qrcode.Medium, 224)
	return state
}

func TestShareListFitsMinimumWindowInBothThemes(t *testing.T) {
	state := filePageSampleState(t)
	h := panelTestNewHarnessBounds(t, panelModuleShares, state, ui.Rect{X: 68, Width: 652, Height: 520})
	for _, theme := range []ui.Theme{ui.LightTheme(), ui.DarkTheme()} {
		h.theme = theme
		h.wake.Invalidate()
		if err := h.driver.Pump(); err != nil {
			t.Fatal(err)
		}
		h.requireLayout()
	}
}

func TestShareListDesignScreenshots(t *testing.T) {
	dir := os.Getenv("GOIM_SHARE_LIST_QA")
	if dir == "" {
		t.Skip("set GOIM_SHARE_LIST_QA to save screenshots")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	state := filePageSampleState(t)
	backend := newShellTestBackend(state)
	shell := New(backend)
	defer shell.Close()
	cfg := shell.Config()
	cfg.Width, cfg.Height = 1440, 960
	session, err := vivaheadless.Open(cfg, nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
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
		t.Fatalf("missing control %v", id)
	}
	shot := func(name string) {
		t.Helper()
		if err := session.Move(380, 42); err != nil {
			t.Fatal(err)
		}
		if err := session.Wait(300 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := session.Expect("layout", "clean"); err != nil {
			t.Fatal(err)
		}
		if err := session.Screenshot(filepath.Join(dir, name+".png")); err != nil {
			t.Fatal(err)
		}
	}
	click(shell.ids.navShares)
	shot("shares-light")
	entry := shell.panels.sharesMine[0]
	click(entry.copyID)
	click(entry.moreID)
	shot("shares-project-menu")
	if err := session.Keys("esc"); err != nil {
		t.Fatal(err)
	}
	click(shell.panels.ids.sharesAdd)
	shot("shares-add-menu")
	if err := session.Keys("esc"); err != nil {
		t.Fatal(err)
	}
	if err := session.Resize(800, 560); err != nil {
		t.Fatal(err)
	}
	shot("shares-compact")
	if err := session.Resize(1440, 960); err != nil {
		t.Fatal(err)
	}
	state.Settings.Theme = "dark"
	state.Revision++
	backend.setState(state)
	shell.invalidator.Invalidate()
	if err := session.WaitFor(func(_ app.Snapshot) bool { return shell.isDark() }, time.Second); err != nil {
		t.Fatal(err)
	}
	shot("shares-dark")
	if session.HasText("输入文件夹路径") {
		t.Fatal("obsolete local directory field appeared in the full shell")
	}
}
