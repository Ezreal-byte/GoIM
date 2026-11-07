//go:build windows

package desktop

import (
	"fmt"
	"syscall"
)

var shareLogicalDrives = syscall.NewLazyDLL("kernel32.dll").NewProc("GetLogicalDrives")

func shareSystemDrives() []shareDirEntry {
	mask, _, callErr := shareLogicalDrives.Call()
	if mask == 0 && callErr != nil && callErr != syscall.Errno(0) {
		return nil
	}
	drives := make([]shareDirEntry, 0, 8)
	for index := 0; index < 26; index++ {
		if mask&(1<<uint(index)) == 0 {
			continue
		}
		root := fmt.Sprintf("%c:\\", 'A'+index)
		drives = append(drives, shareDirEntry{name: root[:2], path: root, dir: true, size: -1})
	}
	return drives
}
