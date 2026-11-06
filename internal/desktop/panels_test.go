package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	vivaheadless "github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

type panelTestHarness struct {
	t        *testing.T
	panels   *Panels
	tree     ui.Tree
	driver   *app.Driver
	module   string
	state    domain.State
	theme    ui.Theme
	bounds   ui.Rect
	commands []domain.Command
}

func panelTestDefaultState(t *testing.T) domain.State {
	t.Helper()
	return domain.State{
		Self:     domain.Peer{ID: "self", Name: "我"},
		Settings: domain.DefaultSettings(t.TempDir()),
		Version:  "test-build",
		Revision: 1,
	}
}

func panelTestNewHarness(t *testing.T, module string, state domain.State) *panelTestHarness {
	t.Helper()
	return panelTestNewHarnessBounds(t, module, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 960,
	})
}

func panelTestNewHarnessBounds(t *testing.T, module string, state domain.State, bounds ui.Rect) *panelTestHarness {
	t.Helper()
	h := &panelTestHarness{
		t:      t,
		panels: &Panels{},
		module: module,
		state:  state,
		theme:  ui.LightTheme(),
		bounds: bounds,
	}
	h.panels.Init(&h.tree)
	driver, err := app.NewDriver(app.Config{
		Title:  "panel driver test",
		Width:  int(bounds.X + bounds.Width),
		Height: int(bounds.Height),
		Tree:   &h.tree,
		Paint: func(canvas *ui.Canvas, _ ui.Size) {
			h.panels.Draw(canvas, h.theme, h.bounds, h.module, h.state, h.submit)
		},
		OnKey:      h.panels.Key,
		OnShortcut: h.panels.Shortcut,
		OnScroll:   h.panels.Wheel,
		TextInput:  h.panels.TextInput(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.driver = driver
	t.Cleanup(driver.Close)
	return h
}

func (h *panelTestHarness) submit(command domain.Command) {
	h.commands = append(h.commands, command)
}

func (h *panelTestHarness) clearCommands() {
	h.commands = nil
}

func (h *panelTestHarness) findHit(id ui.ID) (ui.Hit, bool) {
	for _, hit := range h.driver.Snapshot().Hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return ui.Hit{}, false
}

func (h *panelTestHarness) click(id ui.ID) {
	h.t.Helper()
	hit, ok := h.findHit(id)
	if !ok {
		h.t.Fatalf("control %v is not on screen", id)
	}
	bounds := hit.Bounds
	if err := h.driver.Click(bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2); err != nil {
		h.t.Fatal(err)
	}
}

func (h *panelTestHarness) hasText(value string) bool {
	h.t.Helper()
	for _, label := range h.driver.Snapshot().Labels {
		if strings.Contains(label.Value, value) {
			return true
		}
	}
	for _, hit := range h.driver.Snapshot().Hits {
		if strings.Contains(hit.Label, value) {
			return true
		}
	}
	return false
}

func (h *panelTestHarness) requireLastCommand() domain.Command {
	h.t.Helper()
	if len(h.commands) == 0 {
		h.t.Fatal("no command was submitted")
	}
	return h.commands[len(h.commands)-1]
}

func TestPanelSettingsLayoutAndCategorySwitch(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarness(t, panelModuleSettings, state)

	if !h.hasText("桌面通知") || !h.hasText("主题") {
		t.Fatal("general settings are not visible")
	}
	snapshot := h.driver.Snapshot()
	for _, hit := range snapshot.Hits {
		if hit.Bounds.X < h.bounds.X-0.01 ||
			hit.Bounds.Y < h.bounds.Y-0.01 ||
			hit.Bounds.X+hit.Bounds.Width > h.bounds.X+h.bounds.Width+0.01 ||
			hit.Bounds.Y+hit.Bounds.Height > h.bounds.Y+h.bounds.Height+0.01 {
			t.Fatalf("control %v is drawn outside supplied bounds: %+v, bounds %+v", hit.ID, hit.Bounds, h.bounds)
		}
	}
	for _, problem := range vivaheadless.CheckLayout(snapshot) {
		if problem.Kind != vivaheadless.LayoutClipped {
			t.Fatalf("layout %s at %+v for %q: %s", problem.Kind, problem.Bounds, problem.Text, problem.Detail)
		}
	}

	category, ok := h.findHit(h.panels.ids.settingsCategories.At(0))
	if !ok {
		t.Fatal("general category is not interactive")
	}
	if category.Bounds.X < h.bounds.X || category.Bounds.X+category.Bounds.Width > h.bounds.X+panelSettingsWidth+0.01 {
		t.Fatalf("category control is outside the 300px rail: %+v", category.Bounds)
	}
	contentFound := false
	for _, label := range snapshot.Labels {
		if label.Value == "主题" {
			contentFound = true
			if label.Bounds.X <= h.bounds.X+panelSettingsWidth {
				t.Fatalf("settings content starts inside the category rail: %+v", label.Bounds)
			}
			break
		}
	}
	if !contentFound {
		t.Fatal("theme row is not visible")
	}
	beforeScroll := h.panels.settingsScroll[panelSettingsGeneral]
	if err := h.driver.Scroll(ui.ScrollEvent{
		X: h.bounds.X + panelSettingsWidth + 400, Y: h.bounds.Y + 500,
		DeltaY: 240, Precise: true,
	}); err != nil {
		t.Fatal(err)
	}
	if h.panels.settingsScroll[panelSettingsGeneral] <= beforeScroll {
		t.Fatal("wheel input did not scroll the settings content")
	}

	h.click(h.panels.ids.settingsCategories.At(1))
	if !h.hasText("恢复默认") || !h.hasText("点击更改后录制组合键") {
		t.Fatal("shortcuts page did not replace the general page")
	}
	if h.hasText("桌面通知") {
		t.Fatal("general settings remained visible after switching category")
	}

	h.click(h.panels.ids.settingsCategories.At(2))
	if !h.hasText("应用版本") || !h.hasText("复制版本信息") || !h.hasText("开源许可") {
		t.Fatal("about page is incomplete")
	}
	if h.hasText("桌面通知") {
		t.Fatal("general settings remained visible on the about page")
	}
	h.click(h.panels.ids.aboutCopyVersion)
	if clipboard := h.driver.Clipboard(); !strings.Contains(clipboard, "GoIM test-build") ||
		!strings.Contains(clipboard, "Go ") || !strings.Contains(clipboard, "Viva ") {
		t.Fatalf("version copy = %q", clipboard)
	}
	h.click(h.panels.ids.aboutLicense)
	if !h.hasText("GoIM 应用许可证") {
		t.Fatal("license page did not open")
	}
	h.click(h.panels.ids.aboutCopyLicense)
	if clipboard := h.driver.Clipboard(); !strings.Contains(clipboard, "GoIM 应用许可证") {
		t.Fatalf("license copy = %q", clipboard)
	}
	if provider := h.panels.TextInput(); provider == nil {
		t.Fatal("TextInput returned a nil provider")
	} else if provider(app.TextInputContext{}) != nil {
		t.Fatal("panels unexpectedly supplied native text editing")
	}
}

func TestPanelSettingsSubmitsSettingCommands(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1500,
	})

	h.click(h.panels.ids.themeChoice.At(1))
	command := h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "theme" || command.Value != "dark" {
		t.Fatalf("theme command = %+v", command)
	}

	h.clearCommands()
	h.click(h.panels.ids.settingsNotification)
	command = h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "desktop_notifications" || command.Bool {
		t.Fatalf("notification command = %+v", command)
	}

	h.clearCommands()
	h.click(h.panels.ids.duplicateChoice.At(len(panelDuplicateLabels)))
	h.click(h.panels.ids.duplicateChoice.At(1))
	command = h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "duplicate_policy" || command.Value != "ask" {
		t.Fatalf("duplicate-policy command = %+v", command)
	}

	h.clearCommands()
	h.click(h.panels.ids.sendKeyChoice.At(1))
	command = h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "send_key" || command.Value != "Ctrl+Enter" {
		t.Fatalf("send-key command = %+v", command)
	}

	h.clearCommands()
	h.click(h.panels.ids.settingsSound)
	command = h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "sound" || !command.Bool {
		t.Fatalf("sound command = %+v", command)
	}

	h.clearCommands()
	h.click(h.panels.ids.settingsAutoAccept)
	command = h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "auto_accept" || !command.Bool {
		t.Fatalf("auto-accept command = %+v", command)
	}

	h.clearCommands()
	h.click(h.panels.ids.settingsCloseToTray)
	command = h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "close_to_tray" || command.Bool {
		t.Fatalf("close-to-tray command = %+v", command)
	}

	networkTrigger := h.panels.ids.networkChoice.At(len(h.panels.networkValues))
	h.click(networkTrigger)
	if !h.panels.networkSelect.Open {
		t.Fatal("network select did not open")
	}
	if err := h.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if h.panels.networkSelect.Open {
		t.Fatal("Escape did not close the network select")
	}
	h.click(networkTrigger)
	h.clearCommands()
	h.click(h.panels.ids.networkChoice.At(0))
	command = h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "network" || command.Value != "auto" {
		t.Fatalf("network command = %+v", command)
	}
}

func TestPanelSettingsDirectoryPickerSuccessAndCancel(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1080,
	})
	directory := t.TempDir()
	if err := h.driver.QueueOpenDirectory(directory, nil); err != nil {
		t.Fatal(err)
	}

	h.click(h.panels.ids.settingsChooseDir)
	command := h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "download_dir" || command.Value != directory {
		t.Fatalf("directory command = %+v", command)
	}
	request, ok := h.driver.LastOpenDirectoryRequest()
	if !ok || request.Title == "" {
		t.Fatalf("directory picker request = %+v, observed %v", request, ok)
	}

	h.clearCommands()
	if err := h.driver.QueueOpenDirectory("", app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	h.click(h.panels.ids.settingsChooseDir)
	if len(h.commands) != 0 {
		t.Fatalf("canceling the directory picker submitted commands: %+v", h.commands)
	}
	if h.panels.localError != "" {
		t.Fatalf("canceling the directory picker produced an error: %s", h.panels.localError)
	}
}

