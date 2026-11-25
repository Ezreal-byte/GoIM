//go:build !windows

package core

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func dataLockContended(err error) bool { return errors.Is(err, unix.EWOULDBLOCK) }

func lockDataFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
