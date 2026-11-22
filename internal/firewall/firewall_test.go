package firewall

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"goim/internal/domain"
)

func testPlan() Report {
	return plan(Config{Listen: "[::]:48777", Discovery: true}, []link{{"lan0", netip.MustParsePrefix("192.0.2.9/25")}})
}

func TestActiveListenersAndScope(t *testing.T) {
	lan := link{"lan0", netip.MustParsePrefix("192.0.2.9/25")}
	other := link{"vpn0", netip.MustParsePrefix("198.51.100.3/24")}
	state := domain.State{ListenAddress: "[::]:48777"}
	state.Settings.Network = "lan0"
	state.Settings.IPMSG.Port, state.Settings.Neiwangtong.Port = 3001, 3002
	state.IPMSGStatus.Active, state.NeiwangtongStatus.Active = true, true
	got := plan(FromState(state), []link{lan, other})
	want := []Rule{
		{48777, "tcp", "192.0.2.9", "GoIM 连接"}, {47841, "udp", "239.192.77.77", "GoIM 组播发现"},
		{3001, "udp", "", "飞鸽／飞秋发现"}, {3001, "tcp", "192.0.2.9", "飞鸽／飞秋连接"},
		{3002, "udp", "", "内网通发现"}, {9012, "udp", "192.0.2.9", "内网通控制连接"},
	}
	if got.Interface != "lan0" || got.Subnet != "192.0.2.0/25" || !slices.Equal(want, got.Rules) {
		t.Fatalf("plan: %+v", got)
	}
	state.IPMSGStatus.Active, state.NeiwangtongStatus.Active = false, false
	state.Self.Presence = domain.PresenceInvisible
	if rules := plan(FromState(state), []link{lan}).Rules; !slices.Equal(rules, want[:1]) {
		t.Fatal(rules)
	}
	state.Self.Presence = domain.PresenceOnline
	state.DiscoveryDisabled = true
	if FromState(state).Discovery {
		t.Fatal("disabled startup discovery")
	}
	for _, cfg := range []Config{{Listen: ""}, {Listen: "127.0.0.1:80"}, {Listen: "[2001:db8::1]:99"}, {Listen: "0.0.0.0:80"}, {Listen: "192.0.2.10:80"}, {Listen: "0.0.0.0:80", Network: "absent"}} {
		r := plan(cfg, []link{lan, other})
		if r.Message == "" || r.Interface != "" || r.CanApply {
			t.Fatal(cfg, r)
		}
	}
	if r := plan(Config{Listen: "198.51.100.3:99"}, []link{lan, other}); r.Interface != "vpn0" {
		t.Fatal(r)
	}
}

type testExit int

func (testExit) Error() string   { return "test command failed" }
func (e testExit) ExitCode() int { return int(e) }

func TestFirewalldPartialApplyAndRetry(t *testing.T) {
	ctx := context.Background()
	current := testPlan()
	rules := map[string]bool{"--zone=work|--query-port=48777/tcp": true}
	var writes []string
	interrupted := false
	sys := commands{has: func(s string) bool { return s == "firewall-cmd" }, firewalldOwner: func(context.Context) (bool, error) { return true, nil },
		run: func(_ context.Context, priv bool, name string, args ...string) (string, error) {
			switch args[0] {
			case "--state":
				return "running", nil
			case "--get-zone-of-interface=lan0":
				return "no zone", testExit(2)
			case "--get-default-zone":
				return "work", nil
			}
			key := strings.Join(args, "|")
			if !strings.HasPrefix(key, "--zone=work|") {
				t.Fatalf("unexpected zone %s", key)
			}
			if strings.Contains(key, "--add-rich-rule=") {
				if !priv || !strings.Contains(key, `source address="192.0.2.0/25"`) || !strings.Contains(key, `destination address="`) {
					t.Fatalf("unscoped write %s", key)
				}
				if len(writes) == 1 && !interrupted {
					interrupted = true
					return "", errors.New("cancelled")
				}
				writes = append(writes, key)
				rules[strings.Replace(key, "--add-rich-rule=", "--query-rich-rule=", 1)] = true
				return "success", nil
			}
			if priv || !strings.Contains(key, "--query-") {
				t.Fatal("unexpected command", args)
			}
			if rules[key] {
				return "yes", nil
			}
			return "no", testExit(1)
		}}
	before, err := inspect(ctx, current, sys)
	if err != nil || len(before.Missing) != 2 || before.Zone != "work" || !before.CanApply {
		t.Fatal(before, err)
	}
	partial, err := apply(ctx, current, before, sys)
	if err == nil || partial.CanApply || len(writes) != 1 {
		t.Fatal(partial, writes, err)
	}
	retry, err := inspect(ctx, current, sys)
	if err != nil || len(retry.Missing) != 1 {
		t.Fatal(retry, err)
	}
	done, err := apply(ctx, current, retry, sys)
	if err != nil || len(done.Missing) != 0 || done.CanApply || len(writes) != 3 {
		t.Fatal(done, writes, err)
	}
	_, err = apply(ctx, current, before, sys)
	if err != nil || len(writes) != 3 {
		t.Fatal("duplicated entries", writes, err)
	}
}

