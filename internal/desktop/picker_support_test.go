package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestFilePickerProblemHidesUnsupportedImplementationDetails(t *testing.T) {
	previousLanguage := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previousLanguage) })
	for _, language := range []string{i18n.Chinese, i18n.English} {
		i18n.SetLanguage(language)
		message := filePickerProblem(fmt.Errorf("missing portal, zenity, kdialog: %w", app.ErrUnsupported))
		if message == "" || strings.Contains(message, "unsupported on this platform") || strings.Contains(message, "zenity") {
			t.Fatalf("raw implementation details shown: %q", message)
		}
	}
	if filePickerProblem(nil) != "" || filePickerProblem(errors.New("access denied")) != "access denied" {
		t.Fatal("ordinary picker errors were changed")
	}
}

func TestChatPickerUnavailableShowsFeedback(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.selectConversation("conv-alice")
	h.settle()
	if err := h.driver.QueueOpenFiles(nil, app.ErrUnsupported); err != nil {
		t.Fatal(err)
	}
	h.clickID(h.ui.ids.chatFile)
	if !h.hasText(i18n.T("无法选择文件")) || h.ui.toasts[0].message.Description != filePickerProblem(app.ErrUnsupported) || hasCommandKind(h.backend.commandLog(), domain.SendFiles) {
		t.Fatal("unavailable picker was ignored or submitted files")
	}
}

func TestSharePickerUnavailableShowsFeedback(t *testing.T) {
	h := panelTestNewHarness(t, panelModuleShares, panelTestDefaultState(t))
	if err := h.driver.QueueOpenFiles(nil, app.ErrUnsupported); err != nil {
		t.Fatal(err)
	}
	h.click(h.panels.ids.sharesAdd)
	h.click(h.panels.ids.sharePickFiles)
	if !h.hasText(i18n.T("无法打开系统选择器")) || h.panels.shareToast.message.Description != filePickerProblem(app.ErrUnsupported) || h.panels.shareDialog.open || h.panels.sharePickerOpen || len(h.commands) != 0 {
		t.Fatal("unavailable share picker did not display a safe explanation")
	}
}

func TestAboutDiagnosticsFallsBackWithoutFilePicker(t *testing.T) {
	for _, useDataDirectory := range []bool{false, true} {
		t.Run(fmt.Sprint(useDataDirectory), func(t *testing.T) {
			state := panelTestDefaultState(t)
			state.Settings.DownloadDir = t.TempDir()
			state.Peers = []domain.Peer{{ID: "private-peer", Name: "Private Recipient", Address: "192.168.23.45:47842"}}
			state.Messages = []domain.Message{{ID: "private-message", Text: "Private message body"}}
			if useDataDirectory {
				state.Settings.DownloadDir = ""
			}
			h := aboutTestOpen(t, state, ui.LightTheme(), i18n.Chinese, ui.Rect{X: 68, Width: 1372, Height: 1800})
			dataDirectory := t.TempDir()
			h.panels.diagnosticDataDir = func() string { return dataDirectory }
			var description string
			h.panels.toast = func(title, detail string, tone components.ToastType) { description = detail }
			directory := state.Settings.DownloadDir
			if useDataDirectory {
				directory = filepath.Join(dataDirectory, "diagnostics")
			}
			for count := 1; count <= 2; count++ {
				if err := h.driver.QueueSaveFile("", fmt.Errorf("no file chooser: %w", app.ErrUnsupported)); err != nil {
					t.Fatal(err)
				}
				h.click(h.panels.ids.aboutDiagnostics)
				files, err := filepath.Glob(filepath.Join(directory, "goim-diagnostics-*.txt"))
				if err != nil || len(files) != count || h.panels.localError != "" || h.panels.diagnosticPickerOpen {
					t.Fatalf("fallback files=%v err=%v UI=%q", files, err, h.panels.localError)
				}
				if !strings.Contains(description, directory) {
					t.Fatal("fallback export did not disclose its destination")
				}
				for _, path := range files {
					report, err := os.ReadFile(path)
					if err != nil || !strings.Contains(string(report), "== GoIM diagnostics ==") {
						t.Fatalf("report unreadable: %v", err)
					}
					for _, private := range []string{"Private Recipient", "Private message body", "192.168.23.45", directory, dataDirectory} {
						if strings.Contains(string(report), private) {
							t.Fatalf("report leaked %q", private)
						}
					}
				}
			}
			h.requireLayout()
		})
	}
}

