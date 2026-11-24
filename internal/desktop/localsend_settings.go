package desktop

import (
	"net"
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/i18n"
)

type localSendModal uint8

const (
	localSendNoModal localSendModal = iota
	localSendSettingsModal
	localSendFilesModal
	localSendPINModal
)

func (p *Panels) localSendSettings(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	cfg := state.Settings.LocalSend
	status := state.LocalSend
	panelSection(f, i18n.T("LocalSend 互通"), i18n.T("开启后，可在同一局域网和手机 LocalSend 双向传送文件。默认关闭。"), func() {
		label := i18n.T("启用 LocalSend")
		p.settingsRow(f, theme, label, "", func() {
			p.settingsControl(f, 84, func() {
				value := ui.State[bool](f, "localsend-enabled")
				*value = cfg.Enabled
				if ui.Switch(f, value, "").Hit(p.tree.ID(p.localSend.scope, "enabled"), label).Disabled(status.Unavailable).Changed() {
					cfg.Enabled = *value
					submit(domain.Command{Kind: domain.LocalSendConfigure, LocalSend: domain.LocalSendRequest{Config: cfg}})
				}
			})
		})
	})
}

func (v *localSendPanel) openSettings(cfg domain.LocalSendConfig) {
	v.closeModal()
	v.modal = localSendSettingsModal
	if cfg.Port == 0 {
		cfg.Port = domain.LocalSendDefaultPort
	}
	v.port.SetText(strconv.Itoa(cfg.Port))
	v.httpReceive = cfg.HTTP
}

func (v *localSendPanel) closeModal() {
	if v.modal == localSendNoModal {
		return
	}
	v.modal = localSendNoModal
	v.recordID = ""
	v.clearPIN = false
	v.receivePIN.SetText("")
	v.sendPIN.SetText("")
}

func (p *Panels) drawLocalSendDialog(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	v := &p.localSend
	switch v.modal {
	case localSendSettingsModal:
		p.drawLocalSendSettingsDialog(f, theme, state, submit)
	case localSendFilesModal, localSendPINModal:
		for i := range v.records {
			if v.records[i].transfer.ID != v.recordID {
				continue
			}
			entry := &v.records[i]
			if v.modal == localSendFilesModal {
				p.drawLocalSendFilesDialog(f, theme, entry)
			} else {
				p.drawLocalSendPINDialog(f, entry, submit)
			}
			return
		}
		v.closeModal()
	}
}

func (p *Panels) localSendDialog(f ui.C, name, title, desc string, width float32, focus gfx.ID) (ui.Box, gfx.Scope) {
	scope := p.tree.Sub(p.localSend.scope, p.tree.Name(name))
	return p.dialog(f, ui.DialogSpec{
		Title: title, Description: desc, Width: width, Scope: p.tree.ID(scope, "dialog"), InitialFocusID: focus,
		DismissID: p.tree.ID(scope, "dismiss"), CloseID: p.tree.ID(scope, "close"),
	}), scope
}

func (p *Panels) drawLocalSendSettingsDialog(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	v := &p.localSend
	portID := p.tree.ID(v.scope, "port")
	dialog, scope := p.localSendDialog(f, "settings-dialog", i18n.T("LocalSend 设置"), "", 500, portID)
	port, err := strconv.Atoi(strings.TrimSpace(v.port.Text()))
	pin := strings.TrimSpace(v.receivePIN.Text())
	validPort := err == nil && port >= 1 && port <= 65535
	validPIN := pin == "" || len(pin) >= 4 && len(pin) <= 8 && strings.Trim(pin, "0123456789") == ""
	var submitted bool
	dialog.Body(func() {
		ui.Column(f).FillWidth().Gap(13).Children(func() {
			ui.Column(f).FillWidth().Gap(5).Children(func() {
				ui.Text(f, i18n.T("LocalSend 端口")).Weight(gfx.WeightSemibold)
				field := ui.TextFieldID(f, portID, &v.port, i18n.T("LocalSend 端口"), "53317").Width(135)
				submitted = field.Submitted()
				ui.Wrap(f, i18n.T("默认 TCP 端口 53317。发现使用 UDP 53317。")).Muted()
			})
			ui.Divider(f).FillWidth()
			ui.Column(f).FillWidth().Gap(5).Children(func() {
				ui.Row(f).FillWidth().Items(ui.Center).Gap(8).Children(func() {
					ui.Text(f, i18n.T("HTTP 明文接收")).Grow(1).Weight(gfx.WeightSemibold)
					ui.Switch(f, &v.httpReceive, "").Hit(p.tree.ID(v.scope, "receive-http"), i18n.T("HTTP 明文接收"))
				})
				ui.Wrap(f, i18n.T("关闭时使用 HTTPS。启用 HTTP 后，文件和 PIN 在网络上以明文传输。")).Muted()
			})
			ui.Divider(f).FillWidth()
			ui.Column(f).FillWidth().Gap(6).Children(func() {
				ui.Text(f, i18n.T("LocalSend 接收 PIN")).Weight(gfx.WeightSemibold)
				ui.Row(f).FillWidth().Items(ui.Center).Gap(8).Children(func() {
					field := ui.TextFieldID(f, p.tree.ID(v.scope, "receive-pin"), &v.receivePIN, i18n.T("LocalSend 接收 PIN"), i18n.T("输入新 PIN")).Width(135).Disabled(v.clearPIN)
					submitted = field.Submitted() || submitted
					if state.LocalSend.HasPIN {
						ui.Checkbox(f.Part("clear-pin"), &v.clearPIN, i18n.T("清除当前 PIN"))
					}
				})
				ui.Wrap(f, i18n.T("PIN 使用 4–8 位数字，仅在本次运行期间有效。")).Muted()
			})
			if state.LocalSend.Problem != "" {
				ui.Wrap(f, state.LocalSend.Problem).Color(theme.Danger)
			}
		})
	})
	dialog.Footer(func() {
		if dialogActionButton(f, p.tree.ID(scope, "cancel"), i18n.T("取消")).Outline().Clicked() {
			v.closeModal()
		}
		if dialogActionButton(f, p.tree.ID(scope, "save"), i18n.T("保存")).Disabled(!validPort || !validPIN && !v.clearPIN).Clicked() {
			submitted = true
		}
	})
	if dialog.Dismissed() {
		v.closeModal()
	} else if v.modal == localSendSettingsModal && submitted && validPort && (validPIN || v.clearPIN) {
		if v.clearPIN {
			pin = ""
		}
		cfg := state.Settings.LocalSend
		cfg.Port, cfg.HTTP = port, v.httpReceive
		submit(domain.Command{Kind: domain.LocalSendConfigure, Password: pin, LocalSend: domain.LocalSendRequest{
			Config: cfg, SetPIN: pin != "" || v.clearPIN,
		}})
		v.closeModal()
	}
}

