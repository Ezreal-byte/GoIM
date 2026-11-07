//go:build !windows

package desktop

func shareSystemDrives() []shareDirEntry {
	return []shareDirEntry{{name: "根目录", path: string([]rune{'/'}), dir: true, size: -1}}
}
