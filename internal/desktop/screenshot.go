package desktop

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vivaui/viva/app"

	"goim/internal/domain"
)

const maxScreenshotBytes = 128 << 20

func (r *desktopRuntime) launchScreenshot() {
	go func() {
		tool, err := screenshotToolPath()
		if err != nil {
			r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: err.Error()})
			return
		}
		cache, err := os.UserCacheDir()
		if err != nil {
			r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: "无法定位截图缓存目录：" + err.Error()})
			return
		}
		directory := filepath.Join(cache, "GoIM", "screenshots")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: "无法创建截图缓存目录：" + err.Error()})
			return
		}
		output := filepath.Join(directory, fmt.Sprintf("goim-%d.png", time.Now().UnixNano()))
		command := exec.Command(tool, "-output", output)
		var stderr bytes.Buffer
		command.Stderr = &stderr
		if err := command.Run(); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 2 {
				return
			}
			detail := strings.TrimSpace(stderr.String())
			if detail == "" {
				detail = err.Error()
			}
			r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: "截图工具启动失败：" + detail})
			return
		}
		data, err := readScreenshot(output)
		if err != nil {
			r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: "读取截图失败：" + err.Error()})
			return
		}
		if err := setClipboardPNG(output, data); err != nil {
			r.backend.Submit(domain.Command{Kind: domain.ReportError,
				Text: "截图已保存在 " + output + "，但写入剪贴板失败：" + err.Error()})
			return
		}
		_ = app.SendNotification(app.Notification{
			Title: "GoIM 截图",
			Body:  "截图已复制，可在当前聊天中直接粘贴发送。",
		})
		_ = os.Remove(output)
	}()
}

func screenshotToolPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv("GOIM_SNIP_PATH")); override != "" {
		if info, err := os.Stat(override); err == nil && info.Mode().IsRegular() {
			return override, nil
		}
		return "", errors.New("GOIM_SNIP_PATH 指向的截图工具不存在")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", errors.New("无法定位 GoIM 程序目录：" + err.Error())
	}
	name := "goim-snip"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(filepath.Dir(executable), name)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path, nil
	}
	return "", errors.New("找不到 goim-snip，请确认截图组件与 GoIM 位于同一目录")
}

func readScreenshot(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 || info.Size() > maxScreenshotBytes {
		return nil, fmt.Errorf("PNG 大小无效（%d 字节）", info.Size())
	}
	data, err := io.ReadAll(io.LimitReader(file, maxScreenshotBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxScreenshotBytes {
		return nil, errors.New("截图 PNG 过大")
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return nil, errors.New("截图工具没有生成有效 PNG")
	}
	return data, nil
}
