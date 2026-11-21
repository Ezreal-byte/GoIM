package filemove

import (
	"strings"

	"golang.org/x/sys/windows"
)

// Exclusive publishes a complete file or directory without replacing an existing path.
func Exclusive(source, destination string) error {
	from, err := windows.UTF16PtrFromString(WindowsPath(source))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(WindowsPath(destination))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

// WindowsPath adds the extended-length prefix when an absolute path needs it for a Win32 file operation.
func WindowsPath(path string) string {
	if len(path) < 248 || strings.HasPrefix(path, `\\?\`) {
		return path
	}
	if share, ok := strings.CutPrefix(path, `\\`); ok {
		return `\\?\UNC\` + share
	}
	return `\\?\` + path
}
