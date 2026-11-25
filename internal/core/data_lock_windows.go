package core

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func dataLockContended(err error) bool { return errors.Is(err, windows.ERROR_LOCK_VIOLATION) }

func lockDataFile(file *os.File) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}
