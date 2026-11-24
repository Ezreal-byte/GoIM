//go:build !windows

package remotedesktop

import "os/exec"

func configureProcess(*exec.Cmd) {}
