package desktop

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/firewall"
	"goim/internal/i18n"
)

func init() {
	if runtime.GOOS == "linux" {
		searchableSettings = append(searchableSettings, settingSearchItem{panelSettingsNetwork, "本机防火墙诊断", "扫描 发现 连接 端口 安全中心 麒麟 统信 firewall port ufw firewalld UOS Kylin"})
	}
}

type firewallPanel struct {
	config  firewall.Config
	result  firewall.Report
	failure string
	pending *ui.EffectState[firewall.Report, int]
	run     func(context.Context, firewall.Config, *firewall.Report) (firewall.Report, error)
}

func (v *firewallPanel) start(config firewall.Config, apply bool) {
	v.config = config
	var preview *firewall.Report
	if apply {
		copy := v.result
		preview = &copy
	}
	v.failure = ""
	run := v.run
	// Leaving the page cancels its effect. Returning must never replay an
	// authorization request or a partially applied change without a click.
	var once sync.Once
	var result firewall.Report
	var err error
	v.pending = ui.NewEffect[firewall.Report, int](func(ctx context.Context) (firewall.Report, error) {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			switch {
			case run != nil:
				result, err = run(ctx, config, preview)
			case preview != nil:
				result, err = firewall.Apply(ctx, config, *preview)
			default:
				result, err = firewall.Inspect(ctx, config)
			}
		})
		return result, err
	})
}

func (p *Panels) drawFirewall(f ui.C, theme ui.Theme, s domain.State, submit func(domain.Command)) {
	v := &p.firewall
	if runtime.GOOS != "linux" && v.run == nil {
		return
	}
	c := firewall.FromState(s)
	if v.config != c {
		v.config, v.result, v.failure, v.pending = c, firewall.Report{}, "", nil
	}
	if v.pending != nil {
		ui.Effect(f, v.pending, 1)
		if report, err, done := v.pending.Take(); done {
			v.pending, v.result = nil, report
			if err != nil {
				switch {
				case errors.Is(err, firewall.ErrCancelled):
					v.failure = i18n.T("已取消系统授权。")
				case errors.Is(err, firewall.ErrAuthorization):
					v.failure = i18n.T("系统未授予权限，请检查桌面授权服务或联系管理员。")
				default:
					v.failure = i18n.T("检查未完成。") + " " + err.Error()
				}
				v.result.CanApply = false
			}
		}
	}
	panelSection(f, i18n.T("网络诊断"), "", func() {
		description := i18n.T("检查当前网卡上的 IPv4 发现与连接端口。读取部分系统策略需要管理员授权。")
		if v.pending != nil {
			description = i18n.T("正在检查本机规则，请留意系统授权窗口…")
		}
		p.settingsRow(f, theme, i18n.T("本机防火墙诊断"), description, func() {
			if ui.Button(f.Part("inspect-firewall"), i18n.T("开始诊断")).Outline().Disabled(v.pending != nil).Clicked() {
				v.start(c, false)
				p.invalidate()
			}
		})
		report := v.result
		if report.Message == "" && v.failure == "" {
			return
		}
		if !c.Discovery {
			ui.Paragraph(f, i18n.T("自动发现当前已关闭或处于隐身状态。"), 3).FillWidth()
		}
		if report.Interface != "" {
			scope := []string{report.Interface, report.Subnet}
			if report.Manager != "" {
				scope = append(scope, report.Manager)
			}
			if report.Zone != "" {
				scope = append(scope, report.Zone)
			}
			ui.Paragraph(f, strings.Join(scope, " · "), 3).FillWidth()
		}
		if report.Message != "" {
			ui.Paragraph(f, i18n.T(report.Message), 6).FillWidth()
		}
		if report.Guidance != "" {
			ui.Paragraph(f, i18n.T(report.Guidance), 4).Muted().FillWidth()
		}
		for _, rule := range report.Rules {
			label := fmt.Sprintf("%s · %s %d", i18n.T(rule.Purpose), strings.ToUpper(rule.Protocol), rule.Port)
			if rule.Destination != "" {
				label += " → " + rule.Destination
			}
			if slices.Contains(report.Blocked, rule) {
				label += " · " + i18n.T("已有拒绝规则")
			} else if slices.Contains(report.Missing, rule) {
				label += " · " + i18n.T("缺少显式规则")
			}
			ui.Paragraph(f, label, 3).Muted().FillWidth()
		}
		if v.failure != "" {
			ui.Paragraph(f, v.failure, 5).Color(theme.Danger).FillWidth()
		}
		if report.CanApply && ui.Button(f.Part("allow-firewall"), i18n.T("授权添加缺少的规则")).Disabled(v.pending != nil).Clicked() {
			v.start(c, true)
			p.invalidate()
		}
		if v.pending == nil && c.Discovery && report.Interface != "" &&
			ui.Button(f.Part("retry-lan-discovery"), i18n.T("重新发现联系人")).Outline().Clicked() {
			submit(domain.Command{Kind: domain.RefreshContacts})
		}
	})
}
