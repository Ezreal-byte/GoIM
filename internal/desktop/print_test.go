package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	vivaheadless "github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func printTestState(t *testing.T) domain.State {
	t.Helper()
	now := time.Now()
	settings := domain.DefaultSettings(t.TempDir())
	settings.Nickname = "我"
	settings.LastSection = "print"
	capabilities := domain.PrintCapabilities{
		Papers:            []string{"A4", "A3"},
		Orientations:      []string{domain.PrintOrientationPortrait, domain.PrintOrientationLandscape},
		ColorModes:        []string{domain.PrintColorMonochrome, domain.PrintColorColor},
		DuplexModes:       []string{domain.PrintDuplexSimplex, domain.PrintDuplexLongEdge},
		SupportsPageRange: true,
	}
	return domain.State{
		Self: domain.Peer{ID: "self", Name: "我", Online: true},
		Peers: []domain.Peer{{
			ID: "owner", Name: "陈晨", Online: true, LastSeen: now,
		}},
		Conversations: []domain.Conversation{{
			ID: "conv-print", Kind: "direct", PeerID: "owner", Title: "陈晨", UpdatedAt: now,
		}},
		Messages: []domain.Message{{
			ID: "message-print", ConversationID: "conv-print", SenderID: "self",
			Kind: "print", Text: "会议议程.pdf", PrintJobID: "job-print", CreatedAt: now,
		}},
		LocalPrinters: []domain.LocalPrinter{{
			ID: "HP Front Desk", Name: "HP Front Desk", Available: true, Default: true, Capabilities: capabilities,
		}},
		Printers: []domain.PrinterShare{{
			ID: "printer-remote", OwnerID: "owner", OwnerName: "陈晨", Name: "陈晨-前台 HP",
			DeviceName: "HP Front Desk", Available: true, Capabilities: capabilities, CreatedAt: now,
		}},
		PrintJobs: []domain.PrintJob{{
			ID: "job-print", MessageID: "message-print", ConversationID: "conv-print",
			PrinterID: "printer-remote", PrinterName: "陈晨-前台 HP", PrinterOwnerID: "owner",
			PrinterOwnerName: "陈晨", SenderID: "self", SenderName: "我", FileName: "会议议程.pdf",
			FileSize: 438_272, MIME: "application/pdf", Status: domain.PrintStatusQueued,
			Options: domain.PrintOptions{Paper: "A4", Copies: 1, Orientation: domain.PrintOrientationPortrait,
				Color: domain.PrintColorMonochrome, Duplex: domain.PrintDuplexLongEdge},
			CreatedAt: now, UpdatedAt: now,
		}},
		Settings: settings, Revision: 1,
	}
}

func printTexts(snapshot app.Snapshot) []string {
	values := make([]string, 0, len(snapshot.Texts)+len(snapshot.Labels))
	for _, text := range snapshot.Texts {
		values = append(values, text.Value)
	}
	for _, hit := range snapshot.Labels {
		values = append(values, hit.Value)
	}
	return values
}

func requirePrintTexts(t *testing.T, values []string, wanted ...string) {
	t.Helper()
	for _, want := range wanted {
		found := false
		for _, value := range values {
			found = found || strings.Contains(value, want)
		}
		if !found {
			t.Fatalf("print UI is missing %q; texts=%q", want, values)
		}
	}
}

func TestPrintPanelCardsRecordsAndShareDialog(t *testing.T) {
	state := printTestState(t)
	h := panelTestNewHarness(t, panelModulePrint, state)
	h.panels.presentPrint = func(string) bool { return true }

	requirePrintTexts(t, printTexts(h.driver.Snapshot()), "打印共享", "陈晨-前台 HP", "共享人", "陈晨", "可用", "A4 / A3 · 双面", "已提交队列")
	h.clickLabelRole("打印", gfx.RoleButton)
	h.click(h.panels.print.ids.openShare)
	requirePrintTexts(t, printTexts(h.driver.Snapshot()), "分享打印机", "本机打印机", "共享名称", "我-HP Front Desk")
	if got := h.panels.print.shareName.Text(); got != "我-HP Front Desk" {
		t.Fatalf("default share name=%q", got)
	}
	h.click(h.panels.print.ids.shareSubmit)
	command := h.requireLastCommand()
	if command.Kind != domain.SharePrinter || command.PrinterID != "HP Front Desk" || command.Name != "我-HP Front Desk" {
		t.Fatalf("share command=%+v", command)
	}
	h.requireLayout()
}

