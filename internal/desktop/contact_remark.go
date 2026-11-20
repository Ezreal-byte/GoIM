package desktop

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
)

func remarkUsername(peer domain.Peer) string {
	if peer.Remark != "" {
		return peer.Name
	}
	return ""
}

func (u *UI) peerNameLine(f ui.C, name, username string, size float32) {
	if username == "" {
		ui.Text(f, name).FontSize(size).Weight(gfx.WeightSemibold).Ellipsis()
		return
	}
	ui.Row(f).FillWidth().MinWidth(0).Items(ui.Center).Gap(6).Children(func() {
		ui.Text(f, name).FontSize(size).Weight(gfx.WeightSemibold).Ellipsis()
		ui.Text(f, username).FontSize(max(10, size-2)).Muted().MaxWidth(140).Ellipsis()
	})
}

func (u *UI) openContactRemark(peerID string) {
	peer := u.state.Peer(peerID)
	if peer.ID == "" || peer.ID == u.state.Self.ID {
		return
	}
	u.closeShellMenu()
	u.closeChatPickers()
	u.organize = organizeState{open: true, peerID: peerID, mode: domain.SetContactRemark}
	u.organize.field.SetText(peer.Remark)
}
