//go:build linux

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"goim/internal/domain"
)

// Run only in an isolated Xvfb session with a real, opt-in helper. This
// deliberately exercises native mouse and keyboard input on that desktop.
func TestRemoteDesktopNativeX11(t *testing.T) {
	if os.Getenv("GOIM_TEST_REMOTE_DESKTOP_X11") != "1" {
		t.Skip("requires the RustDesk helper and a disposable Xvfb desktop")
	}
	if os.Getenv("DISPLAY") == "" || os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("GOIM_REMOTE_DESKTOP_HELPER") == "" {
		t.Fatal("expected X11 and an explicit helper path")
	}
	tool, err := exec.LookPath("xdotool")
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(tool, "mousemove", "20", "20").CombinedOutput(); err != nil {
		t.Fatalf("prepare Xvfb pointer: %v: %s", err, output)
	}
	keys := prepareRemoteDesktopKeyboard(t, tool)

	viewer, host := testNode(t, "native-viewer"), testNode(t, "native-host")
	connectNodes(t, viewer, host)
	if !viewer.Snapshot().Peer(host.Snapshot().Self.ID).RemoteDesktopSupported {
		t.Fatal("real helper was not enabled")
	}
	if err := viewer.startRemoteDesktop(host.Snapshot().Self.ID); err != nil {
		t.Fatal(err)
	}
	invite := waitRemote(t, host, domain.RemoteDesktopPending)
	if err := host.acceptRemoteDesktop(invite.ID, true, false); err != nil {
		t.Fatal(err)
	}
	var session domain.RemoteDesktopSession
	eventually(t, "real RustDesk viewer active", func() bool {
		session = viewer.Snapshot().RemoteDesktop
		if session.Status == domain.RemoteDesktopEnded {
			t.Fatalf("helper session ended during startup: %s", session.Reason)
		}
		return session.Status == domain.RemoteDesktopActive
	})
	waitRemote(t, host, domain.RemoteDesktopActive)
	frame := waitFrame(t, viewer.RemoteDesktopFrames(session.ID))
	if frame.Width < 200 || frame.Height < 200 || len(frame.BGRA) != 4*frame.Width*frame.Height {
		t.Fatalf("invalid native frame %dx%d (%d bytes)", frame.Width, frame.Height, len(frame.BGRA))
	}

	if err := viewer.SendRemoteDesktopInput(session.ID, domain.RemoteDesktopInput{Action: "move", X: 32768, Y: 32768}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "actual X11 pointer moved through the authorized tunnel", func() bool {
		out, err := exec.Command(tool, "getmouselocation", "--shell").Output()
		if err != nil {
			return false
		}
		position := map[string]int{}
		for _, line := range strings.Fields(string(out)) {
			name, value, _ := strings.Cut(line, "=")
			position[name], _ = strconv.Atoi(value)
		}
		return position["X"] > 100 && position["Y"] > 100
	})
	for _, input := range []domain.RemoteDesktopInput{
		{Action: "key", Key: "a", Down: true},
		{Action: "key", Key: "a"},
		{Action: "shortcut", Key: "switch-app"},
	} {
		if err := viewer.SendRemoteDesktopInput(session.ID, input); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "actual X11 key and Alt+Tab delivered", func() bool {
		output, err := os.ReadFile(keys)
		return err == nil && strings.Contains(string(output), "keysym 0x61, a") &&
			strings.Contains(string(output), "keysym 0xff09, Tab") &&
			strings.Contains(string(output), "keysym 0xffe9, Alt_L")
	})
	if err := host.stopRemoteDesktop(session.ID, "ended"); err != nil {
		t.Fatal(err)
	}
	waitRemote(t, host, domain.RemoteDesktopEnded)
	waitRemote(t, viewer, domain.RemoteDesktopEnded)
}

func prepareRemoteDesktopKeyboard(t *testing.T, tool string) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "keys.log")
	output, err := os.Create(log)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	const name = "GoIM X11 native keyboard test"
	cmd := exec.Command("stdbuf", "-oL", "xev", "-name", name, "-event", "keyboard")
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	eventually(t, "X11 keyboard test window can receive focus", func() bool {
		windows, err := exec.Command(tool, "search", "--name", name).Output()
		if err != nil || len(strings.Fields(string(windows))) != 1 {
			return false
		}
		return exec.Command(tool, "windowfocus", strings.TrimSpace(string(windows))).Run() == nil
	})
	return log
}
