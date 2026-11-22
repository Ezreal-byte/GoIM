//go:build !windows

package selfupdate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"goim/internal/debupdate"
)

func detectInstall(exe string) Kind {
	dir := filepath.Dir(exe)
	switch runtime.GOOS {
	case "darwin":
		contents := filepath.Dir(dir)
		bundle := filepath.Dir(contents)
		if filepath.Base(exe) == "GoIM" && filepath.Base(dir) == "MacOS" && filepath.Base(contents) == "Contents" &&
			strings.HasSuffix(bundle, ".app") && regularFile(filepath.Join(contents, "Info.plist")) &&
			!strings.Contains(bundle, "/AppTranslocation/") &&
			parentWritable(filepath.Dir(bundle)) {
			return MacApp
		}
		return MacDownload
	case "linux":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if exe == debupdate.Executable {
			result, err := exec.CommandContext(ctx, "/usr/bin/dpkg-query", "-S", "--", exe).Output()
			owner := strings.TrimSpace(string(result))
			if err == nil && (owner == "goim: "+exe || owner == "goim:"+runtime.GOARCH+": "+exe) {
				status, err := exec.CommandContext(ctx, "/usr/bin/dpkg-query", "-W", "-f=${db:Status-Status}", "goim").Output()
				if err == nil && string(status) == "installed" && os.Geteuid() != 0 &&
					executableFile("/usr/bin/pkexec") && executableFile(debupdate.Helper) {
					return LinuxDeb
				}
				return linuxDebManual
			}
		}
		if regularFile(filepath.Join(dir, "build-info.json")) && regularFile(filepath.Join(dir, "goim-snip")) {
			return LinuxTar
		}
	}
	return Unknown
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}
