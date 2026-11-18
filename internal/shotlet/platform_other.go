//go:build !darwin && !windows

package shotlet

import "os/exec"

func Supported() bool { return false }

func hideConsole(_ *exec.Cmd) {}
