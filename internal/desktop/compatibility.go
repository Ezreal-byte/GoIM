package desktop

import (
	"fmt"
	"github.com/vivaui/viva/gfx"
	"strconv"
	"strings"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type connectionRoute uint8

const (
	routeAuto connectionRoute = iota
	routeGoIM
	routeNeiwangtong
	routeIPMSG
)

var connectionRoutes = [...]connectionRoute{routeAuto, routeGoIM, routeNeiwangtong, routeIPMSG}

func (route connectionRoute) label() string {
	switch route {
	case routeGoIM:
		return "GoIM"
	case routeNeiwangtong:
		return i18n.T("内网通原生")
	case routeIPMSG:
		return i18n.T("飞鸽／飞秋兼容")
	default:
		return i18n.T("自动")
	}
}

func (route connectionRoute) transport() string {
	switch route {
	case routeGoIM:
		return domain.TransportGoIM
	case routeNeiwangtong:
		return domain.TransportNeiwangtong
	case routeIPMSG:
		return domain.TransportIPMSG
	default:
		return domain.TransportAuto
	}
}

func connectionAvailable(s domain.State, route connectionRoute) bool {
	switch route {
	case routeNeiwangtong:
		return s.NeiwangtongStatus.Active
	case routeIPMSG:
		return s.IPMSGStatus.Active
	default:
		return true
	}
}

func connectionProblem(s domain.State, route connectionRoute) string {
	var status domain.CompatibilityStatus
	switch route {
	case routeNeiwangtong:
		status = s.NeiwangtongStatus
	case routeIPMSG:
		status = s.IPMSGStatus
	default:
		return ""
	}
	if status.Problem != "" {
		return i18n.T(status.Problem)
	}
	return i18n.T("请先在网络设置中开启此互通服务。")
}

func connectionHint(s domain.State, route connectionRoute) string {
	var port int
	switch route {
	case routeNeiwangtong:
		port = s.Settings.Neiwangtong.Port
		if port == 0 {
			port = domain.NeiwangtongDiscoveryPort
		}
	case routeIPMSG:
		port = s.Settings.IPMSG.Port
		if port == 0 {
			port = domain.IPMSGPort
		}
	default:
		return i18n.T("例如 192.168.1.12 或 192.168.1.12:47842")
	}
	return fmt.Sprintf("192.168.1.12:%d", port)
}

type panelConnectionFields struct {
	route     *connectionRoute
	address   *ui.Field
	addressID gfx.ID
	connectID gfx.ID
}

// connectionFields keeps the same labels, availability and keyboard behavior
// in Settings and in the dialog. The dialog supplies its own footer action.
func connectionFields(f ui.C, state domain.State, form panelConnectionFields) (submitted bool) {
	ui.Column(f).FillWidth().Gap(12).Children(func() {
		ui.Column(f).FillWidth().Gap(4).Children(func() {
			ui.Text(f, i18n.T("连接方式")).FontSize(textSize(f, 12)).Weight(gfx.WeightMedium)
			connectionSelector(f, form.route).FillWidth()
		})
		ui.Column(f).FillWidth().Gap(4).Children(func() {
			ui.Text(f, i18n.T("对方 IP 地址")).FontSize(textSize(f, 12)).Weight(gfx.WeightMedium)
			ui.Row(f).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				available := connectionAvailable(state, *form.route) && !state.ConnectionFeedback.Pending
				field := ui.TextFieldID(f, form.addressID, form.address, i18n.T("对方 IP 地址"), connectionHint(state, *form.route)).Grow(1)
				submitted = available && field.Submitted()
				if !form.connectID.IsZero() && namedButton(f, form.connectID, i18n.T("连接")).Small().Outline().Disabled(!available).Clicked() {
					submitted = true
				}
			})
			if !connectionAvailable(state, *form.route) {
				ui.Paragraph(f, connectionProblem(state, *form.route), 2).FontSize(textSize(f, 11)).Muted()
			}
		})
	})
	return submitted
}

func connectionSelector(c ui.C, route *connectionRoute) ui.Box {
	return ui.Select(c.Part("connection-route"), route, connectionRoutes[:], connectionRoute.label).
		Label(i18n.T("连接方式"))
}

