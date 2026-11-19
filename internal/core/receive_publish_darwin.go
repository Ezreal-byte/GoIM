package core

import "golang.org/x/sys/unix"

func publishExclusive(source, destination string) error {
	return unix.RenamexNp(source, destination, unix.RENAME_EXCL)
}