func TestAboutDiagnosticsCancelDoesNotExport(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Settings.DownloadDir = t.TempDir()
	h := aboutTestOpen(t, state, ui.LightTheme(), i18n.Chinese, ui.Rect{X: 68, Width: 1372, Height: 1800})
	if err := h.driver.QueueSaveFile("", app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	h.click(h.panels.ids.aboutDiagnostics)
	files, err := os.ReadDir(state.Settings.DownloadDir)
	if err != nil || len(files) != 0 || h.panels.localError != "" || h.panels.diagnosticPickerOpen {
		t.Fatal("canceling export wrote a fallback file or left the picker blocked")
	}
}

func TestAboutCopiesReportWithoutFilePicker(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Messages = []domain.Message{{Text: "Do not copy private chat"}}
	h := aboutTestOpen(t, state, ui.LightTheme(), i18n.Chinese, ui.Rect{X: 68, Width: 1372, Height: 1800})
	h.click(h.panels.ids.aboutCopyDiagnostics)
	report := h.driver.Clipboard()
	if !strings.Contains(report, "== GoIM diagnostics ==") || strings.Contains(report, "Do not copy private chat") {
		t.Fatal("copied report was empty or included private chat")
	}
	if _, requested := h.driver.LastSaveFileRequest(); requested {
		t.Fatal("copying diagnostics unexpectedly opened the file picker")
	}
}

func TestAboutDiagnosticsFailureStillAllowsCopy(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Settings.DownloadDir = filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(state.Settings.DownloadDir, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := aboutTestOpen(t, state, ui.LightTheme(), i18n.Chinese, ui.Rect{X: 68, Width: 1372, Height: 1800})
	if err := h.driver.QueueSaveFile("", app.ErrUnsupported); err != nil {
		t.Fatal(err)
	}
	h.click(h.panels.ids.aboutDiagnostics)
	if h.panels.localError == "" || h.panels.diagnosticPickerOpen {
		t.Fatal("failed fallback export was not reported or left the picker blocked")
	}
	h.click(h.panels.ids.aboutCopyDiagnostics)
	if h.panels.localError != "" || !strings.Contains(h.driver.Clipboard(), "== GoIM diagnostics ==") {
		t.Fatal("failed file export prevented copying diagnostics")
	}
	contents, err := os.ReadFile(state.Settings.DownloadDir)
	if err != nil || string(contents) != "keep" {
		t.Fatal("fallback export overwrote an existing file")
	}
}

func TestFilePickerSupportScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	previousLanguage := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previousLanguage) })
	for _, theme := range []string{"light", "dark"} {
		for _, language := range []string{i18n.Chinese, i18n.English} {
			t.Run(theme+"-"+language, func(t *testing.T) {
				state := shellTestState(t)
				state.Settings.Theme, state.Settings.Language = theme, language
				shell, session := openShellQA(t, state, 1440, 1080)
				shellQAClick(t, session, shell.ids.navSettings)
				shellQAClick(t, session, shell.panels.ids.settingsCategories.At(int(panelSettingsAbout)))
				if err := session.Expect("text", i18n.T("复制脱敏报告")); err != nil {
					t.Fatal(err)
				}
				if err := session.Expect("layout", "clean"); err != nil {
					t.Fatal(err)
				}
				shellQAShot(t, session, directory, "picker-about-"+theme+"-"+language+".png")
				shellQAClick(t, session, shell.ids.navMessages)
				shell.selectConversation("conv-alice")
				shell.invalidator.Invalidate()
				if err := session.Driver().QueueOpenFiles(nil, app.ErrUnsupported); err != nil {
					t.Fatal(err)
				}
				if err := session.Driver().Pump(); err != nil {
					t.Fatal(err)
				}
				shellQAClick(t, session, shell.ids.chatFile)
				if err := session.Wait(700 * time.Millisecond); err != nil {
					t.Fatal(err)
				}
				if err := session.Expect("text", i18n.T("无法选择文件")); err != nil {
					t.Fatal(err)
				}
				if err := session.Expect("layout", "clean"); err != nil {
					t.Fatal(err)
				}
				shellQAShot(t, session, directory, "picker-feedback-"+theme+"-"+language+".png")
			})
		}
	}
}
