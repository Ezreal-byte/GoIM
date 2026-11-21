package bundle

import (
	"os"
	"path/filepath"
)

// LinuxRuntimeBin locates private desktop tools in either a tar or DEB install.
func LinuxRuntimeBin() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return dialogDirectory(exe), nil
}

func dialogDirectory(exe string) string {
	for _, root := range []string{filepath.Dir(exe), filepath.Join(filepath.Dir(exe), "..")} {
		dir := filepath.Join(root, "lib", "goim", "file-dialog", "bin")
		info, err := os.Stat(filepath.Join(dir, "zenity"))
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return dir
		}
	}
	return ""
}
