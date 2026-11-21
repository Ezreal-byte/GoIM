//go:build linux

// Package bundle locates optional private companions in GoIM release packages.
package bundle

import (
	"os"
	"os/exec"
)

// Configure keeps desktop-provided file dialogs first and enables the bundled
// fallback on offline/minimal desktops. It never changes the library path.
func Configure() error {
	for _, command := range []string{"zenity", "kdialog"} {
		if _, err := exec.LookPath(command); err == nil {
			return nil
		}
	}
	directory, err := LinuxRuntimeBin()
	if err != nil {
		return err
	}
	if directory != "" {
		path := os.Getenv("PATH")
		if path != "" {
			path += string(os.PathListSeparator)
		}
		return os.Setenv("PATH", path+directory)
	}
	return nil // Development builds can still use the desktop portal.
}
