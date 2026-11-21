//go:build !windows

package desktop

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"goim/internal/bundle"
	"goim/internal/i18n"
)

func setClipboardPNG(path string, data []byte) error {
	if err := validateClipboardImage(data); err != nil {
		return err
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if path == "" {
			// AppleScript reads PNG data from a file. Keep it private and
			// remove it once the system pasteboard owns the image.
			temp, err := os.CreateTemp("", "goim-clipboard-*.png")
			if err != nil {
				return err
			}
			path = temp.Name()
			defer os.Remove(path)
			_, writeErr := temp.Write(data)
			if err := errors.Join(writeErr, temp.Close()); err != nil {
				return err
			}
		}
		command = exec.Command("osascript", "-e",
			`on run argv
set the clipboard to (read (POSIX file (item 1 of argv)) as «class PNGf»)
end run`, path)
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil && os.Getenv("WAYLAND_DISPLAY") != "" {
			command = exec.Command("wl-copy", "--type", "image/png")
			command.Stdin = bytes.NewReader(data)
			break
		}
		args := []string{"-selection", "clipboard", "-t", "image/png", "-i"}
		if tool, err := exec.LookPath("xclip"); err == nil {
			command = exec.Command(tool, args...)
		} else if bin, _ := bundle.LinuxRuntimeBin(); bin != "" {
			root := filepath.Dir(bin)
			command = exec.Command(filepath.Join(root, "ld.so"), "--library-path", filepath.Join(root, "lib"), filepath.Join(root, "xclip"))
			command.Args = append(command.Args, args...)
		} else {
			return errors.New(i18n.T("找不到图片剪贴板工具"))
		}
		command.Stdin = bytes.NewReader(data)
	default:
		return errors.New(i18n.T("当前平台暂不支持直接复制截图"))
	}
	// X11 clipboard owners can outlive the launcher. Leave output disconnected
	// so a child holding an inherited pipe cannot block the next copy.
	return command.Run()
}
