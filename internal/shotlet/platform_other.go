//go:build !darwin && !windows && !linux

package shotlet

import "os/exec"

func Supported() bool { return false }

func hideConsole(_ *exec.Cmd) {}
