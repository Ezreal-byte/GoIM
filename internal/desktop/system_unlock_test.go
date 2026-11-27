package desktop

import (
	"context"
	"testing"
	"time"

	"github.com/vivaui/viva/app/systemauth"
	"goim/internal/domain"
)

func TestSystemUnlockRejectsStaleSuccessAndCancelsNativeRequest(t *testing.T) {
	for _, change := range []string{"disabled", "unlocked", "relocked", "closed"} {
		t.Run(change, func(t *testing.T) {
			state := shellMinimalState(t)
			state.Locked, state.Settings.HasLockPassword, state.Settings.SystemUnlock = true, true, true
			state.LockResult.Request = 10
			h := newShellTestHarness(t, state)
			s := &h.ui.lockUI
			s.method = systemauth.VerificationWindowsHello
			started, canceled := make(chan struct{}), make(chan struct{})
			s.systemAuth = func(ctx context.Context, reason string) error {
				close(started)
				<-ctx.Done()
				close(canceled)
				// A late native success must not override app cancellation.
				return nil
			}
			h.ui.beginSystemUnlock()
			request := s.systemRequest
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("verification worker did not start")
			}
			switch change {
			case "disabled":
				h.ui.state.Settings.SystemUnlock = false
			case "unlocked":
				h.ui.state.Locked = false
			case "relocked":
				h.ui.state.LockResult.Request++
			case "closed":
				h.ui.Close()
			}
			h.ui.collectSystemUnlock()
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("native verification context was not canceled")
			}
			// Explicitly enqueue a success from that old lock generation too.
			s.systemResults <- systemAuthResult{request: request}
			h.ui.collectSystemUnlock()
			if s.systemBusy || hasCommandKind(h.backend.commandLog(), domain.UnlockAppSystem) {
				t.Fatal("stale verification unlocked the application")
			}
		})
	}
}

func TestUnavailableSystemUnlockKeepsPreferenceAndCanBeDisabled(t *testing.T) {
	state := shellMinimalState(t)
	state.Settings.HasLockPassword, state.Settings.SystemUnlock = true, true
	h := newShellTestHarness(t, state)
	h.ui.lockUI.methodResults <- systemauth.VerificationUnavailable
	h.ui.collectSystemUnlock()
	if !h.ui.state.Settings.SystemUnlock {
		t.Fatal("a capability probe reset the stored preference")
	}
	h.clickID(h.ui.ids.navSettings)
	h.clickID(h.ui.panels.ids.settingsCategories.At(int(panelSettingsSecurity)))
	h.clickID(h.ui.panels.settingsExtra.lockSystem)
	command, ok := findShellCommand(h.backend.commandLog(), domain.SetSystemUnlock)
	if !ok || command.Bool || h.ui.panels.settingsExtra.passwordOpen {
		t.Fatal("unavailable verification could not be explicitly disabled")
	}
}

func TestSystemPasswordMethodAndFailuresKeepPasswordFallback(t *testing.T) {
	state := shellMinimalState(t)
	state.Locked, state.Settings.HasLockPassword, state.Settings.SystemUnlock = true, true, true
	h := newShellTestHarness(t, state)
	h.ui.lockUI.method = systemauth.VerificationDevicePassword
	h.ui.invalidator.Invalidate()
	h.settle()
	if !h.hasText("使用系统密码解锁") || !h.hasHit(h.ui.lockUI.submit) {
		t.Fatal("device password capability lost a usable fallback")
	}
	for _, err := range []error{systemauth.ErrCanceled, systemauth.ErrFailed, systemauth.ErrForegroundRequired, systemauth.ErrUnavailable, context.Canceled, context.DeadlineExceeded} {
		h.ui.lockUI.systemAuth = func(context.Context, string) error { return err }
		h.ui.beginSystemUnlock()
		h.waitFor(func() bool { return !h.ui.lockUI.systemBusy })
		if hasCommandKind(h.backend.commandLog(), domain.UnlockAppSystem) {
			t.Fatalf("failure %v unlocked GoIM", err)
		}
		if !h.hasHit(h.ui.lockUI.submit) {
			t.Fatal("failure hid the GoIM password button")
		}
	}
}
