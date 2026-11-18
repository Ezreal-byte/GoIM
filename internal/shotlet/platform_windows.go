package shotlet

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func Supported() bool { return true }

func hideConsole(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}
