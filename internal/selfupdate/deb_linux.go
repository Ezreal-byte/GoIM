package selfupdate

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"goim/internal/debupdate"
)

// The shell remains the desktop user. Only the package installer runs through
// Polkit. It signals readiness after authentication and package verification.
const debRelaunch = `
pid=$1
helper=$2
receipt=$3
shift 3
/usr/bin/pkexec --disable-internal-agent "$helper" "$pid" "$receipt" | (
	IFS= read -r signal && [ "$signal" = ready ] || exit 1
	printf 'ready\n' || exit 1
	# EOF follows dpkg's exit. Relaunch after success or failure so GoIM can
	# check its running version, but only if the old instance actually quit.
	while IFS= read -r line; do :; done
	[ ! -e "/proc/$pid/exe" ] || exit 1
	exec /usr/bin/GoIM "$@" </dev/null >/dev/null 2>&1
)
`

func installDeb(p debupdate.Package, path string, args []string, quit func()) error {
	if quit == nil {
		return errors.New("missing application shutdown handler")
	}
	receipt, err := json.Marshal(p)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	// Open the file as the desktop user. The root helper only consumes this
	// descriptor, never a user-supplied path or user-supplied relaunch arguments.
	argv := []string{"-c", debRelaunch, "goim-update", strconv.Itoa(os.Getpid()), debupdate.Helper, string(receipt)}
	cmd := exec.Command("/bin/sh", append(argv, args...)...)
	cmd.Stdin, cmd.Stderr = file, os.Stderr
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "ready\n" {
		waitErr := cmd.Wait()
		return errors.Join(errors.New("Debian update authorization or preparation failed"), waitErr)
	}
	go cmd.Wait()
	quit()
	return nil
}
