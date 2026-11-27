package desktop

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type dnsSettingsEditor struct {
	advanced                   ui.CollapsibleState
	server, zone               ui.Field
	serverID, domainID, saveID gfx.ID
	source                     domain.DNSDiscoveryConfig
	ready                      bool
}

func (p *Panels) drawDNSDiscovery(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	editor := &p.settingsExtra.dns
	cfg := state.Settings.DNSDiscovery
	if !editor.ready || cfg != editor.source {
		editor.ready, editor.source = true, cfg
		editor.server.SetText(cfg.Server)
		editor.zone.SetText(cfg.Domain)
	}
	status := state.DNSDiscoveryStatus
	switch {
	case state.Settings.DisableAutoDiscovery:
		status = "自动发现已关闭"
	case state.Self.Presence == domain.PresenceInvisible:
		status = "隐身时暂停发现"
	case cfg.Server == "":
		status = "未启用"
	case status == "":
		status = "正在查询 DNS 服务记录"
	}
	panelSection(f, i18n.T("跨网段发现"), i18n.T("各网段需能互访。在入口和每台客户端开启「自动发现联系人」，填写内网 DNS 和相同的发现域名，再点击「保存」。"), func() {
		var save bool
		p.settingsRow(f, theme, i18n.T("DNS 服务器"), i18n.T("仅供 GoIM 使用，清空并保存可关闭。"), func() {
			p.settingsControl(f, 260, func() {
				save = ui.TextField(f, &editor.server, i18n.T("DNS 服务器"), "10.0.0.53", ui.WithID(editor.serverID)).FillWidth().Submitted()
			})
		})
		p.settingsRow(f, theme, i18n.T("发现域名"), i18n.T("请管理员将该域的 _goim._tcp SRV 记录指向一台常在线的 GoIM，作为发现入口。"), func() {
			p.settingsControl(f, 260, func() {
				if ui.TextField(f, &editor.zone, i18n.T("发现域名"), domain.DefaultDiscoveryDomain, ui.WithID(editor.domainID)).FillWidth().Submitted() {
					save = true
				}
			})
		})
		p.settingsRow(f, theme, i18n.T("发现状态"), i18n.T(status), func() {
			p.settingsControl(f, 100, func() {
				if namedButton(f, editor.saveID, i18n.T("保存")).Small().Outline().Clicked() {
					save = true
				}
			})
		})
		if save {
			next, err := (domain.DNSDiscoveryConfig{Server: editor.server.Text(), Domain: editor.zone.Text()}).Normalized()
			if err != nil {
				p.localError = i18n.T(err.Error())
				return
			}
			p.localError = ""
			editor.ready = false
			submit(domain.Command{Kind: domain.SetSetting, Setting: "dns_discovery", DNSDiscovery: next})
		}
	})
}
