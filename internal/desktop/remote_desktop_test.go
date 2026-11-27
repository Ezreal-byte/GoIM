package desktop

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func remoteDesktopTestState(t *testing.T) domain.State {
	t.Helper()
	state := shellTestState(t)
	state.RemoteDesktopAvailable = true
	state.Peers[0].Transport = domain.TransportGoIM
	state.Peers[0].RemoteDesktopSupported = true
	state.Peers[0].RemoteDesktopAssistSupported = true
	state.Conversations[1].Opened = true
	return state
}

func TestRemoteDesktopButtonFollowsVoiceInDirectChat(t *testing.T) {
	state := remoteDesktopTestState(t)
	h := newShellTestHarness(t, state)
	h.ui.selected = "conv-alice"
	h.ui.invalidator.Invalidate()
	if err := h.driver.Pump(); err != nil {
		t.Fatal(err)
	}
	voice, ok := h.findHit(h.ui.ids.chatHeaderCall)
	if !ok {
		t.Fatal("voice button is missing")
	}
	remote, ok := h.findHit(h.ui.ids.chatRemoteDesktop)
	if !ok || remote.Disabled || remote.Bounds.X <= voice.Bounds.Right() || remote.Bounds.Y != voice.Bounds.Y {
		t.Fatalf("remote button does not follow voice button: %+v, %+v", voice, remote)
	}
	h.clickHit(remote)
	if cmd := h.lastCommand(); cmd.Kind != domain.StartRemoteDesktop || cmd.PeerID != state.Peers[0].ID {
		t.Fatalf("unexpected remote assistance request: %+v", cmd)
	}

	h.backend.clearCommands()
	state.Peers[0].RemoteDesktopSupported = false
	state.Revision++
	h.backend.setState(state)
	h.ui.invalidator.Invalidate()
	if err := h.driver.Pump(); err != nil {
		t.Fatal(err)
	}
	remote, ok = h.findHit(h.ui.ids.chatRemoteDesktop)
	if !ok || !remote.Disabled || remote.Hint != i18n.T("对方客户端不支持远程桌面") {
		t.Fatalf("missing unsupported peer feedback: %+v", remote)
	}
	h.clickHit(remote)
	if hasCommandKind(h.backend.commandLog(), domain.StartRemoteDesktop) {
		t.Fatal("disabled button attempted to start a remote session")
	}
}

func TestRemoteDesktopConsentFitsLargeText(t *testing.T) {
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	for _, language := range []string{i18n.Chinese, i18n.English} {
		t.Run(language, func(t *testing.T) {
			i18n.SetLanguage(language)
			state := remoteDesktopTestState(t)
			state.Settings.InterfaceFontSize = 28
			state.RemoteDesktop = domain.RemoteDesktopSession{
				ID: "large-consent", PeerID: state.Peers[0].ID, Status: domain.RemoteDesktopPending, Incoming: true, Host: true,
			}
			view := newRemoteDesktopView(state)
			view.avatarShell = &UI{theme: view.theme, palette: paletteFor(view.theme, state.Settings.ColorScheme)}
			t.Cleanup(view.dispose)
			config := view.config()
			config.Width, config.Height = config.MinWidth, config.MinHeight
			driver := newRemoteDesktopTestDriver(t, config)
			requireShellLayout(t, driver.Snapshot())
		})
	}
}

func TestRemoteDesktopViewerCapturesAndReleasesInput(t *testing.T) {
	view, driver, backend := remoteDesktopTestViewer(t, true, true)
	screen := remoteDesktopTestHit(t, driver, "远程屏幕")
	x, y := screen.Bounds.X+screen.Bounds.Width/2, screen.Bounds.Y+screen.Bounds.Height/2
	if err := driver.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if !view.viewer.captured || !driver.HoldsKeys() {
		t.Fatal("clicking the remote screen did not capture the keyboard")
	}
	if err := driver.KeyDown(gfx.KeyUnknown, "a"); err != nil {
		t.Fatal(err)
	}
	if err := driver.KeyUp(gfx.KeyUnknown, "a"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Scroll(gfx.ScrollEvent{X: x, Y: y, DeltaY: -90}); err != nil {
		t.Fatal(err)
	}
	if err := driver.SetMinimized(true); err != nil {
		t.Fatal(err)
	}
	if view.viewer.captured || backend.inputs[len(backend.inputs)-1].Action != "release" {
		t.Fatalf("minimizing did not release input: %+v", backend.inputs)
	}
	pattern := []string{"button:down", "button:up", "key:down", "key:up", "wheel:up", "release:up"}
	var got []string
	for _, input := range backend.inputs {
		state := "up"
		if input.Down {
			state = "down"
		}
		got = append(got, input.Action+":"+state)
	}
	if !slices.Equal(got, pattern) {
		t.Fatalf("unexpected input sequence: %v", got)
	}
	if first := backend.inputs[0]; first.Button != 1 || first.X != 32768 || first.Y != 32768 {
		t.Fatalf("unexpected remote coordinate: %+v", first)
	}
}

func TestRemoteDesktopViewOnlyStillHasAudioWhenGranted(t *testing.T) {
	_, driver, backend := remoteDesktopTestViewer(t, false, true)
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == i18n.T("远程屏幕") || hit.Label == i18n.T("快捷键") {
			t.Fatalf("view-only assistance exposed control: %s", hit.Label)
		}
	}
	clickRemoteDesktopControl(t, driver, "声音")
	clickRemoteDesktopControl(t, driver, "静音")
	if len(backend.inputs) != 1 || backend.inputs[0].Action != "audio" || !backend.inputs[0].Muted {
		t.Fatalf("view-only audio change: %+v", backend.inputs)
	}
}

