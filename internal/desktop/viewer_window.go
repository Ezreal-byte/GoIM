package desktop

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/i18n"
)

func (r *desktopRuntime) syncViewer() {
	u := r.ui
	if u.lockVisible() && u.chatSurface().viewer.open {
		u.closeImageViewer()
		return
	}
	if !r.viewerPending {
		if r.viewerWindow != nil {
			r.viewerWake.Invalidate()
		}
		return
	}
	r.viewerPending = false
	if r.viewerWindow != nil {
		u.chatSurface().viewer.pinned = r.viewerWindow.AlwaysOnTop()
		u.chatSurface().viewer.fullscreen = r.viewerWindow.Fullscreen()
		r.viewerWindow.Show()
		r.viewerWake.Invalidate()
		return
	}
	var window *app.Window
	var canvas ui.Canvas
	config := u.imageViewerConfig()
	config.Invalidator = &r.viewerWake
	config.OnWake = r.saveViewerOriginal
	config.OnClosed = func() {
		if r.viewerWindow == window {
			r.viewerWindow = nil
			r.viewerSavePath = ""
			u.closeImageViewer()
		}
	}
	var err error
	window, err = r.application.OpenWindow(config, &canvas)
	if err != nil {
		u.closeImageViewer()
		r.reportViewerError(i18n.T("无法打开图片窗口"), err)
		return
	}
	r.viewerWindow = window
}

func (r *desktopRuntime) closeViewer() {
	r.viewerPending = false
	r.viewerSavePath = ""
	if window := r.viewerWindow; window != nil {
		r.viewerWindow = nil
		window.Close()
	}
}

func (r *desktopRuntime) pinViewer(enabled bool) bool {
	if r.viewerWindow == nil {
		return false
	}
	if err := r.viewerWindow.SetAlwaysOnTop(enabled); err != nil {
		r.reportViewerError(i18n.T("无法更改图片窗口置顶"), err)
		return false
	}
	return true
}

func (r *desktopRuntime) fullscreenViewer(enabled bool) bool {
	if r.viewerWindow == nil {
		return false
	}
	if err := r.viewerWindow.SetFullscreen(enabled); err != nil {
		r.reportViewerError(i18n.T("无法切换全屏"), err)
		return false
	}
	return true
}

func (r *desktopRuntime) requestSaveViewer(path string) {
	if path == "" || r.viewerSaving || r.viewerSavePath != "" {
		return
	}
	r.viewerSavePath = path
	r.viewerWake.Invalidate()
}

func (r *desktopRuntime) saveViewerOriginal() {
	path := r.viewerSavePath
	r.viewerSavePath = ""
	if path == "" || r.viewerSaving {
		return
	}
	r.viewerSaving = true
	defer func() { r.viewerSaving = false }()
	filename := filepath.Base(path)
	file := r.ui.state.File(r.ui.chatSurface().viewer.fileID)
	// Use the original filename even when the received file was renamed locally.
	if file.Name != "" && r.ui.fileLocalPath[file.ID] == path {
		filename = filepath.Base(file.Name)
	}
	dest, err := app.SaveFile(app.SaveFileOptions{
		Title: i18n.T("保存原图"), SuggestedName: filename, DefaultExtension: strings.TrimPrefix(filepath.Ext(filename), "."),
	})
	if errors.Is(err, app.ErrCanceled) || (err == nil && dest == "") {
		return
	}
	if err != nil {
		r.reportViewerError(i18n.T("无法保存图片"), err)
		return
	}
	go func() {
		if err := copyViewerImage(path, dest); err != nil {
			r.reportViewerError(i18n.T("无法保存图片"), err)
		}
	}()
}

func (r *desktopRuntime) reportViewerError(label string, err error) {
	r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: label + ": " + err.Error()})
}

func copyViewerImage(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if existing, err := os.Stat(destination); err == nil {
		original, err := input.Stat()
		if err != nil {
			return err
		}
		if os.SameFile(original, existing) {
			return nil
		}
	}
	output, err := os.CreateTemp(filepath.Dir(destination), ".goim-image-*")
	if err != nil {
		return err
	}
	defer os.Remove(output.Name())
	_, copyErr := io.Copy(output, input)
	if err := errors.Join(copyErr, output.Close()); err != nil {
		return err
	}
	return os.Rename(output.Name(), destination)
}