func TestPanelShortcutRecordingConflictAndDefaults(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1080,
	})
	h.click(h.panels.ids.settingsCategories.At(1))
	if h.panels.shortcutEdit != -1 {
		t.Fatalf("a shortcut row started in recording mode: index %d", h.panels.shortcutEdit)
	}

	h.click(h.panels.ids.shortcutRecord.At(1))
	if h.panels.shortcutEdit != 1 {
		t.Fatalf("recording did not start on the chosen row: index %d", h.panels.shortcutEdit)
	}
	handled, err := h.driver.ShortcutChord(ui.ModifierControl, 'f')
	if err != nil || !handled {
		t.Fatalf("recording Ctrl+F: handled=%v err=%v", handled, err)
	}
	if h.panels.shortcutKeys != "Ctrl+F" {
		t.Fatalf("recorded keys = %q", h.panels.shortcutKeys)
	}
	if !h.hasText("与“会话内搜索”冲突") {
		t.Fatal("conflicting shortcut was not explained")
	}

	h.clearCommands()
	if _, ok := h.findHit(h.panels.ids.shortcutSave.At(1)); ok {
		h.click(h.panels.ids.shortcutSave.At(1))
	}
	if len(h.commands) != 0 {
		t.Fatalf("conflicting shortcut was saved: %+v", h.commands)
	}

	handled, err = h.driver.ShortcutChord(ui.ModifierControl|ui.ModifierShift, 'x')
	if err != nil || !handled {
		t.Fatalf("recording Ctrl+Shift+X: handled=%v err=%v", handled, err)
	}
	h.clearCommands()
	h.click(h.panels.ids.shortcutSave.At(1))
	command := h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "shortcuts" {
		t.Fatalf("shortcut save command = %+v", command)
	}
	var shortcuts []domain.Shortcut
	if err := json.Unmarshal([]byte(command.Value), &shortcuts); err != nil {
		t.Fatalf("shortcut JSON: %v\n%s", err, command.Value)
	}
	if len(shortcuts) != len(state.Settings.Shortcuts) || shortcuts[1].Keys != "Ctrl+Shift+X" {
		t.Fatalf("saved shortcuts = %+v", shortcuts)
	}
	if h.panels.shortcutEdit != -1 {
		t.Fatal("shortcut recording remained active after save")
	}

	h.clearCommands()
	h.click(h.panels.ids.shortcutReset)
	if !h.panels.resetShortcuts || !h.hasText("恢复默认只会重置快捷键") {
		t.Fatal("reset confirmation was not shown")
	}
	h.click(h.panels.ids.shortcutConfirmReset)
	command = h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "reset_shortcuts" {
		t.Fatalf("shortcut reset command = %+v", command)
	}
}

