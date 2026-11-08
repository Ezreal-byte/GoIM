//go:build !windows

package desktop

import (
	"bytes"
	"errors"
	"os/exec"
	"runtime"
)

func setClipboardPNG(path string, data []byte) error {
	if err := validateClipboardImage(data); err != nil {
		return err
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("osascript", "-e",
			`on run argv
set the clipboard to (read (POSIX file (item 1 of argv)) as «class PNGf»)
end run`, path)
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil {
			command = exec.Command("wl-copy", "--type", "image/png")
			command.Stdin = bytes.NewReader(data)
			break
		}
		if _, err := exec.LookPath("xclip"); err == nil {
			command = exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-i", path)
			break
		}
		return errors.New("需要安装 wl-copy 或 xclip 才能复制截图")
	default:
		return errors.New("当前平台暂不支持直接复制截图")
	}
	if output, err := command.CombinedOutput(); err != nil {
		return errors.New(string(bytes.TrimSpace(output)))
	}
	return nil
}
