package selfupdate

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func detectInstall(exe string) Kind {
	if !strings.EqualFold(filepath.Base(exe), "GoIM.exe") {
		return Unknown
	}
	dir := filepath.Dir(exe)
	if windowsLocation(registry.CURRENT_USER, "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\"+AppID, dir) &&
		regularFile(filepath.Join(dir, "Uninstall.exe")) {
		if parentWritable(dir) {
			return WindowsSetup
		}
		return Unknown
	}
	if windowsLocation(registry.LOCAL_MACHINE, "Software\\dev.goim.desktop\\Installer", dir) {
		return WindowsMSI
	}
	for _, folder := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if root := os.Getenv(folder); root != "" && strings.EqualFold(dir, filepath.Join(root, "GoIM")) {
			return WindowsMSI
		}
	}
	if !regularFile(filepath.Join(dir, "Uninstall.exe")) &&
		regularFile(filepath.Join(dir, "build-info.json")) && regularFile(filepath.Join(dir, "goim-snip.exe")) {
		return WindowsZIP
	}
	return Unknown
}

func windowsLocation(root registry.Key, name, dir string) bool {
	key, err := registry.OpenKey(root, name, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return false
	}
	defer key.Close()
	path, _, err := key.GetStringValue("InstallLocation")
	if err != nil || !filepath.IsAbs(path) {
		return false
	}
	actual, err := filepath.EvalSymlinks(path)
	return err == nil && strings.EqualFold(actual, dir)
}
