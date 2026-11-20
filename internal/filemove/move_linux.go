package filemove

import "golang.org/x/sys/unix"

// Exclusive publishes a complete file or directory without replacing an existing path.
func Exclusive(source, destination string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
}
