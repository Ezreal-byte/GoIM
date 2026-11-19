package desktop

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type printWindowEntry struct {
	view            *printWindowView
	window          *app.Window
	restoreOnUnlock bool
	concealed       bool
}

func (r *desktopRuntime) openPrintWindow(shareID string) bool {
	if r.application == nil || r.backend == nil || r.printWindows == nil || r.ui.lockVisible() {
		return false
	}
	if existing := r.printWindows[shareID]; existing != nil {
		existing.window.Show()
		return true
	}

	view := newPrintWindowView(r.backend, shareID)
	view.avatar = r.ui.paintPrintAvatar
	view.submit = r.backend.Submit
	view.close = func() {
		if entry := r.printWindows[shareID]; entry != nil && entry.window != nil {
			entry.window.Close()
		}
	}
	config := view.Config()
	config.Icon = windowIcon()
	config.Class = "GoIM.Print"
	var window *app.Window
	baseClosed := config.OnClosed
	config.OnClosed = func() {
		if entry := r.printWindows[shareID]; entry != nil && entry.window == window {
			delete(r.printWindows, shareID)
		}
		view.Close()
		if baseClosed != nil {
			baseClosed()
		}
	}

	var canvas gfx.Canvas
	opened, err := r.application.OpenWindow(config, &canvas)
	if err != nil {
		view.Close()
		r.backend.Submit(domain.Command{
			Kind: domain.ReportError,
			Text: i18n.T("无法打开打印窗口：") + err.Error(),
		})
		return false
	}
	window = opened
	r.printWindows[shareID] = &printWindowEntry{view: view, window: opened}
	opened.Show()
	return true
}

func (entry *printWindowEntry) conceal(locked bool) {
	if entry.concealed == locked {
		return
	}
	entry.concealed = locked
	if locked {
		entry.restoreOnUnlock = !entry.window.Hidden()
		entry.window.Hide()
		return
	}
	if entry.restoreOnUnlock {
		entry.restoreOnUnlock = false
		entry.window.Show()
	}
}

func (r *desktopRuntime) closePrintWindows() {
	if len(r.printWindows) == 0 {
		return
	}
	windows := make([]*printWindowEntry, 0, len(r.printWindows))
	for _, entry := range r.printWindows {
		windows = append(windows, entry)
	}
	for _, entry := range windows {
		entry.window.Close()
	}
}
