package filemove

import "golang.org/x/sys/unix"

// Exclusive publishes a complete file or directory without replacing an existing path.
func Exclusive(source, destination string) error {
	return unix.RenamexNp(source, destination, unix.RENAME_EXCL)
}
