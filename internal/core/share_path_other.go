//go:build !windows

package core

import "path/filepath"

func canonicalSharePath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
