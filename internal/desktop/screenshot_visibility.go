package desktop

import "github.com/vivaui/viva/app"

type screenshotWindow interface {
	Hide()
	Show()
	Hidden() bool
	Minimized() bool
	Closed() bool
}

// Only windows visible at the start belong to this capture's restoration set.
// Hide preserves their native state, size and maximization; it never quits.
func hideScreenshotWindows(windows []screenshotWindow) []screenshotWindow {
	visible := windows[:0]
	for _, window := range windows {
		if !window.Closed() && !window.Hidden() && !window.Minimized() {
			window.Hide()
			visible = append(visible, window)
		}
	}
	return visible
}

func restoreScreenshotWindows(windows []screenshotWindow, origin screenshotWindow, allowed func(screenshotWindow) bool) {
	showOrigin := false
	for _, window := range windows {
		if window.Closed() || window.Minimized() || !allowed(window) {
			continue
		}
		if window == origin {
			showOrigin = true
		} else {
			window.Show()
		}
	}
	if showOrigin {
		origin.Show()
	}
}

func (r *desktopRuntime) screenshotWindows() []screenshotWindow {
	windows := make([]screenshotWindow, 0, 4+len(r.conversationWindows)+len(r.historyWindows)+len(r.printWindows))
	add := func(window *app.Window) {
		if window != nil {
			windows = append(windows, window)
		}
	}
	add(r.window)
	add(r.viewerWindow)
	add(r.voiceWindow)
	if r.remoteDesktopView != nil {
		add(r.remoteDesktopView.handle)
	}
	for _, entry := range r.conversationWindows {
		entry.ui.screenshotMenuOpen = false
		entry.ui.emojiOpen = false
		entry.ui.closeShellMenu()
		add(entry.window)
	}
	for _, entry := range r.historyWindows {
		add(entry.window)
	}
	for _, entry := range r.printWindows {
		add(entry.window)
	}
	return windows
}

func (r *desktopRuntime) restoreScreenshot(windows []screenshotWindow, origin *app.Window) {
	exiting := r.window == nil || r.window.Closed() || r.screenshotContext.Err() != nil || r.ui.update.quit || r.backend.Snapshot().DataResetPending
	locked := r.ui.lockVisible() || r.backend.Snapshot().Locked
	restoreScreenshotWindows(windows, origin, func(window screenshotWindow) bool {
		if window == r.window && r.captureDismissed {
			return false
		}
		if exiting {
			return false
		}
		if !locked {
			return true
		}
		// The main window can display its lock screen. Other content remains
		// concealed; its existing unlock lifecycle will decide when to show it.
		for _, entry := range r.conversationWindows {
			if entry.window == window {
				entry.restoreOnUnlock = true
			}
		}
		for _, entry := range r.historyWindows {
			if entry.window == window {
				entry.restoreOnUnlock = true
			}
		}
		for _, entry := range r.printWindows {
			if entry.window == window {
				entry.restoreOnUnlock = true
			}
		}
		return window == r.window && r.ui.lockVisible()
	})
	r.ui.invalidator.Invalidate()
	for _, entry := range r.conversationWindows {
		entry.ui.invalidator.Invalidate()
	}
}
