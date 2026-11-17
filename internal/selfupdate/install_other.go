//go:build !windows

package selfupdate

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func detectInstall(exe string) Kind {
	dir := filepath.Dir(exe)
	switch runtime.GOOS {
	case "darwin":
		contents := filepath.Dir(dir)
		bundle := filepath.Dir(contents)
		if filepath.Base(exe) == "GoIM" && filepath.Base(dir) == "MacOS" && filepath.Base(contents) == "Contents" &&
			strings.HasSuffix(bundle, ".app") && regularFile(filepath.Join(contents, "Info.plist")) &&
			!strings.Contains(bundle, "/AppTranslocation/") && !strings.HasPrefix(bundle, "/Volumes/") &&
			parentWritable(filepath.Dir(bundle)) {
			return MacApp
		}
		return MacDownload
	case "linux":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if result, err := exec.CommandContext(ctx, "dpkg-query", "-S", "--", exe).Output(); err == nil {
			owner, _, _ := strings.Cut(string(result), ":")
			if owner == "goim" {
				return LinuxDeb
			}
		}
		if regularFile(filepath.Join(dir, "build-info.json")) && regularFile(filepath.Join(dir, "goim-snip")) {
			return LinuxTar
		}
	}
	return Unknown
}
