package core

import (
	"errors"

	"goim/internal/filemove"
	"golang.org/x/sys/windows"
)

func availableReceiveBytes(dir string) (uint64, error) {
	path, err := windows.UTF16PtrFromString(filemove.WindowsPath(dir))
	if err != nil {
		return 0, err
	}
	var free uint64
	err = windows.GetDiskFreeSpaceEx(path, &free, nil, nil)
	return free, err
}

func receiveDiskFull(err error) bool {
	return errors.Is(err, windows.ERROR_DISK_FULL) || errors.Is(err, windows.ERROR_HANDLE_DISK_FULL)
}