func TestPrintIndependentWindowSubmitsSelectedFile(t *testing.T) {
	state := printTestState(t)
	backend := newShellTestBackend(state)
	view := newPrintWindowView(backend, "printer-remote")
	t.Cleanup(view.Close)

	var commands []domain.Command
	view.avatar = func(f ui.C, _, _, _ string, _ float32, _ bool) {}
	view.submit = func(command domain.Command) { commands = append(commands, command) }
	view.close = func() {}
	path := filepath.Join(t.TempDir(), "会议议程.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n%GoIM"), 0o600); err != nil {
		t.Fatal(err)
	}
	view.setFile(path)
	view.paper, view.orientation = "A3", domain.PrintOrientationLandscape
	view.color, view.duplex = domain.PrintColorColor, domain.PrintDuplexSimplex
	view.pageMode = "custom"
	view.pageRange.SetText("1-3,5")
	view.copies.SetText("2")
	view.sendJob(view.printer())

	if len(commands) != 1 {
		t.Fatalf("submitted %d print commands", len(commands))
	}
	command := commands[0]
	options := command.PrintOptions
	if command.Kind != domain.PrintFile || command.PrinterID != "printer-remote" || len(command.Paths) != 1 {
		t.Fatalf("print command=%+v", command)
	}
	if options.Paper != "A3" || options.Copies != 2 || options.Orientation != domain.PrintOrientationLandscape ||
		options.PageRange != "1-3,5" || options.Color != domain.PrintColorColor || options.Duplex != domain.PrintDuplexSimplex {
		t.Fatalf("print options=%+v", options)
	}
}

func TestPrintWindowRendersControlsAndThemes(t *testing.T) {
	for _, themeName := range []string{"light", "dark"} {
		t.Run(themeName, func(t *testing.T) {
			state := printTestState(t)
			state.Settings.Theme = themeName
			backend := newShellTestBackend(state)
			view := newPrintWindowView(backend, "printer-remote")
			t.Cleanup(view.Close)
			driver, err := app.NewDriver(view.Config(), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(driver.Close)
			values := printTexts(driver.Snapshot())
			requirePrintTexts(t, values, "打印文档", "陈晨-前台 HP", "共享人 陈晨", "可用",
				"打印文件", "打印参数", "纸张", "份数", "方向", "页面范围", "色彩", "双面", "发送打印")
			for _, problem := range vivaheadless.CheckLayout(driver.Snapshot()) {
				if problem.Kind != vivaheadless.LayoutClipped {
					t.Fatalf("print window layout %s: %s", problem.Kind, problem.Detail)
				}
			}
			if _, ok := findPrintHit(driver.Snapshot(), view.ids.send); !ok {
				t.Fatal("print send control is missing")
			}
		})
	}
}

func TestChatShowsPrintRecord(t *testing.T) {
	state := printTestState(t)
	state.Settings.LastSection = "messages"
	h := newShellTestHarness(t, state)
	requirePrintTexts(t, printTexts(h.snapshot()), "会议议程.pdf", "打印机 陈晨-前台 HP", "已提交队列")
}

func TestPrintScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	backend := newShellTestBackend(printTestState(t))
	shell := New(backend)
	session, err := vivaheadless.Open(shell.Config(), nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		shell.Close()
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	t.Cleanup(shell.Close)
	if err := session.Wait(700 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := session.Screenshot(filepath.Join(directory, "print-sharing-light.png")); err != nil {
		t.Fatal(err)
	}
	shellQAClick(t, session, shell.ids.navMessages)
	if err := session.Wait(700 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := session.Screenshot(filepath.Join(directory, "print-chat-record.png")); err != nil {
		t.Fatal(err)
	}
	shellQAClick(t, session, shell.ids.navPrint)
	shell.panels.print.shareOpen = true
	shell.panels.print.applyShareDefault(shell.state)
	shell.invalidator.Invalidate()
	if err := session.Wait(700 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := session.Screenshot(filepath.Join(directory, "print-share-dialog.png")); err != nil {
		t.Fatal(err)
	}
	shell.panels.print.closeShareDialog()
	shell.panels.print.tab = printTabRecords
	shell.invalidator.Invalidate()
	if err := session.Wait(700 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := session.Screenshot(filepath.Join(directory, "print-records.png")); err != nil {
		t.Fatal(err)
	}
	shell.panels.print.tab = printTabMine
	shell.invalidator.Invalidate()
	if err := session.Wait(700 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := session.Screenshot(filepath.Join(directory, "print-my-shares.png")); err != nil {
		t.Fatal(err)
	}
	session.Close()

	darkBackend := newShellTestBackend(printTestState(t))
	darkBackend.state.Settings.Theme = "dark"
	darkShell := New(darkBackend)
	darkSession, err := vivaheadless.Open(darkShell.Config(), nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		darkShell.Close()
		t.Fatal(err)
	}
	t.Cleanup(darkSession.Close)
	t.Cleanup(darkShell.Close)
	if err := darkSession.Wait(700 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := darkSession.Screenshot(filepath.Join(directory, "print-sharing-dark.png")); err != nil {
		t.Fatal(err)
	}

	printBackend := newShellTestBackend(printTestState(t))
	printView := newPrintWindowView(printBackend, "printer-remote")
	printWindow, err := vivaheadless.Open(printView.Config(), nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		printView.Close()
		t.Fatal(err)
	}
	t.Cleanup(printWindow.Close)
	t.Cleanup(printView.Close)
	if err := printWindow.Wait(700 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := printWindow.Screenshot(filepath.Join(directory, "print-window.png")); err != nil {
		t.Fatal(err)
	}
}

func findPrintHit(snapshot app.Snapshot, id gfx.ID) (gfx.Hit, bool) {
	for _, hit := range snapshot.Hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}
