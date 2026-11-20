package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	vivaheadless "github.com/vivaui/viva/headless"
	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestOfflineVoiceActionShowsToast(t *testing.T) {
	state := shellTestState(t)
	state.Peers[0].Online = false
	state.Peers[0].VoiceSupported = true
	h := newShellTestHarness(t, state)
	h.ui.selectConversation("conv-alice")
	h.settle()
	h.clickID(h.ui.ids.chatHeaderCall)
	if !h.hasText("对方当前不在线") {
		t.Fatal("offline call action provided no feedback")
	}
	if hasCommandKind(h.backend.commandLog(), domain.StartVoiceCall) {
		t.Fatal("offline call was submitted")
	}
}

func TestUnsupportedOnlineVoiceActionShowsToast(t *testing.T) {
	state := shellTestState(t)
	state.Peers[0].Online = true
	state.Peers[0].Transport = domain.TransportIPMSG
	h := newShellTestHarness(t, state)
	h.ui.selectConversation("conv-alice")
	h.settle()
	h.clickID(h.ui.ids.chatHeaderCall)
	if !h.hasText("对方客户端版本不支持语音通话") {
		t.Fatal("unsupported online call action provided no feedback")
	}
	if hasCommandKind(h.backend.commandLog(), domain.StartVoiceCall) {
		t.Fatal("unsupported call was submitted")
	}
}

func printerExperienceState(t *testing.T) domain.State {
	state := printTestState(t)
	state.Printers[0].PasswordRequired = true
	state.Printers[0].AuthRevision = "revision-one"
	state.Printers[0].Name = "陈晨-前台 Microsoft Office LaserJet 彩色双面打印机"
	own := state.Printers[0]
	own.ID, own.OwnerID, own.OwnerName = "printer-own", "self", "我"
	state.Printers = append(state.Printers, own)
	return state
}

func TestPrinterAdaptiveLayoutAtWindowAndFontSizes(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		for _, font := range []int{9, 14, 28} {
			for _, size := range [][2]int{{1000, 700}, {1440, 960}, {2000, 1200}} {
				t.Run(fmt.Sprintf("%s-%d-%d", theme, font, size[0]), func(t *testing.T) {
					state := printerExperienceState(t)
					state.Settings.Theme, state.Settings.InterfaceFontSize = theme, font
					h := newShellTestHarnessAtSize(t, state, size[0], size[1])
					requireShellLayout(t, h.snapshot())
					for _, tab := range []printTab{printTabMine, printTabRecords} {
						h.ui.panels.print.tab = tab
						h.ui.invalidator.Invalidate()
						h.settle()
						requireShellLayout(t, h.snapshot())
					}
				})
			}
		}
	}
}

func TestPrinterCardPinAndEditPassword(t *testing.T) {
	state := printerExperienceState(t)
	h := newShellTestHarness(t, state)
	card := h.ui.panels.print.lanCards[0]
	h.clickID(card.pinID)
	if command := h.lastCommand(); command.Kind != domain.SetPrinterPinned || !command.Bool || command.PrinterID != card.printer.ID {
		t.Fatalf("pin command=%+v", command)
	}
	h.ui.panels.print.tab = printTabMine
	h.ui.invalidator.Invalidate()
	h.settle()
	h.clickID(h.ui.panels.print.mineCards[0].editID)
	if !h.ui.panels.print.shareProtected || !h.ui.panels.print.sharePassword.Secure {
		t.Fatal("existing password was not preserved or masked")
	}
	h.clickID(h.ui.panels.print.ids.shareSubmit)
	if command := h.lastCommand(); command.Setting != "" || command.Password != "" {
		t.Fatal("saving a name unexpectedly cleared the password")
	}
	h.ui.panels.print.editShareDialog(state.Printers[1])
	h.ui.panels.print.sharePassword.SetText("new-password")
	h.ui.invalidator.Invalidate()
	h.settle()
	h.clickID(h.ui.panels.print.ids.shareSubmit)
	if command := h.lastCommand(); command.Kind != domain.SharePrinter || command.Setting != "password" || command.Password != "new-password" {
		t.Fatalf("password command=%+v", command)
	}
	h.ui.panels.print.editShareDialog(state.Printers[1])
	h.ui.panels.print.shareProtected = false
	h.ui.invalidator.Invalidate()
	h.settle()
	h.clickID(h.ui.panels.print.ids.shareSubmit)
	if command := h.lastCommand(); command.Setting != "password" || command.Password != "" {
		t.Fatal("password cannot be removed")
	}
}

