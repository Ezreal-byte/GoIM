package desktop

import (
	"os"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/buildinfo"
	"goim/internal/domain"
)

func TestSettingsSearchNavigatesToLiveControl(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{Width: 1040, Height: 600})
	search := h.panels.ids.settingsSearch
	h.typeInto(search, "dns srv")
	h.clickLabelRole("发现域名", gfx.RoleButton)
	if err := h.driver.Advance(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if !h.panels.settingsExtra.dns.advanced.Open || h.panels.search.field.Text() != "" {
		t.Fatal("DNS result did not open the advanced settings")
	}
	if _, ok := h.findHit(h.panels.settingsExtra.dns.domainID); !ok {
		t.Fatal("matched DNS field is outside the viewport")
	}
	h.typeInto(search, "xyz-missing")
	if !h.hasText("没有匹配的设置") {
		t.Fatal("missing empty search state")
	}
	if err := h.driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if h.panels.search.field.Text() != "" {
		t.Fatal("Escape did not exit search")
	}
	h.typeInto(search, "gItHuB 图标")
	if err := h.driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	h.click(h.panels.ids.settingsShowGitHub)
	if cmd := h.requireLastCommand(); cmd.Kind != domain.SetSetting || cmd.Setting != "show_title_github" || cmd.Bool {
		t.Fatalf("wrong toggle command: %+v", cmd)
	}
}

func TestMainTitleGitHubAndContactRefresh(t *testing.T) {
	state := shellTestState(t)
	h := newShellTestHarness(t, state)
	lan, hasLAN := h.findHit(h.ui.ids.titleLAN)
	pin, hasPin := h.findHit(h.ui.ids.mainPin)
	link, ok := h.findHit(h.ui.ids.titleGitHub)
	if !ok || !hasLAN || !hasPin || lan.Bounds.Right() > pin.Bounds.X || pin.Bounds.Right() > link.Bounds.X {
		t.Fatal("GitHub icon is out of order")
	}
	h.clickID(h.ui.ids.titleGitHub)
	if url, ok := h.driver.LastOpenedURL(); !ok || url != buildinfo.RepositoryURL {
		t.Fatal("GitHub button did not open the GoIM repository", url)
	}
	state.Settings.HideTitleGitHub = true
	state.Revision++
	h.publish(state)
	if _, ok := h.findHit(h.ui.ids.titleGitHub); ok {
		t.Fatal("GitHub button still visible")
	}
	h.clickID(h.ui.ids.navContacts)
	h.clickID(h.ui.ids.sidebarRefresh)
	if cmd := h.lastCommand(); cmd.Kind != domain.RefreshContacts {
		t.Fatalf("refresh: %+v", cmd)
	}
	state.ContactRefresh = domain.ContactRefresh{Pending: true, Sequence: 1}
	h.publish(state)
	h.waitFor(func() bool { return h.ui.state.ContactRefresh.Sequence == 1 })
	if !h.hasText("正在刷新局域网联系人") {
		t.Fatal("refresh did not start")
	}
	state.Peers = append(state.Peers, state.Self,
		domain.Peer{ID: "hidden", ContactHidden: true, Online: true},
		domain.Peer{ID: "compat", Transport: domain.TransportIPMSG, ClientType: domain.ClientNeiwangtong, Online: true})
	state.ContactRefresh = domain.ContactRefresh{Sequence: 2}
	h.publish(state)
	h.waitFor(func() bool { return h.ui.state.ContactRefresh.Sequence == 2 })
	if !h.hasText("联系人刷新完成") || !h.hasText("当前有 2 位在线联系人") || h.hasText("正在刷新局域网联系人") {
		t.Fatal("completion did not replace pending feedback")
	}
	h.publish(state)
	count := 0
	for _, notice := range h.ui.toasts {
		if notice.message.Title == "联系人刷新完成" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("repeated completion toast: %d", count)
	}
}

func TestSettingsSearchScreenshotQA(t *testing.T) {
	dir := os.Getenv("GOIM_QA_SCREENSHOTS")
	if dir == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to save the search results")
	}
	for _, theme := range []string{"light", "dark"} {
		state := shellTestState(t)
		state.Settings.Theme = theme
		shell, session := openShellQA(t, state, 1040, 680)
		shellQAClick(t, session, shell.ids.navSettings)
		shellQAClick(t, session, shell.panels.ids.settingsSearch)
		if err := session.Driver().Type("网络"); err != nil {
			t.Fatal(err)
		}
		if err := session.Expect("layout", "clean"); err != nil {
			t.Fatal(err)
		}
		shellQAShot(t, session, dir, "settings-search-"+theme+".png")
	}
}

func BenchmarkSettingsSearchPaint(b *testing.B) {
	state := domain.State{Settings: domain.DefaultSettings(b.TempDir())}
	shell := New(newShellTestBackend(state))
	defer shell.Close()
	shell.section = sectionSettings
	shell.panels.search.field.SetText("网络")
	driver, err := app.NewDriver(shell.Config(), nil)
	if err != nil {
		b.Fatal(err)
	}
	defer driver.Close()
	if len(shell.panels.search.results) == 0 {
		b.Fatal("missing search results")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		shell.invalidator.Invalidate()
		if err := driver.Pump(); err != nil {
			b.Fatal(err)
		}
	}
}
