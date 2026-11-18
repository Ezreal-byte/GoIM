package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vivaui/viva/app"

	"goim/internal/capture"
	"goim/internal/domain"
	"goim/internal/i18n"
	"goim/internal/shotlet"
)

const maxScreenshotBytes = 128 << 20

type screenshotTool struct {
	path    string
	shotlet bool
}

func (r *desktopRuntime) launchScreenshot() {
	if !r.screenshotRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer r.screenshotRunning.Store(false)
		if err := r.captureScreenshot(r.screenshotContext); err != nil && r.screenshotContext.Err() == nil {
			r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: err.Error()})
		}
	}()
}

func (r *desktopRuntime) captureScreenshot(ctx context.Context) error {
	tool, err := resolveScreenshotTool()
	if err != nil {
		return err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("%s%w", i18n.T("无法定位截图缓存目录："), err)
	}
	directory := filepath.Join(cache, "GoIM", "screenshots")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("%s%w", i18n.T("无法创建截图缓存目录："), err)
	}
	file, err := os.CreateTemp(directory, "goim-*.png")
	if err != nil {
		return fmt.Errorf("%s%w", i18n.T("无法创建截图文件："), err)
	}
	output := file.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(output)
		}
	}()
	if err := file.Close(); err != nil {
		return err
	}
	theme := ""
	if tool.shotlet {
		theme = r.backend.Snapshot().Settings.Theme
	}
	completed, err := tool.run(ctx, output, theme)
	if errors.Is(err, shotlet.ErrScreenRecordingPermission) {
		return errors.New(i18n.T("请在系统设置的“隐私与安全性”中允许 GoIM 或 Shotlet 截图录制屏幕，然后重试。"))
	}
	if err != nil {
		return fmt.Errorf("%s%w", i18n.T("截图失败："), err)
	}
	if !completed || ctx.Err() != nil {
		return ctx.Err()
	}
	data, err := readScreenshot(output)
	if err != nil {
		return fmt.Errorf("%s%w", i18n.T("读取截图失败："), err)
	}
	if err := setClipboardPNG(output, data); err != nil {
		keep = true
		return fmt.Errorf("%s%s%s%w", i18n.T("截图已保存在 "), output, i18n.T("，但写入剪贴板失败："), err)
	}
	_ = app.SendNotification(app.Notification{
		Title: i18n.T("GoIM 截图"),
		Body:  i18n.T("截图已复制，可在当前聊天中直接粘贴发送。"),
	})
	return nil
}

func (tool screenshotTool) run(ctx context.Context, output, theme string) (bool, error) {
	if tool.shotlet {
		if theme != "dark" {
			theme = "light"
		}
		frame, err := shotlet.Capture(ctx, tool.path, i18n.Language(), theme)
		if err != nil || frame == nil {
			return false, err
		}
		file, err := os.OpenFile(output, os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return false, err
		}
		encodeErr := png.Encode(file, frame)
		return true, errors.Join(encodeErr, file.Close())
	}
	command := exec.CommandContext(ctx, tool.path, "-select", "-lang", i18n.Language(), "-output", output)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && (exit.ExitCode() == 2 || exit.ExitCode() == capture.ExitSaved) {
			return false, nil
		}
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return false, errors.New(detail)
		}
		return false, err
	}
	return true, nil
}

func resolveScreenshotTool() (screenshotTool, error) {
	if override := strings.TrimSpace(os.Getenv("GOIM_SNIP_PATH")); override != "" {
		if info, err := os.Stat(override); err == nil && info.Mode().IsRegular() {
			return screenshotTool{path: override}, nil
		}
		return screenshotTool{}, errors.New(i18n.T("GOIM_SNIP_PATH 指向的截图工具不存在"))
	}
	executable, err := os.Executable()
	if err != nil {
		return screenshotTool{}, fmt.Errorf("%s%w", i18n.T("无法定位 GoIM 程序目录："), err)
	}
	return packagedScreenshotTool(executable)
}

func packagedScreenshotTool(executable string) (screenshotTool, error) {
	if shotlet.Supported() {
		path := shotlet.HelperPath(executable)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return screenshotTool{path: path, shotlet: true}, nil
		}
	}
	name := "goim-snip"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(filepath.Dir(executable), name)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return screenshotTool{path: path}, nil
	}
	return screenshotTool{}, errors.New(i18n.T("找不到截图组件，请完整解压或重新安装 GoIM。"))
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
		return nil, fmt.Errorf(i18n.T("PNG 大小无效（%d 字节）"), info.Size())
	}
	data, err := io.ReadAll(io.LimitReader(file, maxScreenshotBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxScreenshotBytes {
		return nil, errors.New(i18n.T("截图 PNG 过大"))
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return nil, errors.New(i18n.T("截图工具没有生成有效 PNG"))
	}
	return data, nil
}