func TestPrintWindowAuthenticatesBeforeSendingAndRetries(t *testing.T) {
	state := printerExperienceState(t)
	backend := newShellTestBackend(state)
	view := newPrintWindowView(backend, "printer-remote")
	t.Cleanup(view.Close)
	view.submit = backend.Submit
	closed := 0
	view.close = func() { closed++ }
	file := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(file, []byte("%PDF-1.7 payload"), 0600); err != nil {
		t.Fatal(err)
	}
	view.setFile(file)
	view.password.SetText("first-secret")
	view.sendJob(view.printer())
	command := backend.commandLog()[0]
	if command.Kind != domain.AuthenticatePrinter || command.Password != "first-secret" || !view.pendingPrint || closed != 0 {
		t.Fatalf("auth command=%+v", command)
	}
	state.PrinterAuth = map[string]domain.PrinterAuthResult{view.shareID: {Request: command.Request, Error: "打印密码不正确，请重新输入"}}
	backend.setState(state)
	view.refresh()
	if view.pendingPrint || !strings.Contains(view.err, "不正确") || hasCommandKind(backend.commandLog(), domain.PrintFile) {
		t.Fatal("failed authorization sent a print")
	}
	view.password.SetText("correct-secret")
	view.sendJob(view.printer())
	command = backend.commandLog()[1]
	state.PrinterAuthorized = map[string]string{view.shareID: "revision-one"}
	state.PrinterAuth = map[string]domain.PrinterAuthResult{view.shareID: {Request: command.Request}}
	backend.setState(state)
	view.refresh()
	view.refresh()
	if !hasCommandKind(backend.commandLog(), domain.PrintFile) || closed != 1 || view.password.Text() != "" {
		t.Fatal("successful authorization did not send exactly once")
	}
	state.Printers[0].AuthRevision = "revision-two"
	backend.setState(state)
	view.refresh()
	if !view.needsPassword(view.printer()) {
		t.Fatal("changed password did not require verification")
	}
}

func TestVoiceCustomTitlebarAndMinimumLayout(t *testing.T) {
	for _, font := range []int{14, 28} {
		state := voiceUIState(t, domain.VoiceCall{ID: "minimum-call", PeerID: "peer-a", PeerName: "校长", Direction: domain.VoiceCallOutgoing,
			Status: domain.VoiceCallActive, ConnectedAt: time.Now(), Muted: true, SpeakerMuted: true})
		state.Settings.InterfaceFontSize = font
		view := New(newShellTestBackend(state))
		view.voiceAudio = nil
		t.Cleanup(view.Close)
		config := view.voiceConfig()
		if config.WindowMode != app.WindowCustomTitlebar || config.WindowControls == nil {
			t.Fatal("voice still uses native titlebar")
		}
		config.Width, config.Height = 400, 500
		driver, err := app.NewDriver(config, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(driver.Close)
		requireShellLayout(t, driver.Snapshot())
	}
}

func TestPrinterDialogsAndWindowAtLargeTypography(t *testing.T) {
	for _, font := range []int{14, 28} {
		state := printerExperienceState(t)
		state.Settings.InterfaceFontSize = font
		h := newShellTestHarnessAtSize(t, state, 1000, 700)
		h.ui.panels.print.editShareDialog(state.Printers[1])
		h.ui.invalidator.Invalidate()
		h.settle()
		requireShellLayout(t, h.snapshot())
		view := newPrintWindowView(newShellTestBackend(state), "printer-remote")
		t.Cleanup(view.Close)
		config := view.Config()
		config.Width, config.Height = 580, 660
		driver, err := app.NewDriver(config, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(driver.Close)
		requireShellLayout(t, driver.Snapshot())
	}
}

func TestPrinterEnglishLayoutAtLargeTypography(t *testing.T) {
	previousLanguage := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previousLanguage) })
	state := printerExperienceState(t)
	state.Settings.Language = "en"
	state.Settings.InterfaceFontSize = 28
	h := newShellTestHarnessAtSize(t, state, 1000, 700)
	requirePrintRegionLayout(t, h)
	h.ui.panels.print.tab = printTabMine
	h.ui.invalidator.Invalidate()
	h.settle()
	requirePrintRegionLayout(t, h)
	h.ui.panels.print.editShareDialog(state.Printers[1])
	h.ui.invalidator.Invalidate()
	h.settle()
	requirePrintRegionLayout(t, h)
	view := newPrintWindowView(newShellTestBackend(state), "printer-remote")
	t.Cleanup(view.Close)
	config := view.Config()
	config.Width, config.Height = 580, 660
	window, err := vivaheadless.Open(config, nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(window.Close)
	if err := window.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
}

