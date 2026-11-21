package desktop

import (
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func displayLANEndpoint(state domain.State) string {
	host, port, err := net.SplitHostPort(state.ListenAddress)
	if err != nil || port == "" || port == "0" {
		return ""
	}
	ip, err := netip.ParseAddr(host)
	if host == "" || err == nil && ip.IsUnspecified() {
		ip, err = netip.ParseAddr(state.LocalIP)
	}
	if err != nil || !ip.Unmap().IsGlobalUnicast() || ip.IsLinkLocalUnicast() {
		return ""
	}
	return net.JoinHostPort(ip.Unmap().String(), port)
}

func (u *UI) drawLANStatus(f ui.C, wide bool) ui.Box {
	label, dot := i18n.T("等待网络"), u.palette.secondaryText
	if u.lanEndpoint != "" {
		label, dot = i18n.T("局域网在线"), u.palette.success
	}
	return ui.Row(f).Height(max(24, 11*u.theme.Metrics.EffectiveTextScale()+7)).PadX(3).Gap(7).Items(ui.Center).
		HitWith(u.ids.titleLAN, label, gfx.HitOptions{Role: gfx.RoleGroup, Hint: u.lanEndpoint}).
		Children(func() {
			ui.Stack(f).Size(7, 7).Radius(4).Background(dot)
			if wide {
				ui.Text(f, label).FontSize(11).Muted()
			}
		})
}

func (u *UI) drawLANHint(c *gfx.Canvas, bounds gfx.Rect) {
	id := u.ids.titleLAN
	if u.lanEndpoint == "" || u.modalOpen() || !c.Pressed().IsZero() ||
		c.Hovered() != id && c.FocusVisible() != id {
		return
	}
	components.BeginTooltip(c, u.theme, bounds, u.lanAnchor, components.TooltipOptions{
		ID: id, Side: components.TooltipBottom, Layer: 56,
	}).Paint(u.lanEndpoint)
}

const shellToastDuration = 4 * time.Second

type shellNotice struct {
	id       gfx.ID
	message  components.ToastSnapshot
	deadline time.Duration
}

func (u *UI) showToast(title, description string, tone components.ToastType) {
	// Keep the three newest notices. Each slot has a stable identity even when
	// its position changes as another notice expires.
	u.toastSlot = (u.toastSlot + 1) % len(u.toasts)
	copy(u.toasts[1:], u.toasts[:len(u.toasts)-1])
	u.toasts[0] = shellNotice{
		id:      u.ids.toastScope.At(u.toastSlot),
		message: components.ToastSnapshot{Title: title, Description: description, Type: tone},
	}
	u.invalidator.Invalidate()
}

func (u *UI) drawToasts(c *gfx.Canvas, viewport gfx.Rect) {
	var stack [3]components.SonnerToast
	n := 0
	now := c.Now()
	for i := range u.toasts {
		toast := &u.toasts[i]
		if toast.id.IsZero() {
			continue
		}
		if toast.deadline == 0 {
			toast.deadline = now + shellToastDuration
		}
		if toast.deadline <= now || c.Clicked(toast.id) {
			*toast = shellNotice{}
			continue
		}
		c.RequestFrameIn(toast.deadline - now)
		stack[n] = components.SonnerToast{Snapshot: toast.message, DismissID: toast.id}
		n++
	}
	components.PaintSonner(c, u.theme, viewport, stack[:n], components.SonnerOptions{
		Layer: 100, Position: components.ToastTopRight, Expanded: true,
	})
}

func (u *UI) presentContactRefresh() {
	seq := u.state.ContactRefresh.Sequence
	if seq == u.contactRefreshSequence {
		return
	}
	u.contactRefreshSequence = seq
	if !u.contactRefreshRequested || u.detachedWindow {
		return
	}
	if u.state.ContactRefresh.Pending {
		u.showToast(i18n.T("正在刷新局域网联系人"), "", components.ToastInfo)
		u.contactRefreshToast = u.toasts[0]
		return
	}
	u.contactRefreshRequested = false
	for i, t := range u.toasts {
		if t.id == u.contactRefreshToast.id && t.message.Title == u.contactRefreshToast.message.Title {
			u.toasts[i] = shellNotice{}
		}
	}
	online := 0
	for _, peer := range u.state.Peers {
		if peer.ID != u.state.Self.ID && peer.ListedContact() && !peer.ContactHidden && peer.VisibleOnline() {
			online++
		}
	}
	u.showToast(i18n.T("联系人刷新完成"), fmt.Sprintf(i18n.T("当前有 %d 位在线联系人"), online), components.ToastSuccess)
}
