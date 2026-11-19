package desktop

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"goim/internal/domain"
)

func TestVoiceDoesNotBlockTheMainChatOrNavigation(t *testing.T) {
	state := voiceUIState(t, domain.VoiceCall{ID: "call", PeerID: "peer-a", Status: domain.VoiceCallRinging})
	h := newShellTestHarness(t, state)
	if h.ui.modalOpen() || h.hasHit(h.ui.ids.voiceHangup) {
		t.Fatal("call captured main window")
	}
	h.typeIntoLabel("消息输入框", "通话期间仍可聊天")
	h.clickID(h.ui.ids.chatSend)
	if command := h.lastCommand(); command.Kind != domain.SendText {
		t.Fatalf("command=%+v", command)
	}
	h.clickID(h.ui.ids.navContacts)
	h.clickID(h.ui.ids.sidebarRefresh)
	if command := h.lastCommand(); command.Kind != domain.RefreshContacts {
		t.Fatalf("command=%+v", command)
	}
}

func TestComposerVoiceActionIsTrailingAndDropHintStaysInComposer(t *testing.T) {
	state := shellTestState(t)
	state.Settings.LastSection = "messages"
	state.Peers[0].VoiceSupported = true
	h := newShellTestHarness(t, state)
	h.ui.selectConversation("conv-alice")
	h.settle()
	call, ok := h.findHit(h.ui.ids.chatHeaderCall)
	emoji, found := h.findHit(h.ui.ids.chatEmoji)
	if !ok || !found || call.Bounds.Y != emoji.Bounds.Y || call.Bounds.X <= emoji.Bounds.X {
		t.Fatal("call action not in trailing composer toolbar")
	}
	composer := h.ui.composerBox.Rect()
	_, err := h.driver.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: composer.X + 20, Y: composer.Y + 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range h.snapshot().Texts {
		if label.Value == "松开以发送文件" && (label.Bounds.Y < composer.Y || label.Bounds.Bottom() > composer.Bottom()) {
			t.Fatal("drop hint covered the chat history")
		}
	}
}

func TestIncomingChatFileAcceptAndSaveAsUseOneAction(t *testing.T) {
	for _, saveAs := range []bool{false, true} {
		t.Run(fmt.Sprint(saveAs), func(t *testing.T) {
			state := shellTestState(t)
			state.Messages[2].SenderID = "peer-a"
			state.Files[0].SenderID = "peer-a"
			state.Transfers[0].Direction, state.Transfers[0].Status = "receive", "offered"
			h := newShellTestHarness(t, state)
			destination := filepath.Join(t.TempDir(), "received.zip")
			if saveAs {
				if err := h.driver.QueueSaveFile(destination, nil); err != nil {
					t.Fatal(err)
				}
				h.clickID(h.ui.fileSaveIDs["transfer-1"])
			} else {
				h.clickID(h.ui.fileAcceptIDs["transfer-1"])
			}
			h.settle()
			count := 0
			for _, command := range h.backend.commandLog() {
				if command.Kind == domain.AcceptTransfer {
					count++
					if saveAs && (len(command.Paths) != 1 || command.Paths[0] != destination) {
						t.Fatal("wrong save path")
					}
				}
			}
			if count != 1 || h.ui.filePickerOpen || h.ui.drawer != "" {
				t.Fatalf("accepts=%d picker=%v drawer=%q", count, h.ui.filePickerOpen, h.ui.drawer)
			}
		})
	}
}

func TestInterfaceFontSizeScalesChatAndDetachedConversation(t *testing.T) {
	for _, size := range []int{9, 14, 18, 28} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			state := shellTestState(t)
			state.Settings.InterfaceFontSize = size
			h := newShellTestHarness(t, state)
			requireShellLayout(t, h.snapshot())
			foundMain := false
			for _, label := range h.snapshot().Texts {
				foundMain = foundMain || label.Value == "GoIM"
				if label.Value == "GoIM" && math.Abs(float64(label.Style.Size-16*float32(size)/14)) > 0.01 {
					t.Fatalf("main font=%v size=%d", label.Style.Size, size)
				}
			}
			if !foundMain {
				t.Fatal("main font label missing")
			}
			h.clickID(h.ui.ids.navContacts)
			requireShellLayout(t, h.snapshot())
			h.clickID(h.ui.ids.navSettings)
			h.clickID(h.ui.panels.ids.settingsCategories.At(int(panelSettingsGeneral)))
			requireShellLayout(t, h.snapshot())
			view := newDetachedConversationView(h.backend, "conv-alice")
			defer view.Close()
			driver, err := app.NewDriver(view.DetachedConfig(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			foundMessage := false
			for _, label := range driver.Snapshot().Texts {
				foundMessage = foundMessage || label.Value == "晚点确认。"
				if label.Value == "晚点确认。" && math.Abs(float64(label.Style.Size-14*float32(size)/14)) > 0.01 {
					t.Fatalf("detached font=%v size=%d", label.Style.Size, size)
				}
			}
			if !foundMessage {
				t.Fatal("detached message missing")
			}
			view.drawer = "files"
			if err := driver.Paint(); err != nil {
				t.Fatal(err)
			}
			if view.surface.filesPane.Width == 0 {
				t.Fatal("detached files drawer missing")
			}
			_ = driver.Advance(time.Millisecond)
		})
	}
}

func TestRelease013ScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			state := shellTestState(t)
			state.Settings.Theme = theme
			shell, session := openShellQA(t, state, 1440, 960)
			if err := session.Expect("layout", "clean"); err != nil {
				t.Fatal(err)
			}
			shellQAShot(t, session, directory, "release013-chat-"+theme+".png")
			composer := shell.composerBox.Rect()
			if _, err := session.Driver().Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: composer.X + 20, Y: composer.Y + 20}); err != nil {
				t.Fatal(err)
			}
			shellQAShot(t, session, directory, "release013-drop-"+theme+".png")
			if _, err := session.Driver().Drop(gfx.FileDropEvent{Phase: gfx.FileDropLeave}); err != nil {
				t.Fatal(err)
			}
			shell.toggleFilesPane()
			if err := session.Driver().Paint(); err != nil {
				t.Fatal(err)
			}
			if err := session.Wait(500 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if err := session.Expect("layout", "clean"); err != nil {
				t.Fatal(err)
			}
			shellQAShot(t, session, directory, "release013-files-"+theme+".png")
		})
	}
}

func TestVoiceWindowLargeTypographyFits(t *testing.T) {
	state := voiceUIState(t, domain.VoiceCall{ID: "large-call", PeerID: "peer-a", PeerName: "Alice", Direction: domain.VoiceCallOutgoing, Status: domain.VoiceCallActive, ConnectedAt: time.Now()})
	state.Settings.InterfaceFontSize = 28
	h := newVoiceWindowHarness(t, state)
	requireShellLayout(t, h.snapshot())
}
