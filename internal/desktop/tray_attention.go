package desktop

import (
	"time"

	"github.com/vivaui/viva/app"
)

const attentionInterval = 500 * time.Millisecond

func attentionIcon(icon app.StatusIcon) app.StatusIcon {
	// Keep the notification area slot clickable even during the dim phase.
	icon.RGBA = append([]byte(nil), icon.RGBA...)
	for i := 3; i < len(icon.RGBA); i += 4 {
		icon.RGBA[i] /= 5
	}
	return icon
}

func (r *desktopRuntime) startAttention(now time.Time) {
	if r.tray == nil {
		return
	}
	r.flashUntil = now.Add(12 * time.Second)
	if r.flashDue.IsZero() {
		r.flashDue = now
	}
}

// The timer only posts a wake. Viva requires status item changes on its event
// loop, and there is no timer or repaint once the burst ends.
func (r *desktopRuntime) tickAttention(now time.Time) {
	if r.flashDue.IsZero() || now.Before(r.flashDue) {
		return
	}
	if !now.Before(r.flashUntil) {
		r.stopAttention()
		return
	}
	r.flashOn = !r.flashOn
	icon := r.trayNormal
	if r.flashOn {
		icon = r.trayFlash
	}
	_ = r.tray.SetIcon(icon)
	r.flashDue = now.Add(attentionInterval)
	r.flashTimer = time.AfterFunc(attentionInterval, r.ui.invalidator.Invalidate)
}

func (r *desktopRuntime) stopAttention() {
	if r.flashTimer != nil {
		r.flashTimer.Stop()
		r.flashTimer = nil
	}
	if r.flashOn {
		_ = r.tray.SetIcon(r.trayNormal)
	}
	r.flashOn = false
	r.flashDue = time.Time{}
	r.flashUntil = time.Time{}
}
