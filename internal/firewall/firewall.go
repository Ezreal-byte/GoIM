// Package firewall checks explicit inbound LAN rules on the local desktop.
package firewall

import (
	"context"
	"fmt"

	"net/netip"
	"slices"
	"strings"

	"goim/internal/domain"
)

// Config is a snapshot of the listeners used for discovery and connections.
type Config struct {
	Network, Listen    string
	Discovery          bool
	IPMSG, Neiwangtong int
}

func FromState(s domain.State) Config {
	c := Config{Network: s.Settings.Network, Listen: s.ListenAddress,
		Discovery: !s.DiscoveryDisabled && !s.Settings.DisableAutoDiscovery && s.Self.Presence != domain.PresenceInvisible}
	if s.IPMSGStatus.Active {
		c.IPMSG = s.Settings.IPMSG.Port
	}
	if s.NeiwangtongStatus.Active {
		c.Neiwangtong = s.Settings.Neiwangtong.Port
	}
	return c
}

type Rule struct {
	Port                           int
	Protocol, Destination, Purpose string
}

type Report struct {
	Manager, Zone, Interface, Subnet, Message string
	Guidance                                  string
	Rules                                     []Rule
	// Missing includes a rule when its current or persistent entry is absent.
	Missing  []Rule
	Blocked  []Rule
	CanApply bool
}

type link struct {
	name    string
	address netip.Prefix // retains the local host bits
}

func plan(c Config, links []link) Report {
	var r Report
	bind, err := netip.ParseAddrPort(c.Listen)
	if err != nil || bind.Port() == 0 {
		r.Message = "未取得本机 TCP 监听端口，请先检查 GoIM 网络状态。"
		return r
	}
	ip := bind.Addr().Unmap()
	if ip.IsLoopback() || (!ip.Is4() && !ip.IsUnspecified()) {
		r.Message = "当前监听地址不能接收局域网 IPv4 连接，请先检查启动参数。"
		return r
	}
	var matches []link
	for _, l := range links {
		if c.Network != "" && c.Network != "auto" && c.Network != l.name {
			continue
		}
		if !ip.IsUnspecified() && ip != l.address.Addr() {
			continue
		}
		matches = append(matches, l)
	}
	var host string
	switch len(matches) {
	case 0:
		r.Message = "当前网络没有匹配的 IPv4 地址，请检查网线、无线连接和所选网卡。"
	case 1:
		l := matches[0]
		r.Interface, r.Subnet = l.name, l.address.Masked().String()
		host = l.address.Addr().String()
	default:
		r.Message = "检测到多个可用 IPv4 地址，请在“使用的网络”选择局域网网卡。若该网卡有多个地址，请由管理员指定放行范围。"
	}
	r.Rules = append(r.Rules, Rule{int(bind.Port()), "tcp", host, "GoIM 连接"})
	if c.Discovery {
		r.Rules = append(r.Rules, Rule{domain.LANDiscoveryPort, "udp", domain.LANDiscoveryGroup, "GoIM 组播发现"})
	}
	if c.IPMSG > 0 && c.IPMSG <= 65535 {
		r.Rules = append(r.Rules, Rule{c.IPMSG, "udp", "", "飞鸽／飞秋发现"}, Rule{c.IPMSG, "tcp", host, "飞鸽／飞秋连接"})
	}
	if c.Neiwangtong > 0 && c.Neiwangtong <= 65535 {
		r.Rules = append(r.Rules, Rule{c.Neiwangtong, "udp", "", "内网通发现"}, Rule{domain.NeiwangtongControlPort, "udp", host, "内网通控制连接"})
	}
	return r
}

func apply(ctx context.Context, current, preview Report, sys commands) (Report, error) {
	r, err := inspect(ctx, current, sys)
	if err != nil {
		return r, err
	}
	if !preview.CanApply || r.Manager != preview.Manager || r.Zone != preview.Zone ||
		r.Interface != preview.Interface || r.Subnet != preview.Subnet || !slices.Equal(r.Rules, preview.Rules) {
		r.Message = "网络或防火墙状态已变化，请重新诊断并确认规则。"
		r.CanApply = false
		return r, nil
	}
	if !r.CanApply {
		return r, nil
	}
	for _, rule := range r.Missing {
		if !slices.Contains(preview.Missing, rule) {
			r.Message = "网络或防火墙状态已变化，请重新诊断并确认规则。"
			r.CanApply = false
			return r, nil
		}
	}
	for _, rule := range r.Missing {
		if r.Manager == "UFW" {
			dest := rule.Destination
			if dest == "" {
				dest = "any"
			}
			_, err = sys.run(ctx, true, "ufw", "allow", "in", "on", r.Interface, "from", r.Subnet, "to", dest, "port", fmt.Sprint(rule.Port), "proto", rule.Protocol)
		} else {
			err = ensureRichRule(ctx, sys, r, rule)
		}
		if err != nil {
			r.CanApply = false
			r.Message = "添加规则未完成。已生效的条目会保留，重新诊断后可以继续。"
			return r, err
		}
	}
	r, err = inspect(ctx, current, sys)
	if err == nil && r.CanApply {
		r.Message = "复查仍有缺少的规则，请检查系统安全中心或联系管理员。"
	}
	return r, err
}

// UFW status uses separate columns for destination, action and source.
// Compare tokens rather than accepting a substring of a port or subnet.
func ufwVerdict(status string, r Report, rule Rule) string {
	for _, line := range strings.Split(status, "\n") {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		action := slices.IndexFunc(fields, func(field string) bool { return field == "ALLOW" || field == "DENY" || field == "REJECT" })
		if action < 1 {
			continue
		}
		to, from := fields[:action], fields[action+1:]
		if len(from) > 0 && from[0] == "IN" {
			from = from[1:]
		}
		if len(from) != 1 || (from[0] != r.Subnet && from[0] != "Anywhere") {
			continue
		}
		if i := slices.Index(to, "on"); i >= 0 {
			if i+2 != len(to) || to[i+1] != r.Interface {
				continue
			}
			to = to[:i]
		}
		if len(to) == 2 && to[0] == rule.Destination {
			to = to[1:]
		}
		if len(to) == 1 && (to[0] == "Anywhere" || to[0] == rule.Destination || to[0] == fmt.Sprint(rule.Port) || to[0] == fmt.Sprintf("%d/%s", rule.Port, rule.Protocol)) {
			return fields[action]
		}
	}
	return ""
}