func TestRemoteDesktopWindowReflectsConnectionAndSharingState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		session domain.RemoteDesktopSession
		stop    string
	}{
		{name: "connecting", session: domain.RemoteDesktopSession{ID: "connecting", Status: domain.RemoteDesktopConnecting}, stop: "取消"},
		{name: "sharing", session: domain.RemoteDesktopSession{ID: "sharing", Status: domain.RemoteDesktopActive, Incoming: true, Host: true}, stop: "停止共享"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := remoteDesktopTestState(t)
			state.RemoteDesktop = tc.session
			view := newRemoteDesktopView(state)
			stopped := false
			view.stop = func() { stopped = true }
			t.Cleanup(view.dispose)
			driver := newRemoteDesktopTestDriver(t, view.config())
			for _, hit := range driver.Snapshot().Hits {
				if hit.Label == i18n.T("允许控制") || hit.Label == i18n.T("远程屏幕") {
					t.Fatalf("unexpected control at %s: %s", tc.name, hit.Label)
				}
			}
			clickRemoteDesktopControl(t, driver, tc.stop)
			if !stopped {
				t.Fatal("stop action was not dispatched")
			}
		})
	}
}

func TestRemoteDesktopViewerKeepsReleaseChordLocal(t *testing.T) {
	view, driver, backend := remoteDesktopTestViewer(t, true, false)
	clickRemoteDesktopControl(t, driver, "远程屏幕")
	if err := driver.KeyDown(gfx.KeyControlLeft, ""); err != nil {
		t.Fatal(err)
	}
	if err := driver.KeyDown(gfx.KeyShiftLeft, ""); err != nil {
		t.Fatal(err)
	}
	if err := driver.KeyDown(gfx.KeyF12, ""); err != nil {
		t.Fatal(err)
	}
	if view.viewer.captured {
		t.Fatal("the local chord did not release control")
	}
	released := false
	for _, input := range backend.inputs {
		if input.Key == "f12" {
			t.Fatal("local release chord was forwarded to the shared computer")
		}
		released = released || input.Action == "release"
	}
	if !released {
		t.Fatal("local chord did not release the remote input state")
	}
}

func TestRemoteDesktopAudioDeviceCanReturnToDefault(t *testing.T) {
	view, _, backend := remoteDesktopTestViewer(t, false, true)
	view.viewer.setDevices([]string{"USB speaker"})
	view.viewer.device = "USB speaker"
	view.viewer.updateAudio()
	view.viewer.setDevices(nil)
	if len(backend.inputs) != 2 || backend.inputs[0].Device != "USB speaker" || backend.inputs[1].Device != "" {
		t.Fatalf("device selection did not fall back to the default: %+v", backend.inputs)
	}
}

func remoteDesktopTestViewer(t *testing.T, control, audio bool) (*remoteDesktopView, *app.Driver, *remoteDesktopTestBackend) {
	t.Helper()
	state := remoteDesktopTestState(t)
	state.RemoteDesktop = domain.RemoteDesktopSession{
		ID: "active", Status: domain.RemoteDesktopActive, Control: control, Audio: audio,
	}
	backend := &remoteDesktopTestBackend{}
	view := newRemoteDesktopView(state)
	view.stop = func() {}
	view.viewer = newRemoteDesktopViewer(view, backend)
	frame := domain.RemoteDesktopFrame{Width: 320, Height: 240, BGRA: make([]byte, 320*240*4)}
	view.viewer.pending.Store(&frame)
	view.viewer.nextFrame()
	t.Cleanup(view.dispose)
	driver := newRemoteDesktopTestDriver(t, view.config())
	return view, driver, backend
}

type remoteDesktopTestBackend struct {
	inputs []domain.RemoteDesktopInput
}

func (*remoteDesktopTestBackend) RemoteDesktopFrames(string) <-chan domain.RemoteDesktopFrame {
	return nil
}
func (b *remoteDesktopTestBackend) SendRemoteDesktopInput(_ string, input domain.RemoteDesktopInput) error {
	b.inputs = append(b.inputs, input)
	return nil
}

func newRemoteDesktopTestDriver(t *testing.T, config app.Config) *app.Driver {
	t.Helper()
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return driver
}

func remoteDesktopTestHit(t *testing.T, driver *app.Driver, label string) gfx.Hit {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == i18n.T(label) {
			return hit
		}
	}
	t.Fatalf("remote desktop control %q is missing", label)
	return gfx.Hit{}
}

func clickRemoteDesktopControl(t *testing.T, driver *app.Driver, label string) {
	t.Helper()
	hit := remoteDesktopTestHit(t, driver, label)
	if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
}
