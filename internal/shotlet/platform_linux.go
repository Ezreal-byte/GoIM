package shotlet

import (
	"os"
	"os/exec"
	"strings"
)

// Match the upstream capture backend: Xwayland is not desktop capture.
func Supported() bool {
	return os.Getenv("DISPLAY") != "" && os.Getenv("WAYLAND_DISPLAY") == "" &&
		!strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland")
}

func hideConsole(_ *exec.Cmd) {}
