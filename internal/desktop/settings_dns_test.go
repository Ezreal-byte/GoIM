package desktop

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func TestNetworkSettingsSaveAndClearDNSDiscovery(t *testing.T) {
	h := panelTestNewHarnessBounds(t, panelModuleSettings, panelTestDefaultState(t), ui.Rect{X: 68, Width: 1372, Height: 2400})
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsNetwork)))
	e := &h.panels.settingsExtra.dns
	h.click(e.serverID)
	if err := h.driver.Type("10.20.1.53"); err != nil {
		t.Fatal(err)
	}
	h.click(e.domainID)
	if err := h.driver.Type("office.test"); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	cmd := h.requireLastCommand()
	if cmd.Setting != "dns_discovery" || cmd.DNSDiscovery.Server != "10.20.1.53:53" || cmd.DNSDiscovery.Domain != "office.test" {
		t.Fatalf("save = %+v", cmd)
	}
	state := panelTestDefaultState(t)
	state.Settings.DNSDiscovery = cmd.DNSDiscovery
	h.publish(state)
	h.click(e.serverID)
	if _, err := h.driver.Shortcut('a'); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	h.click(e.saveID)
	if cmd = h.requireLastCommand(); cmd.Setting != "dns_discovery" || cmd.DNSDiscovery != (domain.DNSDiscoveryConfig{}) {
		t.Fatalf("clear = %+v", cmd)
	}
}
