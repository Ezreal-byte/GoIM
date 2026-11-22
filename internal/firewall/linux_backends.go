package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type commands struct {
	has            func(string) bool
	run            func(context.Context, bool, string, ...string) (string, error)
	firewalldOwner func(context.Context) (bool, error)
}

var ErrAuthorization = errors.New("system authorization unavailable")
var ErrCancelled = errors.New("system authorization cancelled")

func inspect(ctx context.Context, r Report, sys commands) (Report, error) {
	if r.Message != "" {
		return r, nil
	}
	firewalld, ufw := false, false
	if sys.has("firewall-cmd") {
		var err error
		firewalld, err = sys.firewalldOwner(ctx)
		if err != nil {
			return r, err
		}
	}
	if firewalld {
		out, err := sys.run(ctx, false, "firewall-cmd", "--state")
		if err != nil || out != "running" {
			return r, unexpected("firewall-cmd", out, err)
		}
	}
	var status string
	if sys.has("ufw") {
		var err error
		status, err = sys.run(ctx, false, "ufw", "status")
		if err != nil && strings.Contains(strings.ToLower(status), "root") {
			status, err = sys.run(ctx, true, "ufw", "status")
		}
		if err != nil {
			return r, err
		}
		line, _, _ := strings.Cut(status, "\n")
		switch strings.TrimSpace(line) {
		case "Status: active":
			ufw = true
		case "Status: inactive":
		default:
			return r, unexpected("ufw", line, nil)
		}
	}
	switch {
	case firewalld && ufw:
		r.Manager = "firewalld + UFW"
		r.Message = "两个防火墙管理器都在运行，请由管理员检查规则优先级。"
		return r, nil
	case firewalld:
		r.Manager = "firewalld"
		zone, err := sys.run(ctx, false, "firewall-cmd", "--get-zone-of-interface="+r.Interface)
		if zone == "no zone" {
			zone, err = sys.run(ctx, false, "firewall-cmd", "--get-default-zone")
		}
		if err != nil || zone == "" || strings.ContainsAny(zone, " \t\r\n") {
			return r, unexpected("firewall-cmd", zone, err)
		}
		r.Zone = zone
		for _, rule := range r.Rules {
			runtime, err := firewalldAllows(ctx, sys, r, rule, false)
			if err != nil {
				return r, err
			}
			permanent, err := firewalldAllows(ctx, sys, r, rule, true)
			if err != nil {
				return r, err
			}
			if !runtime || !permanent {
				r.Missing = append(r.Missing, rule)
			}
		}
	case ufw:
		r.Manager = "UFW"
		for _, rule := range r.Rules {
			switch ufwVerdict(status, r, rule) {
			case "DENY", "REJECT":
				r.Blocked = append(r.Blocked, rule)
			case "":
				r.Missing = append(r.Missing, rule)
			}
		}
		if len(r.Blocked) > 0 {
			r.Message = "发现匹配的 UFW 入站拒绝规则，请管理员核对规则顺序后再试。"
			return r, nil
		}
	default:
		return inspectKernelPolicy(ctx, r, sys)
	}
	r.CanApply = len(r.Missing) > 0
	r.Message = "已找到所列端口的显式放行规则。仍无法发现时，请检查对端、防火墙优先级和网络组播隔离。"
	if r.CanApply {
		r.Message = "部分端口未找到匹配的显式放行规则。其他策略也可能已允许这些流量。"
	}
	return r, nil
}

// A vendor security center may own the kernel rules directly. Show evidence
// from the INPUT path while leaving all edits to that policy's owner.
func inspectKernelPolicy(ctx context.Context, r Report, sys commands) (Report, error) {
	r.Message = "未检测到活动的 firewalld 或 UFW。请在系统安全中心的网络防护中核对下列端口和 GoIM 的联网权限。"
	read := func(name string, args ...string) (string, error) {
		output, err := sys.run(ctx, false, name, args...)
		msg := strings.ToLower(output)
		if err != nil && (strings.Contains(msg, "permission denied") || strings.Contains(msg, "operation not permitted") || strings.Contains(msg, "must be root")) {
			return sys.run(ctx, true, name, args...)
		}
		return output, err
	}
	const drop = "检测到 IPv4 入站链的默认策略为丢弃。请管理员在系统安全中心核对下列端口的例外规则。"
	if sys.has("nft") {
		output, err := read("nft", "-j", "list", "chains")
		if err == nil {
			drops, err := nftInputDrops(output)
			if err != nil {
				return r, err
			}
			if drops {
				r.Manager, r.Message = "nftables", drop
				return r, nil
			}
		} else {
			// Some older nft versions do not support listing chains as JSON.
			msg := strings.ToLower(output)
			unsupported := strings.Contains(msg, "syntax error") || strings.Contains(msg, "invalid option") || strings.Contains(msg, "not supported") || strings.Contains(msg, "unrecognized option")
			if !unsupported || !sys.has("iptables") {
				return r, err
			}
		}
	}
	if sys.has("iptables") {
		output, err := read("iptables", "-S", "INPUT")
		if err != nil {
			return r, err
		}
		if slices.Contains(strings.Split(output, "\n"), "-P INPUT DROP") {
			r.Manager, r.Message = "iptables", drop
		}
	}
	return r, nil
}

func nftInputDrops(output string) (bool, error) {
	var data struct {
		Nftables []struct {
			Chain struct{ Family, Hook, Policy string }
		}
	}
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		return false, err
	}
	for _, entry := range data.Nftables {
		c := entry.Chain
		if (c.Family == "ip" || c.Family == "inet") && c.Hook == "input" && c.Policy == "drop" {
			return true, nil
		}
	}
	return false, nil
}

func unexpected(tool, output string, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s: unexpected response %.240q", tool, output)
}

func exitCode(err error) int {
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

func richRule(r Report, rule Rule) string {
	s := fmt.Sprintf(`rule family="ipv4" source address="%s"`, r.Subnet)
	if rule.Destination != "" {
		s += fmt.Sprintf(` destination address="%s"`, rule.Destination)
	}
	return s + fmt.Sprintf(` port port="%d" protocol="%s" accept`, rule.Port, rule.Protocol)
}

func zoneArgs(zone string, permanent bool, operation string) []string {
	args := []string{"--zone=" + zone, operation}
	if permanent {
		args = append(args, "--permanent")
	}
	return args
}

func firewalldAllows(ctx context.Context, sys commands, r Report, rule Rule, permanent bool) (bool, error) {
	for _, query := range []string{fmt.Sprintf("--query-port=%d/%s", rule.Port, rule.Protocol), "--query-rich-rule=" + richRule(r, rule)} {
		out, err := sys.run(ctx, false, "firewall-cmd", zoneArgs(r.Zone, permanent, query)...)
		if err == nil && out == "yes" {
			return true, nil
		}
		if exitCode(err) != 1 || out != "no" {
			return false, unexpected("firewall-cmd", out, err)
		}
	}
	return false, nil
}

func ensureRichRule(ctx context.Context, sys commands, r Report, rule Rule) error {
	for _, permanent := range []bool{false, true} {
		ok, err := firewalldAllows(ctx, sys, r, rule, permanent)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := sys.run(ctx, true, "firewall-cmd", zoneArgs(r.Zone, permanent, "--add-rich-rule="+richRule(r, rule))...); err != nil {
				return err
			}
		}
	}
	return nil
}
