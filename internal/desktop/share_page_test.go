package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	vivaheadless "github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func (h *panelTestHarness) waitShareIdle() {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !h.panels.shareLoading {
			return
		}
		select {
		case <-h.driver.Wakes():
			if err := h.driver.Pump(); err != nil {
				h.t.Fatal(err)
			}
		case <-time.After(5 * time.Millisecond):
		}
	}
	h.t.Fatal("share browser did not settle")
}

func (h *panelTestHarness) waitShareQR() {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !h.panels.shareQR.loading {
			return
		}
		select {
		case <-h.driver.Wakes():
			if err := h.driver.Pump(); err != nil {
				h.t.Fatal(err)
			}
		case <-time.After(5 * time.Millisecond):
		}
	}
	h.t.Fatal("share QR decode did not settle")
}

func (h *panelTestHarness) rightClick(id ui.ID) {
	h.t.Helper()
	hit, ok := h.findHit(id)
	if !ok {
		h.t.Fatalf("control %v is not on screen", id)
	}
	bounds := hit.Bounds
	if err := h.driver.ClickButton(bounds.X+bounds.Width/2,
		bounds.Y+bounds.Height/2, ui.PointerButtonSecondary); err != nil {
		h.t.Fatal(err)
	}
}

func shareTestRowIndex(entries []shareDirEntry, name string) int {
	for index, entry := range entries {
		if entry.name == name {
			return index
		}
	}
	return -1
}

func shareTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if (x/8+y/8)%2 == 0 {
				source.Set(x, y, color.RGBA{A: 255})
			} else {
				source.Set(x, y, color.White)
			}
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, source); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func shareRequireUniqueHitIDs(t *testing.T, h *panelTestHarness) {
	t.Helper()
	seen := make(map[ui.ID]struct{})
	for _, hit := range h.driver.Snapshot().Hits {
		if hit.ID.IsZero() {
			continue
		}
		if _, exists := seen[hit.ID]; exists {
			t.Fatalf("control identity %v is painted more than once", hit.ID)
		}
		seen[hit.ID] = struct{}{}
	}
}

