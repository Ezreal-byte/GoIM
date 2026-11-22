// Command goim-deb-update is installed only by the Debian package. It takes
// signed metadata and receives the package bytes on standard input from GoIM.
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"goim/internal/buildinfo"
	"goim/internal/debupdate"
)

func main() {
	if err := install(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func install(args []string) error {
	if os.Geteuid() != 0 || len(args) != 2 {
		return errors.New("expected Polkit authorization, GoIM process ID and signed package metadata")
	}
	pid, err := strconv.Atoi(args[0])
	if err != nil || pid <= 1 {
		return errors.New("invalid GoIM process ID")
	}
	proc := fmt.Sprintf("/proc/%d/exe", pid)
	if path, err := os.Readlink(proc); err != nil || path != debupdate.Executable {
		return errors.New("GoIM is no longer running from its Debian installation")
	}
	var p debupdate.Package
	if err := json.Unmarshal([]byte(args[1]), &p); err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(buildinfo.UpdatePublicKey)
	if err != nil {
		return err
	}
	if err := p.Verify(key, p.Version, runtime.GOARCH); err != nil {
		return err
	}
	installed, err := exec.Command("/usr/bin/dpkg-query", "-W", "-f=${Version}", "goim").Output()
	if err != nil {
		return err
	}
	if err := exec.Command("/usr/bin/dpkg", "--compare-versions", string(installed), "lt", p.Version).Run(); err != nil {
		return errors.New("the signed Debian package must be newer than the installed version")
	}
	dir, err := os.MkdirTemp("/tmp", "goim-deb-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "goim.deb")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err := errors.Join(p.Stage(file, os.Stdin), file.Close()); err != nil {
		return err
	}
	// Check package identity too. A packaging error must not replace GoIM with
	// a differently named package, even when its release checksum was signed.
	meta, err := exec.Command("/usr/bin/dpkg-deb", "--show", "--showformat=${Package}\n${Version}\n${Architecture}", path).Output()
	if err != nil || string(meta) != "goim\n"+p.Version+"\n"+p.Arch {
		return errors.New("signed file does not contain the expected GoIM package")
	}
	if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
		return err
	}
	deadline := time.Now().Add(time.Minute)
	for {
		_, err := os.Stat(proc)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for GoIM to exit")
		}
		time.Sleep(100 * time.Millisecond)
	}
	cmd := exec.Command("/usr/bin/dpkg", "--refuse-downgrade", "--install", path)
	cmd.Dir = "/"
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}
