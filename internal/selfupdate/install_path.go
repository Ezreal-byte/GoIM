package selfupdate

import "os"

func regularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func parentWritable(directory string) bool {
	file, err := os.CreateTemp(directory, ".goim-update-probe-*")
	if err != nil {
		return false
	}
	closeErr := file.Close()
	removeErr := os.Remove(file.Name())
	return closeErr == nil && removeErr == nil
}