func (p *Panels) drawLocalSendReady(f ui.C, theme ui.Theme, width float32, status domain.LocalSendStatus, address, dir string) {
	cardWidth := min(590, max(0, width-2*min(width/30, 26)))
	name := status.Alias
	if name == "" {
		name = "GoIM"
	}
	ui.Column(f.Part("receiver-ready")).FillWidth().Items(ui.Center).PadY(32).Gap(14).Children(func() {
		ui.Column(f).Width(cardWidth).MinWidth(0).Radius(radiusCard).Border(1, theme.Border).
			Background(theme.Background).Items(ui.Center).PadSides(26, 24, 23, 24).Gap(12).Children(func() {
			ui.Stack(f).Size(78, 78).Radius(39).Background(panelAccentSurface(theme)).Center().Children(func() {
				ui.Stack(f).Size(55, 55).Radius(28).Background(theme.Background).Center().Children(func() {
					drawLocalSendMark(f, theme, 32)
				})
			})
			ui.Row(f).Items(ui.Center).Gap(6).Children(func() {
				ui.Row(f).Size(7, 7).Radius(4).Background(panelStatusColor(theme, "completed"))
				ui.Text(f, i18n.T("可接收")).FontSize(textSize(f, 13)).Weight(gfx.WeightMedium).Color(panelStatusColor(theme, "completed"))
			})
			ui.Text(f, name).MaxWidth(max(0, cardWidth-55)).FontSize(textSize(f, 28)).Weight(gfx.WeightSemibold).Ellipsis()
			ui.Wrap(f, i18n.T("在同一 Wi-Fi 的 LocalSend 中选择这台设备。")).Muted()
			if address != "" && status.Port != 0 {
				endpoint := net.JoinHostPort(address, strconv.Itoa(status.Port))
				label := i18n.T("复制接收地址")
				id := p.tree.ID(p.localSend.scope, "copy-receive-address")
				copyAddress := ui.Row(f).Height(31).PadX(10).Items(ui.Center).Gap(8).Radius(radiusControl).
					Border(1, theme.Border).Hover(ui.Look{}.Background(theme.Hover)).
					HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: endpoint}).Tooltip(label)
				copyAddress.Children(func() {
					ui.Text(f, endpoint).FontSize(textSize(f, 13)).Weight(gfx.WeightMedium)
					ui.Icon(f, Copy).Size(14, 14).Color(theme.MutedForeground)
				})
				if copyAddress.Clicked() {
					p.localSendCopy(f, endpoint)
				}
			}
			if dir != "" {
				ui.Divider(f).FillWidth()
				ui.Row(f).FillWidth().Items(ui.Center).Gap(9).Children(func() {
					ui.Icon(f, FolderOpen).Size(16, 16).Color(theme.MutedForeground)
					ui.Text(f, i18n.T("接收目录")).FontSize(textSize(f, 13)).Muted()
					ui.Text(f, dir).Grow(1).MinWidth(0).FontSize(textSize(f, 13)).Ellipsis()
				})
			}
		})
	})
}
