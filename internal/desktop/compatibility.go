package desktop

import (
	"fmt"
	"github.com/vivaui/viva/gfx"
	"strconv"
	"strings"

	"github.com/vivaui/viva/ui"
	"goim/internal/flex"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func connectionLabels() []string {
	return []string{i18n.T("自动"), "GoIM", i18n.T("内网通原生"), i18n.T("飞鸽／飞秋兼容")}
}

func connectionTransport(index int) string {
	switch index {
	case 1:
		return domain.TransportGoIM
	case 2:
		return domain.TransportNeiwangtong
	case 3:
		return domain.TransportIPMSG
	default:
		return domain.TransportAuto
	}
}

func connectionAvailable(s domain.State, index int) bool {
	switch index {
	case 2:
		return s.NeiwangtongStatus.Active
	case 3:
		return s.IPMSGStatus.Active
	default:
		return true
	}
}

func connectionProblem(s domain.State, index int) string {
	if index < 2 {
		return ""
	}
	status := s.IPMSGStatus
	if index == 2 {
		status = s.NeiwangtongStatus
	}
	if status.Problem != "" {
		return i18n.T(status.Problem)
	}
	return i18n.T("请先在网络设置中开启此互通服务。")
}

func connectionHint(s domain.State, index int) string {
	port := 47842
	if index == 2 {
		port = s.Settings.Neiwangtong.Port
		if port == 0 {
			port = domain.NeiwangtongDiscoveryPort
		}
	} else if index == 3 {
		port = s.Settings.IPMSG.Port
		if port == 0 {
			port = domain.IPMSGPort
		}
	}
	if index < 2 {
		return i18n.T("例如 192.168.1.12 或 192.168.1.12:47842")
	}
	return fmt.Sprintf("192.168.1.12:%d", port)
}

type panelConnectionFields struct {
	protocol    gfx.Scope
	selectState *flex.SelectState
	route       *int
	address     *flex.Field
	addressID   gfx.ID
	connectID   gfx.ID
}

// panelConnectionForm draws the manual-connect fields shared by the connection
// dialog and Settings. The caller owns the submitted command and its feedback.
func panelConnectionForm(f *flex.Frame, state domain.State, form panelConnectionFields) (address, transport string, submitted bool) {
	if index, picked := f.Select(form.protocol, form.selectState, connectionLabels(), *form.route).
		FillWidth().Label(i18n.T("连接方式")).Picked(); picked {
		*form.route = index
	}
	available := connectionAvailable(state, *form.route)
	f.Row().FillWidth().Gap(8).Items(flex.Center).Children(func() {
		field := f.Field(form.address, form.addressID, i18n.T("对方 IP 地址"), connectionHint(state, *form.route)).Grow(1)
		if f.Button(form.connectID, i18n.T("连接")).Small().Outline().Disabled(!available).Clicked() ||
			available && field.Submitted() {
			address, transport, submitted = strings.TrimSpace(form.address.Text()), connectionTransport(*form.route), true
		}
	})
	return address, transport, submitted
}

type compatibilityEditor struct {
	field                    flex.Field
	portID, toggleID, saveID gfx.ID
	source                   domain.CompatibilityConfig
	ready                    bool
}

func (e *compatibilityEditor) init(tree *gfx.Tree, scope gfx.Scope) {
	e.portID = tree.ID(scope, "port")
	e.toggleID = tree.ID(scope, "enabled")
	e.saveID = tree.ID(scope, "save")
}

func (p *Panels) drawCompatibility(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	p.compatibilitySection(f, theme, &p.settingsExtra.compat[0], domain.TransportNeiwangtong,
		"内网通互通", "UDP 发现端口；控制和传输使用独立端点。", domain.NeiwangtongDiscoveryPort,
		state.Settings.Neiwangtong, state.NeiwangtongStatus, submit)
	p.compatibilitySection(f, theme, &p.settingsExtra.compat[1], domain.TransportIPMSG,
		"飞鸽／飞秋互通", "文字使用 UDP；文件使用同一端口的 TCP。", domain.IPMSGPort,
		state.Settings.IPMSG, state.IPMSGStatus, submit)
}

func (p *Panels) compatibilitySection(f *flex.Frame, theme ui.Theme, editor *compatibilityEditor,
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
				unavailable := status.Unavailable
				if f.Switch(editor.toggleID, cfg.Enabled).Label(i18n.T(title)).Disabled(unavailable).Clicked() {
					next := domain.CompatibilityConfig{
						Enabled: !cfg.Enabled,
						Port:    compatibilitySavedPort(cfg, defaultPort),
					}
					p.localError = ""
					submit(domain.Command{Kind: domain.SetCompatibility, Transport: transport, Compatibility: next})
				}
			})
		})
		p.settingsRow(f, theme, i18n.T("端口"), fmt.Sprintf("%s %d", i18n.T("默认端口"), defaultPort), func() {
			p.settingsControl(f, 220, func() {
				f.Row().FillWidth().Gap(8).Items(flex.Center).Children(func() {
					port, err := strconv.Atoi(strings.TrimSpace(editor.field.Text()))
					valid := err == nil && port >= 1 && port <= 65535
					next := domain.CompatibilityConfig{Enabled: cfg.Enabled, Port: port}
					changed := next != cfg || cfg.Enabled && !status.Active || status.Pending != nil
					field := f.Field(&editor.field, editor.portID, i18n.T(title)+" · "+i18n.T("端口"), strconv.Itoa(defaultPort)).Grow(1)
					if f.Button(editor.saveID, i18n.T("保存")).Small().Outline().Disabled(!valid || !changed || status.Unavailable && transport == domain.TransportIPMSG).Clicked() ||
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