type compatibilityEditor struct {
	field                    ui.Field
	portID, toggleID, saveID gfx.ID
	source                   domain.CompatibilityConfig
	ready                    bool
}

func (e *compatibilityEditor) init(tree *gfx.Tree, scope gfx.Scope) {
	e.portID = tree.ID(scope, "port")
	e.toggleID = tree.ID(scope, "enabled")
	e.saveID = tree.ID(scope, "save")
}

func (p *Panels) drawCompatibility(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	p.compatibilitySection(f, theme, &p.settingsExtra.compat[0], domain.TransportNeiwangtong,
		"内网通互通", "UDP 发现端口；控制和传输使用独立端点。", domain.NeiwangtongDiscoveryPort,
		state.Settings.Neiwangtong, state.NeiwangtongStatus, submit)
	p.compatibilitySection(f, theme, &p.settingsExtra.compat[1], domain.TransportIPMSG,
		"飞鸽／飞秋互通", "文字使用 UDP；文件使用同一端口的 TCP。", domain.IPMSGPort,
		state.Settings.IPMSG, state.IPMSGStatus, submit)
}

func (p *Panels) compatibilitySection(f ui.C, theme ui.Theme, editor *compatibilityEditor,
	transport, title, description string, defaultPort int, cfg domain.CompatibilityConfig,
	status domain.CompatibilityStatus, submit func(domain.Command)) {
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if !editor.ready || editor.source != cfg {
		editor.ready, editor.source = true, cfg
		editor.field.SetText(strconv.Itoa(cfg.Port))
	}
	panelSection(f, i18n.T(title), i18n.T(description), func() {
		stateText := i18n.T("已关闭")
		if status.Active {
			stateText = i18n.T("运行中")
		}
		if status.Pending != nil {
			stateText = i18n.T("传输结束后生效")
		}
		if status.Problem != "" {
			stateText += " · " + i18n.T(status.Problem)
		}
		p.settingsRow(f, theme, i18n.T("启用")+i18n.T(title), stateText, func() {
			p.settingsControl(f, 84, func() {
				enabled := ui.State[bool](f, "compatibility-enabled")
				*enabled = cfg.Enabled
				if ui.Switch(f, enabled, "").Hit(editor.toggleID, i18n.T(title)).Disabled(status.Unavailable).Changed() {
					next := domain.CompatibilityConfig{
						Enabled: *enabled,
						Port:    compatibilitySavedPort(cfg, defaultPort),
					}
					p.localError = ""
					submit(domain.Command{Kind: domain.SetCompatibility, Transport: transport, Compatibility: next})
				}
			})
		})
		p.settingsRow(f, theme, i18n.T("端口"), fmt.Sprintf("%s %d", i18n.T("默认端口"), defaultPort), func() {
			p.settingsControl(f, 220, func() {
				ui.Row(f).FillWidth().Gap(8).Items(ui.Center).Children(func() {
					port, err := strconv.Atoi(strings.TrimSpace(editor.field.Text()))
					valid := err == nil && port >= 1 && port <= 65535
					next := domain.CompatibilityConfig{Enabled: cfg.Enabled, Port: port}
					changed := next != cfg || cfg.Enabled && !status.Active || status.Pending != nil
					field := ui.TextFieldID(f, editor.portID, &editor.field, i18n.T(title)+" · "+i18n.T("端口"), strconv.Itoa(defaultPort)).Grow(1)
					if namedButton(f, editor.saveID, i18n.T("保存")).Small().Outline().Disabled(!valid || !changed || status.Unavailable && transport == domain.TransportIPMSG).Clicked() ||
						field.Submitted() && valid && changed && !(status.Unavailable && transport == domain.TransportIPMSG) {
						p.localError = ""
						submit(domain.Command{Kind: domain.SetCompatibility, Transport: transport, Compatibility: next})
					}
				})
			})
		})
	})
}

func compatibilitySavedPort(cfg domain.CompatibilityConfig, defaultPort int) int {
	if cfg.Port >= 1 && cfg.Port <= 65535 {
		return cfg.Port
	}
	return defaultPort
}