func panelTestTransferIndex(entries []panelTransferEntry, id string) int {
	for index, entry := range entries {
		if entry.transfer.ID == id {
			return index
		}
	}
	return -1
}

func TestPanelTransfersSubmitActionsAndOpenCompletedFiles(t *testing.T) {
	completedPath := filepath.Join(t.TempDir(), "received.txt")
	if err := os.WriteFile(completedPath, []byte("received"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	state := panelTestDefaultState(t)
	state.Peers = []domain.Peer{
		{ID: "peer-a", Name: "Alice"},
		{ID: "peer-b", Name: "Bob"},
	}
	state.Transfers = []domain.Transfer{
		{ID: "receive-offer", PeerID: "peer-a", Direction: "receive", Status: "offered", Name: "report.txt", Size: 100, UpdatedAt: now},
		{ID: "sending", PeerID: "peer-b", Direction: "send", Status: "transferring", Name: "photo.png", Size: 1000, Done: 250, Speed: 100, UpdatedAt: now.Add(-time.Minute)},
		{ID: "completed", PeerID: "peer-a", Direction: "receive", Status: "completed", Name: "received.txt", Size: 8, Done: 8, Path: completedPath, UpdatedAt: now.Add(-2 * time.Minute)},
		{ID: "failed", PeerID: "peer-b", Direction: "send", Status: "failed", Name: "broken.bin", Size: 50, Error: "源文件不可用", UpdatedAt: now.Add(-3 * time.Minute)},
	}
	state.Revision = 2
	h := panelTestNewHarnessBounds(t, panelModuleTransfers, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1120,
	})
	if len(h.panels.transferPending) != 2 || len(h.panels.transferFinished) != 2 {
		t.Fatalf("transfer groups: pending=%d finished=%d", len(h.panels.transferPending), len(h.panels.transferFinished))
	}
	receiveIndex := panelTestTransferIndex(h.panels.transferPending, "receive-offer")
	sendIndex := panelTestTransferIndex(h.panels.transferPending, "sending")
	if receiveIndex < 0 || sendIndex < 0 {
		t.Fatal("pending transfers are missing")
	}
	if h.panels.transferPending[receiveIndex].peer != "Alice" || h.panels.transferPending[sendIndex].peer != "Bob" {
		t.Fatal("per-recipient transfer labels are wrong")
	}

	savePath := filepath.Join(t.TempDir(), "chosen.txt")
	if err := h.driver.QueueSaveFile(savePath, nil); err != nil {
		t.Fatal(err)
	}
	h.click(h.panels.ids.transferSaveAs.At(receiveIndex))
	command := h.requireLastCommand()
	if command.Kind != domain.AcceptTransfer || command.TransferID != "receive-offer" ||
		len(command.Paths) != 1 || command.Paths[0] != savePath {
		t.Fatalf("save-as command = %+v", command)
	}
	if request, ok := h.driver.LastSaveFileRequest(); !ok || request.SuggestedName != "report.txt" {
		t.Fatalf("save picker request = %+v, observed %v", request, ok)
	}

	h.clearCommands()
	h.click(h.panels.ids.transferAccept.At(receiveIndex))
	command = h.requireLastCommand()
	if command.Kind != domain.AcceptTransfer || command.TransferID != "receive-offer" || len(command.Paths) != 0 {
		t.Fatalf("accept command = %+v", command)
	}

	h.clearCommands()
	h.click(h.panels.ids.transferReject.At(receiveIndex))
	command = h.requireLastCommand()
	if command.Kind != domain.RejectTransfer || command.TransferID != "receive-offer" {
		t.Fatalf("reject command = %+v", command)
	}

	h.clearCommands()
	h.click(h.panels.ids.transferCancel.At(sendIndex))
	command = h.requireLastCommand()
	if command.Kind != domain.CancelTransfer || command.TransferID != "sending" {
		t.Fatalf("cancel command = %+v", command)
	}

	h.click(h.panels.ids.transferFilters.At(panelTransferFinished))
	if h.panels.transferFilter != panelTransferFinished {
		t.Fatal("completed transfer filter did not change")
	}
	completedIndex := panelTestTransferIndex(h.panels.transferFinished, "completed")
	failedIndex := panelTestTransferIndex(h.panels.transferFinished, "failed")
	if completedIndex < 0 || failedIndex < 0 {
		t.Fatal("finished transfers are missing")
	}

	h.click(h.panels.ids.transferOpen.At(completedIndex))
	if path, ok := h.driver.LastOpenedPath(); !ok || path != completedPath {
		t.Fatalf("opened path = %q, observed %v", path, ok)
	}
	h.click(h.panels.ids.transferReveal.At(completedIndex))
	if path, ok := h.driver.LastRevealedPath(); !ok || path != completedPath {
		t.Fatalf("revealed path = %q, observed %v", path, ok)
	}

	h.clearCommands()
	h.click(h.panels.ids.transferRetry.At(failedIndex))
	command = h.requireLastCommand()
	if command.Kind != domain.RetryTransfer || command.TransferID != "failed" {
		t.Fatalf("retry command = %+v", command)
	}
}

func panelTestShareIndex(entries []panelShareEntry, id string) int {
	for index, entry := range entries {
		if entry.file.ID == id {
			return index
		}
	}
	return -1
}

func TestPanelSharesAddFilterGetRemoveAndOpen(t *testing.T) {
	root := t.TempDir()
	ownPath := filepath.Join(root, "own.txt")
	if err := os.WriteFile(ownPath, []byte("own"), 0o600); err != nil {
		t.Fatal(err)
	}
	addOne := filepath.Join(root, "add-one.txt")
	addTwo := filepath.Join(root, "add-two.txt")
	for _, path := range []string{addOne, addTwo} {
		if err := os.WriteFile(path, []byte("share"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	state := panelTestDefaultState(t)
	state.Peers = []domain.Peer{{ID: "peer-a", Name: "Alice"}}
	state.Files = []domain.File{
		{ID: "own-share", Name: "own.txt", Size: 3, SenderID: "self", SourcePath: ownPath, Shared: true, CreatedAt: now},
		{ID: "remote-share", Name: "remote.txt", Size: 5, SenderID: "peer-a", Shared: true, CreatedAt: now.Add(-time.Minute)},
		{ID: "private", Name: "private.txt", Shared: false, CreatedAt: now.Add(-2 * time.Minute)},
	}
	state.Revision = 2
	h := panelTestNewHarness(t, panelModuleShares, state)
	if len(h.panels.sharesAll) != 2 || h.panels.shareLabels[1] != "我分享的 1" || h.panels.shareLabels[2] != "他人分享 1" {
		t.Fatalf("share groups = %v, labels = %v", len(h.panels.sharesAll), h.panels.shareLabels)
	}
	ownIndex := panelTestShareIndex(h.panels.sharesAll, "own-share")
	remoteIndex := panelTestShareIndex(h.panels.sharesAll, "remote-share")
	if ownIndex < 0 || remoteIndex < 0 {
		t.Fatal("shared file rows are missing")
	}

	if err := h.driver.QueueOpenFiles([]string{addOne, addTwo}, nil); err != nil {
		t.Fatal(err)
	}
	h.click(h.panels.ids.sharesAdd)
	command := h.requireLastCommand()
	if command.Kind != domain.ShareFiles || len(command.Paths) != 2 ||
		command.Paths[0] != addOne || command.Paths[1] != addTwo {
		t.Fatalf("share-files command = %+v", command)
	}
	if request, ok := h.driver.LastOpenFilesRequest(); !ok || !request.AllowMultiple || request.Title == "" {
		t.Fatalf("open-files request = %+v, observed %v", request, ok)
	}

	h.clearCommands()
	if err := h.driver.QueueOpenFiles(nil, app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	h.click(h.panels.ids.sharesAdd)
	if len(h.commands) != 0 {
		t.Fatalf("canceling file selection submitted commands: %+v", h.commands)
	}

	h.click(h.panels.ids.sharesFilters.At(panelSharesMine))
	if h.panels.shareFilter != panelSharesMine {
		t.Fatal("own-shares filter did not change")
	}
	if _, ok := h.findHit(h.panels.ids.shareGet.At(0)); ok {
		t.Fatal("own share was rendered with a remote-only Get action")
	}
	h.click(h.panels.ids.shareOpen.At(0))
	if path, ok := h.driver.LastOpenedPath(); !ok || path != ownPath {
		t.Fatalf("opened share path = %q, observed %v", path, ok)
	}
	h.click(h.panels.ids.shareReveal.At(0))
	if path, ok := h.driver.LastRevealedPath(); !ok || path != ownPath {
		t.Fatalf("revealed share path = %q, observed %v", path, ok)
	}
	h.clearCommands()
	h.click(h.panels.ids.shareRemove.At(0))
	command = h.requireLastCommand()
	if command.Kind != domain.RemoveShare || command.FileID != "own-share" {
		t.Fatalf("remove-share command = %+v", command)
	}

	h.click(h.panels.ids.sharesFilters.At(panelSharesRemote))
	if _, ok := h.findHit(h.panels.ids.shareRemove.At(0)); ok {
		t.Fatal("remote share was rendered with a Remove action")
	}
	h.clearCommands()
	h.click(h.panels.ids.shareGet.At(0))
	command = h.requireLastCommand()
	if command.Kind != domain.GetShared || command.FileID != "remote-share" {
		t.Fatalf("get-shared command = %+v", command)
	}

	h.clearCommands()
	h.click(h.panels.ids.sharesRefresh)
	command = h.requireLastCommand()
	if command.Kind != domain.RefreshShares {
		t.Fatalf("refresh-shares command = %+v", command)
	}
}