func TestShareBrowserListsFoldersAsynchronouslyAndNavigates(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarness(t, panelModuleTransfers, state)

	folder := filepath.Join(h.home, "资料")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{
		filepath.Join(h.home, "说明.txt"),
		filepath.Join(folder, "设计稿.png"),
	}
	for _, path := range files {
		if err := os.WriteFile(path, []byte("share"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	h.showModule(panelModuleShares)
	h.waitShareIdle()
	if h.panels.shareDir != h.home {
		t.Fatalf("initial share directory = %q, want %q", h.panels.shareDir, h.home)
	}
	folderIndex := shareTestRowIndex(h.panels.shareEntries, "资料")
	fileIndex := shareTestRowIndex(h.panels.shareEntries, "说明.txt")
	if folderIndex < 0 || fileIndex < 0 || folderIndex > fileIndex {
		t.Fatalf("directories should sort before files: %+v", h.panels.shareEntries)
	}
	h.click(h.panels.ids.shareLocalRows.At(folderIndex))
	h.waitShareIdle()
	if h.panels.shareDir != folder {
		t.Fatalf("folder navigation opened %q, want %q", h.panels.shareDir, folder)
	}
	if shareTestRowIndex(h.panels.shareEntries, "设计稿.png") < 0 {
		t.Fatal("folder contents were not listed")
	}

	h.click(h.panels.ids.shareUp)
	h.waitShareIdle()
	if h.panels.shareDir != h.home {
		t.Fatalf("up opened %q, want %q", h.panels.shareDir, h.home)
	}
	shareRequireUniqueHitIDs(t, h)
	h.requireLayout()
}

func TestShareBrowserDoesNotReadFilesystemDuringPaint(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarness(t, panelModuleTransfers, state)
	started := make(chan string, 1)
	release := make(chan struct{})
	h.panels.shareBrowser.readDir = func(path string) shareDirResult {
		started <- path
		<-release
		return shareDirResult{path: path, entries: nil}
	}
	h.showModule(panelModuleShares)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("the share browser did not start a detached directory read")
	}
	snapshot := h.driver.Snapshot()
	resizeDone := make(chan error, 1)
	go func() {
		resizeDone <- h.driver.Resize(snapshot.Width+1, snapshot.Height)
	}()
	select {
	case err := <-resizeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("repaint waited for the detached directory read")
	}
	close(release)
	h.waitShareIdle()
}

func TestShareBrowserErrorOffersDistinctReloadAndRetry(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarness(t, panelModuleTransfers, state)
	var reads atomic.Int32
	h.panels.shareBrowser.readDir = func(string) shareDirResult {
		reads.Add(1)
		return shareDirResult{err: "测试读取失败"}
	}

	h.showModule(panelModuleShares)
	h.waitShareIdle()
	h.requireLayout()
	shareRequireUniqueHitIDs(t, h)
	if _, ok := h.findHit(h.panels.ids.shareLocalReload); !ok {
		t.Fatal("folder refresh is missing in the error state")
	}
	if _, ok := h.findHit(h.panels.ids.shareLocalRetry); !ok {
		t.Fatal("folder retry is missing in the error state")
	}
	h.click(h.panels.ids.shareLocalRetry)
	h.waitShareIdle()
	if reads.Load() != 2 {
		t.Fatalf("retry performed %d directory reads, want 2", reads.Load())
	}
}

func TestShareBrowserQuickLocationsIncludeDownloadsDrivesAndRoot(t *testing.T) {
	downloads := t.TempDir()
	drivePath := t.TempDir()
	state := panelTestDefaultState(t)
	state.Settings.DownloadDir = downloads
	state.Revision = 2
	h := panelTestNewHarness(t, panelModuleShares, state)
	h.waitShareIdle()
	h.panels.shareBrowser.listDrives = func() []shareDirEntry {
		return []shareDirEntry{{name: "测试盘", path: drivePath, dir: true, size: -1}}
	}

	h.click(h.panels.ids.shareDownloads)
	h.waitShareIdle()
	if h.panels.shareDir != downloads {
		t.Fatalf("downloads shortcut opened %q, want %q", h.panels.shareDir, downloads)
	}

	h.click(h.panels.ids.shareDrives)
	h.waitShareIdle()
	if h.panels.shareLocationTitle != "此电脑" || len(h.panels.shareEntries) != 1 ||
		h.panels.shareEntries[0].path != drivePath {
		t.Fatalf("drive listing = title %q entries %+v", h.panels.shareLocationTitle, h.panels.shareEntries)
	}

	h.click(h.panels.ids.shareRoot)
	h.waitShareIdle()
	if h.panels.shareDir != shareRootPath(h.home) {
		t.Fatalf("root shortcut opened %q, want %q", h.panels.shareDir, shareRootPath(h.home))
	}
}

func TestShareSelectionOpensPasswordDialogAndSubmitsPathsWithoutPicker(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarness(t, panelModuleTransfers, state)
	first := filepath.Join(h.home, "first.txt")
	second := filepath.Join(h.home, "second.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte(path), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h.showModule(panelModuleShares)
	h.waitShareIdle()

	firstIndex := shareTestRowIndex(h.panels.shareEntries, "first.txt")
	secondIndex := shareTestRowIndex(h.panels.shareEntries, "second.txt")
	if firstIndex < 0 || secondIndex < 0 {
		t.Fatalf("test files were not listed: %+v", h.panels.shareEntries)
	}
	h.click(h.panels.ids.shareLocalRows.At(firstIndex))
	if h.panels.shareSelectionCount() != 1 {
		t.Fatalf("first click selected %d files", h.panels.shareSelectionCount())
	}
	if err := h.driver.KeyDown(ui.KeyControlLeft, ""); err != nil {
		t.Fatal(err)
	}
	h.click(h.panels.ids.shareLocalRows.At(secondIndex))
	if err := h.driver.KeyUp(ui.KeyControlLeft, ""); err != nil {
		t.Fatal(err)
	}
	if h.panels.shareSelectionCount() != 2 {
		t.Fatalf("control-click selected %d files", h.panels.shareSelectionCount())
	}

	h.click(h.panels.ids.sharesAdd)
	if !h.panels.shareDialog.open || !h.hasText("分享所选文件") {
		t.Fatal("sharing selected files did not open the in-app dialog")
	}
	shareRequireUniqueHitIDs(t, h)
	h.requireLayout()
	h.click(h.panels.ids.shareDialogPassword)
	if err := h.driver.Type("s3cret"); err != nil {
		t.Fatal(err)
	}
	h.clearCommands()
	h.click(h.panels.ids.shareDialogSubmit)
	command := h.requireLastCommand()
	if command.Kind != domain.ShareFiles || command.Text != "s3cret" ||
		len(command.Paths) != 2 || command.Paths[0] != first || command.Paths[1] != second {
		t.Fatalf("share-files command = %+v", command)
	}
	if _, ok := h.driver.LastOpenFilesRequest(); ok {
		t.Fatal("sharing from the local browser used a native file picker")
	}
	if h.panels.shareSelectionCount() != 0 {
		t.Fatal("selection survived after submitting the share")
	}
}

func TestShareLocalContextMenuCanShareTheSelectedFile(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarness(t, panelModuleTransfers, state)
	path := filepath.Join(h.home, "context-share.txt")
	if err := os.WriteFile(path, []byte("share"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.showModule(panelModuleShares)
	h.waitShareIdle()
	index := shareTestRowIndex(h.panels.shareEntries, "context-share.txt")
	if index < 0 {
		t.Fatalf("test file was not listed: %+v", h.panels.shareEntries)
	}

	h.rightClick(h.panels.ids.shareLocalRows.At(index))
	if !h.panels.shareMenu.open {
		t.Fatal("right click did not open the local file menu")
	}
	shareRequireUniqueHitIDs(t, h)
	h.requireLayout()
	if _, ok := h.findHit(h.panels.ids.shareMenuShare); !ok {
		t.Fatal("local file menu has no share action")
	}
	h.click(h.panels.ids.shareMenuShare)
	if !h.panels.shareDialog.open || len(h.panels.shareDialog.paths) != 1 ||
		h.panels.shareDialog.paths[0] != path {
		t.Fatalf("context-menu share dialog = %+v", h.panels.shareDialog)
	}
}

func TestShareMenuCopyQRPasswordAndUnshare(t *testing.T) {
	ownPath := filepath.Join(t.TempDir(), "own.txt")
	if err := os.WriteFile(ownPath, []byte("own"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := panelTestDefaultState(t)
	state.Files = []domain.File{{
		ID: "own-share", Name: "own.txt", Size: 3, SenderID: "self",
		SourcePath: ownPath, Shared: true, CreatedAt: time.Now(),
		ShareURL: "https://goim.test/s/own", ShareQR: shareTestPNG(t, 512, 512),
	}}
	state.Revision = 2
	h := panelTestNewHarness(t, panelModuleShares, state)
	h.waitShareIdle()
	shareRequireUniqueHitIDs(t, h)
	var decodes atomic.Int32
	h.panels.shareBrowser.decodeQR = func(data []byte, limit int) (ui.Bitmap, bool) {
		decodes.Add(1)
		return decodeShareQR(data, limit)
	}
	index := panelTestShareIndex(h.panels.sharesMine, "own-share")
	if index < 0 {
		t.Fatal("own share row is missing")
	}
	rowID := h.panels.ids.shareRow.At(index)

	h.rightClick(rowID)
	if !h.panels.shareMenu.open {
		t.Fatal("right click did not open the share context menu")
	}
	shareRequireUniqueHitIDs(t, h)
	h.requireLayout()
	h.click(h.panels.ids.shareMenuCopy)
	if clipboard := h.driver.Clipboard(); clipboard != "https://goim.test/s/own" {
		t.Fatalf("share link clipboard = %q", clipboard)
	}
	if h.panels.localNotice != "分享链接已复制" {
		t.Fatalf("copy notice = %q", h.panels.localNotice)
	}
	h.click(h.panels.ids.sharesNoticeOff)
	if h.panels.localNotice != "" {
		t.Fatal("copy notice was not dismissible")
	}

	h.rightClick(rowID)
	h.click(h.panels.ids.shareMenuQR)
	h.waitShareQR()
	shareRequireUniqueHitIDs(t, h)
	h.requireLayout()
	if !h.panels.shareQR.open || len(h.panels.shareQR.bitmap.Pix) == 0 {
		t.Fatalf("QR dialog did not receive a decoded bitmap: %+v", h.panels.shareQR)
	}
	if h.panels.shareQR.bitmap.Width > shareQRDecodeSize ||
		h.panels.shareQR.bitmap.Height > shareQRDecodeSize {
		t.Fatalf("QR bitmap is not small: %+v", h.panels.shareQR.bitmap)
	}
	if decodes.Load() != 1 {
		t.Fatalf("QR decode count = %d, want 1", decodes.Load())
	}
	h.click(h.panels.ids.shareQRDismissButton)

	h.rightClick(rowID)
	h.click(h.panels.ids.shareMenuQR)
	h.waitShareQR()
	if decodes.Load() != 1 {
		t.Fatalf("cached QR was decoded again: %d", decodes.Load())
	}
	h.click(h.panels.ids.shareQRDismissButton)

	h.rightClick(rowID)
	h.click(h.panels.ids.shareMenuPassword)
	if !h.panels.sharePassword.open {
		t.Fatal("password dialog did not open")
	}
	shareRequireUniqueHitIDs(t, h)
	h.requireLayout()
	h.click(h.panels.ids.sharePasswordField)
	if err := h.driver.Type("new-secret"); err != nil {
		t.Fatal(err)
	}
	h.clearCommands()
	h.click(h.panels.ids.sharePasswordSubmit)
	command := h.requireLastCommand()
	if command.Kind != domain.SetSharePassword || command.FileID != "own-share" ||
		command.Text != "new-secret" {
		t.Fatalf("set-share-password command = %+v", command)
	}

	h.rightClick(rowID)
	h.click(h.panels.ids.shareMenuUnshare)
	command = h.requireLastCommand()
	if command.Kind != domain.RemoveShare || command.FileID != "own-share" {
		t.Fatalf("unshare command = %+v", command)
	}
}

func TestSharePasswordEmptySubmitClearsPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "protected.txt")
	if err := os.WriteFile(path, []byte("protected"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := panelTestDefaultState(t)
	state.Files = []domain.File{{
		ID: "protected", Name: "protected.txt", SenderID: "self", SourcePath: path,
		Shared: true, ShareProtected: true, CreatedAt: time.Now(),
	}}
	state.Revision = 2
	h := panelTestNewHarness(t, panelModuleShares, state)
	h.waitShareIdle()
	index := panelTestShareIndex(h.panels.sharesMine, "protected")
	h.rightClick(h.panels.ids.shareRow.At(index))
	h.click(h.panels.ids.shareMenuPassword)
	h.clearCommands()
	h.click(h.panels.ids.sharePasswordSubmit)
	command := h.requireLastCommand()
	if command.Kind != domain.SetSharePassword || command.FileID != "protected" ||
		command.Text != "" {
		t.Fatalf("empty password command = %+v", command)
	}
}

func TestShareMenuKeepsMemberSharesReadOnly(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Peers = []domain.Peer{{ID: "peer-a", Name: "Alice"}}
	state.Files = []domain.File{{
		ID: "remote", Name: "remote.txt", SenderID: "peer-a", Shared: true,
		CreatedAt: time.Now(), ShareURL: "https://goim.test/s/remote",
	}}
	state.Revision = 2
	h := panelTestNewHarness(t, panelModuleShares, state)
	h.waitShareIdle()
	h.click(h.panels.ids.sharesFilters.At(panelSharesRemote))
	index := panelTestShareIndex(h.panels.sharesRemote, "remote")
	if index < 0 {
		t.Fatal("member share row is missing")
	}
	h.rightClick(h.panels.ids.shareRow.At(index))
	h.requireDisabled(h.panels.ids.shareMenuUnshare)
	h.requireDisabled(h.panels.ids.shareMenuPassword)
	h.clearCommands()
	if _, ok := h.findHit(h.panels.ids.shareMenuGet); !ok {
		t.Fatal("member share menu has no Get action")
	}
	h.click(h.panels.ids.shareMenuGet)
	command := h.requireLastCommand()
	if command.Kind != domain.GetShared || command.FileID != "remote" {
		t.Fatalf("get-shared command = %+v", command)
	}
}

func TestShareQRSmalformedBytesFallBackWithoutDecodingInPaint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad-qr.txt")
	if err := os.WriteFile(path, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := panelTestDefaultState(t)
	state.Files = []domain.File{{
		ID: "bad-qr", Name: "bad-qr.txt", SenderID: "self", SourcePath: path,
		Shared: true, CreatedAt: time.Now(), ShareQR: []byte("not a png"),
	}}
	state.Revision = 2
	h := panelTestNewHarness(t, panelModuleShares, state)
	h.waitShareIdle()
	index := panelTestShareIndex(h.panels.sharesMine, "bad-qr")
	h.rightClick(h.panels.ids.shareRow.At(index))
	h.click(h.panels.ids.shareMenuQR)
	h.waitShareQR()
	if !h.panels.shareQR.open || h.panels.shareQR.err == "" ||
		len(h.panels.shareQR.bitmap.Pix) != 0 {
		t.Fatalf("malformed QR state = %+v", h.panels.shareQR)
	}
}

func TestSharePageFitsMinimumWindowInBothThemes(t *testing.T) {
	for _, theme := range []ui.Theme{ui.LightTheme(), ui.DarkTheme()} {
		state := panelTestDefaultState(t)
		ownPath := filepath.Join(t.TempDir(), "own.txt")
		if err := os.WriteFile(ownPath, []byte("own"), 0o600); err != nil {
			t.Fatal(err)
		}
		state.Peers = []domain.Peer{{ID: "peer-a", Name: "Alice"}}
		state.Files = []domain.File{
			{
				ID: "own", Name: "own.txt", SenderID: "self", SourcePath: ownPath,
				Shared: true, ShareProtected: true, CreatedAt: time.Now(),
				ShareURL: "https://goim.test/s/own",
			},
			{
				ID: "peer", Name: "peer.txt", SenderID: "peer-a", Shared: true,
				CreatedAt: time.Now().Add(-time.Minute),
			},
		}
		state.Revision = 2
		h := panelTestNewHarnessBounds(t, panelModuleShares, state, ui.Rect{
			X: 68, Y: 48, Width: 932, Height: 652,
		})
		h.theme = theme
		h.wake.Invalidate()
		if err := h.driver.Pump(); err != nil {
			t.Fatal(err)
		}
		h.waitShareIdle()
		h.requireLayout()
		if !h.hasText("本机文件") || !h.hasText("共享") || !h.hasText("own.txt") {
			t.Fatal("two-column file browser is incomplete at the minimum size")
		}
		h.click(h.panels.ids.sharesFilters.At(panelSharesRemote))
		h.requireLayout()
		if !h.hasText("peer.txt") {
			t.Fatal("member shares are not visible at the minimum size")
		}
	}
}

func TestShareBrowserKeepsPeerSharesAccessible(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Peers = []domain.Peer{{ID: "peer-a", Name: "Alice"}}
	state.Files = []domain.File{
		{ID: "mine", Name: "mine.txt", SenderID: "self", Shared: true, CreatedAt: time.Now()},
		{ID: "peer", Name: "peer.txt", SenderID: "peer-a", Shared: true, CreatedAt: time.Now()},
	}
	state.Revision = 2
	h := panelTestNewHarness(t, panelModuleShares, state)
	if h.panels.shareFilter != panelSharesMine {
		t.Fatal("file page did not default to own shares")
	}
	h.click(h.panels.ids.sharesFilters.At(panelSharesRemote))
	if h.panels.shareFilter != panelSharesRemote {
		t.Fatal("member share tab did not open")
	}
	if !h.hasText("peer.txt") || h.panels.sharesRemote[0].file.ID != "peer" {
		t.Fatal("member share is not reachable from the secondary tab")
	}
}

func TestSharePageScreenshots(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "项目资料"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"产品说明.pdf", "设计素材.zip", "会议记录.txt"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte("preview"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := shellTestState(t)
	state.Files = append(state.Files, domain.File{
		ID: "share-design", Name: "设计素材.zip", Size: 7, SenderID: "self",
		SourcePath: filepath.Join(home, "设计素材.zip"), Shared: true, ShareProtected: true,
		CreatedAt: time.Now(), ShareURL: "https://goim.test/s/design",
	})
	backend := newShellTestBackend(state)
	shell := New(backend)
	shell.panels.shareBrowser.homeDir = func() (string, error) { return home, nil }
	session, err := vivaheadless.Open(shell.Config(), nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		shell.Close()
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	t.Cleanup(shell.Close)
	click := func(id ui.ID) {
		t.Helper()
		for _, hit := range session.Snapshot().Hits {
			if hit.ID == id {
				if err := session.Click(hit.Bounds.X+hit.Bounds.Width/2,
					hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("control %v is not on screen", id)
	}
	waitForText := func(session *vivaheadless.Session, want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if err := session.Driver().Pump(); err != nil {
				t.Fatal(err)
			}
			for _, text := range session.Snapshot().Texts {
				if strings.Contains(text.Value, want) {
					return
				}
			}
			select {
			case <-session.Driver().Wakes():
			case <-time.After(20 * time.Millisecond):
			}
		}
		var visible []string
		for _, text := range session.Snapshot().Texts {
			visible = append(visible, text.Value)
		}
		t.Fatalf("text %q did not appear in the file page; visible text: %v", want, visible)
	}
	click(shell.ids.navShares)
	waitForText(session, "产品说明.pdf")
	waitForText(session, "本地文件可用")
	if err := session.Screenshot(filepath.Join(directory, "files-page-light.png")); err != nil {
		t.Fatal(err)
	}
	if err := session.Resize(1000, 700); err != nil {
		t.Fatal(err)
	}
	if err := session.Screenshot(filepath.Join(directory, "files-page-minimum-light.png")); err != nil {
		t.Fatal(err)
	}

	state.Settings.Theme = "dark"
	darkBackend := newShellTestBackend(state)
	darkShell := New(darkBackend)
	darkShell.panels.shareBrowser.homeDir = func() (string, error) { return home, nil }
	darkSession, err := vivaheadless.Open(darkShell.Config(), nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		darkShell.Close()
		t.Fatal(err)
	}
	t.Cleanup(darkSession.Close)
	t.Cleanup(darkShell.Close)
	for _, hit := range darkSession.Snapshot().Hits {
		if hit.ID == darkShell.ids.navShares {
			if err := darkSession.Click(hit.Bounds.X+hit.Bounds.Width/2,
				hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	waitForText(darkSession, "产品说明.pdf")
	waitForText(darkSession, "本地文件可用")
	if err := darkSession.Screenshot(filepath.Join(directory, "files-page-dark.png")); err != nil {
		t.Fatal(err)
	}
}
