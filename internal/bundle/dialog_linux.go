//go:build linux

// Package bundle locates optional private companions in GoIM release packages.
package bundle

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Configure keeps desktop-provided file dialogs first and enables the bundled
// fallback on offline/minimal desktops. It never changes the library path.
func Configure() error {
	for _, command := range []string{"zenity", "kdialog"} {
		if _, err := exec.LookPath(command); err == nil {
			return nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if directory := dialogDirectory(exe); directory != "" {
		path := os.Getenv("PATH")
		if path != "" {
			path += string(os.PathListSeparator)
		}
		return os.Setenv("PATH", path+directory)
	}
	return nil // Development builds can still use the desktop portal.
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
