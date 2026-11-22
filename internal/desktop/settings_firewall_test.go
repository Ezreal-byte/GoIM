package desktop

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/firewall"
)

func TestFirewallSettingsPreviewApplyAndRediscover(t *testing.T) {
	state := panelTestDefaultState(t)
	state.ListenAddress = "0.0.0.0:47842"
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{Width: 1200, Height: 1000})
	var reads, writes atomic.Int32
	rule := firewall.Rule{Port: 47842, Protocol: "tcp", Destination: "192.0.2.2", Purpose: "GoIM 连接"}
	preview := firewall.Report{Manager: "UFW", Interface: "lan0", Subnet: "192.0.2.0/24", Rules: []firewall.Rule{rule}, Missing: []firewall.Rule{rule}, Message: "部分端口未找到匹配的显式放行规则。其他策略也可能已允许这些流量。", CanApply: true}
	h.panels.firewall.run = func(_ context.Context, _ firewall.Config, confirmation *firewall.Report) (firewall.Report, error) {
		if confirmation == nil {
			reads.Add(1)
			return preview, nil
		}
		if !confirmation.CanApply || confirmation.Subnet != preview.Subnet {
			t.Error("missing preview")
		}
		writes.Add(1)
		done := preview
		done.Missing, done.CanApply = nil, false
		done.Message = "已找到所列端口的显式放行规则。仍无法发现时，请检查对端、防火墙优先级和网络组播隔离。"
		return done, nil
	}
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsNetwork)))
	if _, ok := h.findHitLabelRole("授权添加缺少的规则", gfx.RoleButton); ok || reads.Load() != 0 || writes.Load() != 0 {
		t.Fatal("network settings should be passive")
	}
	wait := func() {
		t.Helper()
		timeout := time.After(3 * time.Second)
		for h.panels.firewall.pending != nil {
			select {
			case <-h.driver.Wakes():
				if err := h.driver.Pump(); err != nil {
					t.Fatal(err)
				}
			case <-timeout:
				t.Fatal("firewall effect did not settle")
			}
		}
	}
	h.clickLabelRole("开始诊断", gfx.RoleButton)
	wait()
	if reads.Load() != 1 || writes.Load() != 0 || !h.hasText("lan0 · 192.0.2.0/24 · UFW") {
		t.Fatal("diagnosis did not render scope", h.panels.firewall.result)
	}
	h.clickLabelRole("授权添加缺少的规则", gfx.RoleButton)
	wait()
	if writes.Load() != 1 || h.panels.firewall.result.CanApply {
		t.Fatal("apply should finish exactly once")
	}
	h.clickLabelRole("重新发现联系人", gfx.RoleButton)
	if cmd := h.requireLastCommand(); cmd.Kind != domain.RefreshContacts {
		t.Fatal(cmd)
	}
	state.ListenAddress = "0.0.0.0:48800"
	h.publish(state)
	if h.panels.firewall.result.Manager != "" || writes.Load() != 1 {
		t.Fatal("changed listener left stale results")
	}
}
