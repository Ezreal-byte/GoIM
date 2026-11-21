package core

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Some Windows junctions are traversable even when EvalSymlinks cannot
// expand them. Resolve the handle opened by Windows in that case.
func canonicalSharePath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buffer := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if n >= uint32(len(buffer)) {
			buffer = make([]uint16, n+1)
			continue
		}
		name := windows.UTF16ToString(buffer[:n])
		if tail, ok := strings.CutPrefix(name, "\\\\?\\UNC\\"); ok {
			return "\\\\" + tail, nil
		}
		if tail, ok := strings.CutPrefix(name, "\\\\?\\"); ok && len(tail) >= 3 && tail[1] == ':' {
			return tail, nil
		}
		return name, nil
	}
}