func TestUFWPreviewAndChangedPolicy(t *testing.T) {
	ctx := context.Background()
	current := testPlan()
	status := "Status: active\n48777/tcp ALLOW Anywhere"
	writes := 0
	sys := commands{has: func(s string) bool { return s == "ufw" }, run: func(_ context.Context, priv bool, _ string, args ...string) (string, error) {
		if args[0] == "status" {
			if !priv {
				return "ERROR: You need to be root to run this script", testExit(1)
			}
			return status, nil
		}
		want := []string{"allow", "in", "on", "lan0", "from", "192.0.2.0/25", "to", "239.192.77.77", "port", "47841", "proto", "udp"}
		if !priv || !slices.Equal(want, args) {
			t.Fatal(args)
		}
		writes++
		status += "\n239.192.77.77 47841/udp on lan0 ALLOW IN 192.0.2.0/25"
		return "", nil
	}}
	preview, err := inspect(ctx, current, sys)
	if err != nil || len(preview.Missing) != 1 || !preview.CanApply {
		t.Fatal(preview, err)
	}
	initial := status
	status = "Status: active"
	changed, err := apply(ctx, current, preview, sys)
	if err != nil || changed.CanApply || writes != 0 {
		t.Fatal(changed, err)
	}
	status = initial
	done, err := apply(ctx, current, preview, sys)
	if err != nil || done.CanApply || len(done.Missing) != 0 || writes != 1 {
		t.Fatal(done, writes, err)
	}
	status = "Status: active\nAnywhere on lan0 DENY IN Anywhere\n" + status
	blocked, err := apply(ctx, current, preview, sys)
	if err != nil || len(blocked.Blocked) != 2 || blocked.CanApply || writes != 1 {
		t.Fatal(blocked, err)
	}
}

func TestUFWDirectionScopeAndExactPort(t *testing.T) {
	r := testPlan()
	rule := r.Rules[0]
	for _, row := range []string{"48777/udp ALLOW IN Anywhere", "48777/tcp on vpn0 ALLOW Anywhere", "48777/tcp ALLOW OUT Anywhere", "148777/tcp ALLOW Anywhere", "48777/tcp ALLOW 192.0.2.128/25", "192.0.2.10 48777/tcp ALLOW Anywhere", "48777/tcp (v6) ALLOW Anywhere (v6)"} {
		if v := ufwVerdict(row, r, rule); v != "" {
			t.Fatal(row, v)
		}
	}
}

func TestUFWSourcePrefixesAndRuleOrder(t *testing.T) {
	r := testPlan()
	for _, tc := range []struct{ rules, want string }{
		{"ALLOW IN 192.0.0.0/16", "ALLOW"},
		{"ALLOW IN 192.0.2.0/26", ""},
		{"DENY IN 192.0.2.7\n48777/tcp ALLOW IN Anywhere", "DENY"},
		{"REJECT IN 192.0.2.64/26", "REJECT"},
		{"DENY IN 192.0.2.128/25", ""},
		{"ALLOW IN 192.0.2.0/24\n48777/tcp DENY IN 192.0.2.7", "ALLOW"},
	} {
		if got := ufwVerdict("48777/tcp "+tc.rules, r, r.Rules[0]); got != tc.want {
			t.Errorf("rules %q: got %q, want %q", tc.rules, got, tc.want)
		}
	}
}

func TestVendorInputPolicyIsReadOnly(t *testing.T) {
	for _, tool := range []string{"nft", "iptables"} {
		sys := commands{has: func(name string) bool { return name == tool }, run: func(_ context.Context, _ bool, name string, args ...string) (string, error) {
			if name == "iptables" && slices.Equal(args, []string{"-S", "INPUT"}) {
				return "-P INPUT DROP\n-A INPUT -j ACCEPT", nil
			}
			if name == "nft" && slices.Equal(args, []string{"-j", "list", "chains"}) {
				return `{"nftables":[{"chain":{"family":"inet","hook":"input","policy":"drop"}}]}`, nil
			}
			t.Fatal(args)
			return "", nil
		}}
		got, err := inspect(context.Background(), testPlan(), sys)
		if err != nil || !strings.HasPrefix(got.Manager, tool) || got.CanApply || !strings.Contains(got.Message, "默认策略为丢弃") {
			t.Fatal(got, err)
		}
	}
}

func TestTwoManagersAndInactiveFirewalld(t *testing.T) {
	owned := true
	sys := commands{has: func(string) bool { return true }, firewalldOwner: func(context.Context) (bool, error) { return owned, nil }, run: func(_ context.Context, priv bool, name string, args ...string) (string, error) {
		if !priv && owned && name == "firewall-cmd" && args[0] == "--state" {
			return "running", nil
		}
		if !priv && name == "ufw" && args[0] == "status" {
			return "Status: active", nil
		}
		t.Fatalf("unexpected command %s %v", name, args)
		return "", nil
	}}
	got, err := inspect(context.Background(), testPlan(), sys)
	if err != nil || got.CanApply || got.Manager != "firewalld + UFW" {
		t.Fatal(got, err)
	}
	owned = false
	got, err = inspect(context.Background(), testPlan(), sys)
	if err != nil || !got.CanApply || got.Manager != "UFW" {
		t.Fatal(got, err)
	}
}
