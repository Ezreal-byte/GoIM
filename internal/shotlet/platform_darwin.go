package shotlet

import (
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func Supported() bool {
	version, err := unix.Sysctl("kern.osproductversion")
	if err != nil {
		return false
	}
	major, remainder, _ := strings.Cut(version, ".")
	minor, _, _ := strings.Cut(remainder, ".")
	majorVersion, _ := strconv.Atoi(major)
	minorVersion, _ := strconv.Atoi(minor)
	return majorVersion > 13 || majorVersion == 13 && minorVersion >= 3
}

func hideConsole(_ *exec.Cmd) {}
