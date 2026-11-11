package desktop

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func connectionLabels() []string {
	return []string{"GoIM", i18n.T("内网通原生"), i18n.T("飞鸽／飞秋兼容")}
}

func connectionTransport(index int) string {
	switch index {
	case 1:
		return domain.TransportNeiwangtong
	case 2:
		return domain.TransportIPMSG
	default:
		return domain.TransportGoIM
	}
}

func connectionAvailable(s domain.State, index int) bool {
	switch index {
	case 1:
		return s.NeiwangtongStatus.Active
	case 2:
		return s.IPMSGStatus.Active
	default:
		return true
	}
}

func connectionProblem(s domain.State, index int) string {
	status := s.IPMSGStatus
	if index == 1 {
		status = s.NeiwangtongStatus
	}
	if status.Problem != "" {
		return i18n.T(status.Problem)
	}
	return i18n.T("请先在网络设置中开启此互通服务。")
}

func connectionHint(s domain.State, index int) string {
	port := 47842
	if index == 1 {
		port = s.Settings.Neiwangtong.Port
		if port == 0 {
			port = domain.NeiwangtongDiscoveryPort
		}
	} else if index == 2 {
		port = s.Settings.IPMSG.Port
		if port == 0 {
			port = domain.IPMSGPort
		}
	}
	return fmt.Sprintf("192.168.1.12:%d", port)
}

type compatibilityEditor struct {
	field                    flex.Field
	portID, toggleID, saveID ui.ID
	source                   domain.CompatibilityConfig
	enabled, ready           bool
}

func (e *compatibilityEditor) init(tree *ui.Tree, scope ui.Scope) {
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
		editor.ready, editor.source, editor.enabled = true, cfg, cfg.Enabled
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
		panelSettingRow(f, theme, i18n.T("启用")+i18n.T(title), stateText, func() {
			panelControlColumn(f, 84, func() {
				unavailable := status.Unavailable
				if f.Switch(editor.toggleID, editor.enabled).Label(i18n.T(title)).Disabled(unavailable).Clicked() {
					editor.enabled = !editor.enabled
				}
			})
		})
		panelSettingRow(f, theme, i18n.T("端口"), fmt.Sprintf("%s %d", i18n.T("默认端口"), defaultPort), func() {
			panelControlColumn(f, 220, func() {
				f.Row().FillWidth().Gap(8).Items(flex.Center).Children(func() {
					port, err := strconv.Atoi(strings.TrimSpace(editor.field.Text()))
					valid := err == nil && port >= 1 && port <= 65535
					next := domain.CompatibilityConfig{Enabled: editor.enabled, Port: port}
					changed := next != cfg || cfg.Enabled && !status.Active || status.Pending != nil
					field := f.Field(&editor.field, editor.portID, i18n.T(title)+" · "+i18n.T("端口"), strconv.Itoa(defaultPort)).Grow(1)
					if f.Button(editor.saveID, i18n.T("保存")).Small().Outline().Disabled(!valid || !changed || status.Unavailable && transport == domain.TransportIPMSG).Clicked() ||
						field.Submitted() && valid && changed && !(status.Unavailable && transport == domain.TransportIPMSG) {
						submit(domain.Command{Kind: domain.SetCompatibility, Transport: transport, Compatibility: next})
					}
				})
			})
		})
	})
}
