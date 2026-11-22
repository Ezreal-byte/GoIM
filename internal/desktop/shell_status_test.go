package desktop

import (
	"testing"
	"time"

	"goim/internal/domain"
)

func TestTitlebarShowsLANEndpointOnlyOnHover(t *testing.T) {
	state := shellMinimalState(t)
	state.ListenAddress, state.LocalIP = "[::]:47842", "192.168.1.8"
	h := newShellTestHarness(t, state)
	if !h.hasText("局域网在线") || h.hasText(state.ListenAddress) || h.hasText("192.168.1.8:47842") {
		t.Fatalf("LAN endpoint %q, state %q/%q; status label=%v; listener displayed=%v; endpoint displayed=%v",
			h.ui.lanEndpoint, h.ui.state.ListenAddress, h.ui.state.LocalIP,
			h.hasText("局域网在线"), h.hasText(state.ListenAddress), h.hasText("192.168.1.8:47842"))
	}
	hit, ok := h.findHit(h.ui.ids.titleLAN)
	if !ok || hit.Bounds.Y >= titlebarHeight {
		t.Fatal("LAN status is missing from the title bar")
	}
	if err := h.driver.Move(hit.Bounds.X+3, hit.Bounds.Y+3); err != nil {
		t.Fatal(err)
	}
	if !h.hasText("192.168.1.8:47842") {
		t.Fatal("LAN hover did not show the reachable address and port")
	}
	if endpoint := displayLANEndpoint(domain.State{ListenAddress: "[::]:47842", LocalIP: "127.0.0.1"}); endpoint != "" {
		t.Fatalf("loopback was advertised as LAN endpoint: %s", endpoint)
	}
}

func TestMainWindowBorderFollowsTheActiveTheme(t *testing.T) {
	h := newShellTestHarness(t, shellMinimalState(t))
	config := h.ui.Config()
	if config.WindowBorderColor == nil || config.WindowBorderColor() != config.ClearColor {
		t.Fatal("the native border must start with the window edge color")
	}
	h.ui.state.Settings.Theme = "dark"
	h.ui.theme = themeForSettings(h.ui.state.Settings)
	h.ui.palette = paletteFor(h.ui.theme, h.ui.state.Settings.ColorScheme)
	if got := config.WindowBorderColor(); got == config.ClearColor || got != colorToApp(h.ui.palette.rail) {
		t.Fatalf("native border after a theme change = %+v", got)
	}
}

func TestGlobalShortcutFailureIsTransientAndKeepsConfiguration(t *testing.T) {
	h := newShellTestHarness(t, shellMinimalState(t))
	r := desktopRuntime{ui: h.ui, backend: h.backend}
	var current, rejected string
	var stop func()
	for range 2 {
		r.applyGlobalShortcut("显示或隐藏窗口", "Ctrl+A+B", &current, &rejected, &stop, func() {})
	}
	if !h.ui.toasts[1].id.IsZero() || len(h.backend.commandLog()) != 0 || current != "" {
		t.Fatal("failed registration repeated the warning or wrote settings")
	}
	if err := h.driver.Move(25, 25); err != nil {
		t.Fatal(err)
	}
	if !h.hasText("无法注册全局快捷键") {
		t.Fatal("missing warning toast")
	}
	if err := h.driver.Advance(shellToastDuration + time.Second); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Paint(); err != nil {
		t.Fatal(err)
	}
	if h.hasText("无法注册全局快捷键") {
		t.Fatalf("shortcut warning did not expire at %v (deadline %v, id %v)",
			h.driver.Now(), h.ui.toasts[0].deadline, h.ui.toasts[0].id)
	}
}

func TestCopyConfirmationUsesTransientToast(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.clickID(h.ui.ids.navSettings)
	h.settle()
	h.clickID(h.ui.panels.ids.settingsCategories.At(2))
	h.settle()
	h.clickID(h.ui.panels.ids.aboutCopyVersion)
	h.settle()

	if h.ui.toasts[0].id.IsZero() || !h.hasText("已复制") {
		t.Fatal("copy confirmation did not use the Viva toast surface")
	}
	if err := h.driver.Advance(shellToastDuration + time.Second); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Paint(); err != nil {
		t.Fatal(err)
	}
	if h.hasText("已复制") {
		t.Fatal("copy confirmation remained on the page after its toast expired")
	}
}

func TestSuccessNoticeUsesToastAndIsCleared(t *testing.T) {
	state := shellTestState(t)
	h := newShellTestHarness(t, state)
	state.Notice = "个人资料已保存"
	state.Revision++
	h.publish(state)

	if h.ui.toasts[0].id.IsZero() || !h.hasText("个人资料已保存") {
		t.Fatal("success notice was not promoted to a transient toast")
	}
	if !hasCommandKind(h.backend.commandLog(), domain.ClearError) {
		t.Fatal("success notice was not cleared from persisted state")
	}
	if err := h.driver.Advance(shellToastDuration + time.Second); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Paint(); err != nil {
		t.Fatal(err)
	}
	if h.hasText("个人资料已保存") {
		t.Fatal("success notice was rendered as a persistent page banner")
	}
}