func requirePrintRegionLayout(t *testing.T, h *shellTestHarness) {
	t.Helper()
	for _, problem := range vivaheadless.CheckLayout(h.snapshot()) {
		if problem.Bounds.X >= h.ui.railExtent() && problem.Kind != vivaheadless.LayoutClipped {
			t.Errorf("print layout %s for %q: %s", problem.Kind, problem.Text, problem.Detail)
		}
	}
}

func TestPrinterExperienceScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	for _, theme := range []string{"light", "dark"} {
		state := printerExperienceState(t)
		state.Settings.Theme = theme
		shell, session := openShellQA(t, state, 1440, 960)
		if err := session.Expect("layout", "clean"); err != nil {
			t.Fatal(err)
		}
		shellQAShot(t, session, directory, "printers-adaptive-"+theme+".png")
		shell.panels.print.tab = printTabMine
		shell.invalidator.Invalidate()
		shellQAShot(t, session, directory, "printers-mine-"+theme+".png")
		shell.panels.print.editShareDialog(state.Printers[1])
		shell.invalidator.Invalidate()
		shellQAShot(t, session, directory, "printers-password-"+theme+".png")
		call := domain.VoiceCall{ID: "qa-call", PeerID: "peer-a", PeerName: "Alice", Direction: domain.VoiceCallOutgoing, Status: domain.VoiceCallRinging, StartedAt: time.Now()}
		voiceState := voiceUIState(t, call)
		voiceState.Settings.Theme = theme
		_, voiceSession := openVoiceQA(t, voiceState, 430, 540)
		if err := voiceSession.Expect("layout", "clean"); err != nil {
			t.Fatal(err)
		}
		shellQAShot(t, voiceSession, directory, "voice-custom-title-"+theme+".png")
		for _, font := range []int{14, 28} {
			state.Settings.InterfaceFontSize = font
			view := newPrintWindowView(newShellTestBackend(state), "printer-remote")
			t.Cleanup(view.Close)
			config := view.Config()
			config.Width, config.Height = 580, 660
			window, err := vivaheadless.Open(config, nil, vivaheadless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(window.Close)
			if err := window.Expect("layout", "clean"); err != nil {
				t.Fatal(err)
			}
			shellQAShot(t, window, directory, fmt.Sprintf("print-password-window-%s-%d.png", theme, font))
		}
	}
}

func BenchmarkPrintPagePaint(b *testing.B) {
	settings := domain.DefaultSettings(b.TempDir())
	settings.LastSection = "print"
	state := domain.State{Self: domain.Peer{ID: "self"}, Settings: settings,
		Peers: []domain.Peer{{ID: "owner", Name: "Owner", Online: true}}}
	for index := 0; index < 24; index++ {
		state.Printers = append(state.Printers, domain.PrinterShare{ID: fmt.Sprint(index), Name: "Office printer", OwnerID: "owner", OwnerName: "Owner", Available: true})
	}
	shell := New(newShellTestBackend(state))
	defer shell.Close()
	driver, err := app.NewDriver(shell.Config(), nil)
	if err != nil {
		b.Fatal(err)
	}
	defer driver.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		shell.invalidator.Invalidate()
		if err := driver.Pump(); err != nil {
			b.Fatal(err)
		}
	}
}
