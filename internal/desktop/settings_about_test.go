package desktop

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/buildinfo"
	"goim/internal/domain"
	"goim/internal/i18n"
)

func aboutTestBuildInfo() panelBuildInfo {
	return panelBuildInfo{
		version:     "1.2.3",
		goVersion:   "go1.27.3",
		vivaVersion: "v0.6.5-0.20261115140306-89ce846fe65c",
		platform:    "Windows · x86-64",
		source:      "github-actions",
		revision:    "fedcba9876543210fedcba9876543210fedcba98",
		revisionDay: "2026-11-07",
		buildTime:   "2026-11-10 04:34 UTC",
	}
}

func aboutTestOpen(t *testing.T, state domain.State, theme ui.Theme, language string, bounds ui.Rect) *panelTestHarness {
	t.Helper()
	i18n.SetLanguage(language)
	t.Cleanup(func() { i18n.SetLanguage(i18n.Chinese) })
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, bounds)
	h.theme = theme
	h.panels.settingsTab = panelSettingsAbout
	h.panels.build = aboutTestBuildInfo()
	h.publish(state)
	return h
}

func aboutTestID(h *panelTestHarness, name string) gfx.ID {
	return h.panels.ids.aboutCards.ID(h.tree.Name(name))
}

func TestAboutCopyContainsOnlyBuildFacts(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Version = "1.2.3 (fedcba987654)"
	state.Settings.DownloadDir = t.TempDir()
	h := aboutTestOpen(t, state, ui.LightTheme(), i18n.Chinese, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1500,
	})

	h.click(h.panels.ids.aboutCopyVersion)
	clipboard := h.driver.Clipboard()
	for _, want := range []string{
		"GoIM 1.2.3",
		"平台: Windows · x86-64",
		"构建来源: GitHub Actions",
		"源码提交: fedcba9876543210fedcba9876543210fedcba98",
		"提交日期: 2026-11-07",
		"Go go1.27.3",
		"Viva v0.6.5-0.20261115140306-89ce846fe65c",
		"构建时间: 2026-11-10 12:34 GMT+8",
	} {
		if !strings.Contains(clipboard, want) {
			t.Errorf("copied build facts do not contain %q:\n%s", want, clipboard)
		}
	}
	if strings.Contains(clipboard, state.Settings.DownloadDir) {
		t.Fatalf("copied build facts contain a user path: %s", clipboard)
	}
	if strings.Contains(clipboard, "聊天内容") || strings.Contains(clipboard, "联系人") || strings.Contains(clipboard, "CI 链接") {
		t.Fatalf("copy should not include user data: %s", clipboard)
	}

	h.click(h.panels.ids.aboutCards.At(0))
	if h.driver.Clipboard() != clipboard {
		t.Fatal("clicking a build card did not copy the same build facts")
	}
}

func TestAboutUnknownFactsAreExplicitAndCommitTimeIsNotBuildTime(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Version = "test-build"
	build := aboutTestBuildInfo()
	build.buildTime = ""
	build.vivaVersion = ""
	report := panelVersionReport(state, build)

	if !strings.Contains(report, "提交日期: 2026-11-07") {
		t.Fatalf("commit date is missing: %s", report)
	}
	if !strings.Contains(report, "构建时间: 未提供") {
		t.Fatalf("missing build time was not explicit: %s", report)
	}
	if !strings.Contains(report, "Viva 未提供") || strings.Contains(report, "CI 链接") {
		t.Fatalf("unknown facts were fabricated: %s", report)
	}
}

func TestAboutExternalLinksAndCommunityAvailability(t *testing.T) {
	state := panelTestDefaultState(t)
	h := aboutTestOpen(t, state, ui.LightTheme(), i18n.Chinese, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1500,
	})

	h.click(aboutTestID(h, "project-repository"))
	if target, ok := h.driver.LastOpenedURL(); !ok || target != buildinfo.RepositoryURL {
		t.Fatalf("repository link = %q, %v", target, ok)
	}
	h.click(aboutTestID(h, "project-issues"))
	if target, ok := h.driver.LastOpenedURL(); !ok || target != buildinfo.IssuesURL {
		t.Fatalf("issues link = %q, %v", target, ok)
	}
	h.click(aboutTestID(h, "project-site"))
	if target, ok := h.driver.LastOpenedURL(); !ok || target != aboutSite {
		t.Fatalf("site link = %q, %v", target, ok)
	}
	h.click(aboutTestID(h, "community-wechat"))
	if target, ok := h.driver.LastOpenedURL(); !ok || target != aboutWeChat {
		t.Fatalf("WeChat link = %q, %v", target, ok)
	}
	h.click(aboutTestID(h, "community-qq"))
	if target, ok := h.driver.LastOpenedURL(); !ok || target != aboutQQURL {
		t.Fatalf("QQ link = %q, %v; want %q", target, ok, aboutQQURL)
	}
	if h.hasText("CI 运行") || !h.hasText("加入交流") || !h.hasText("项目") {
		t.Fatal("about page sections were not updated")
	}
	if h.hasText("系统诊断报告") || !h.hasText("导出脱敏报告") || !h.hasText("不含聊天内容") {
		t.Fatal("diagnostics and feedback were not merged into one redacted report row")
	}
}

func TestAboutUsesMottyBrandVectorMarks(t *testing.T) {
	qq := AboutQq
	if qq.ViewBox != 120 || qq.ViewHeight != 145 || qq.Stroke != 2 || len(qq.Commands) != 0 || len(qq.Layers) != 8 {
		t.Fatalf("QQ mark is not Motty's eight-layer 120x145 penguin: %+v", qq)
	}
	for index, layer := range qq.Layers {
		if len(layer.Commands) == 0 {
			t.Fatalf("QQ layer %d is empty", index)
		}
	}
	for _, want := range []gfx.Color{
		gfx.RGB(0xFA, 0xAB, 0x07),
		gfx.RGB(0x11, 0x11, 0x11),
		gfx.RGB(0xFF, 0xFF, 0xFF),
		gfx.RGB(0xEB, 0x19, 0x23),
	} {
		if !aboutIconHasColor(qq, want) {
			t.Errorf("QQ mark is missing Motty color %+v", want)
		}
	}

	for name, icon := range map[string]gfx.Icon{"WeChat": AboutWechat, "GitHub": AboutGithub} {
		if icon.ViewBox != 24 || icon.Stroke != 0 || len(icon.Commands) == 0 {
			t.Fatalf("%s mark is not Motty's filled 24px vector: %+v", name, icon)
		}
	}

	if aboutQQLinkMark.chip != gfx.RGB(0xFF, 0xFF, 0xFF) || aboutQQLinkMark.ink != gfx.RGB(0xFF, 0xFF, 0xFF) || aboutQQLinkMark.size != 21 {
		t.Fatalf("QQ mark styling does not match Motty: %+v", aboutQQLinkMark)
	}
	if aboutWeChatLinkMark.chip != gfx.RGB(0x07, 0xC1, 0x60) || aboutWeChatLinkMark.ink != gfx.RGB(0xFF, 0xFF, 0xFF) || aboutWeChatLinkMark.size != 16 {
		t.Fatalf("WeChat mark styling does not match Motty: %+v", aboutWeChatLinkMark)
	}
	if aboutGitHubLinkMark.chip != gfx.RGB(0xFF, 0xFF, 0xFF) || aboutGitHubLinkMark.ink != gfx.RGB(0x18, 0x17, 0x17) || aboutGitHubLinkMark.size != 17 {
		t.Fatalf("GitHub mark styling does not match Motty: %+v", aboutGitHubLinkMark)
	}
}

func aboutIconHasColor(icon gfx.Icon, want gfx.Color) bool {
	for _, layer := range icon.Layers {
		if math.Abs(float64(layer.Fill.Red-want.Red)) < 0.0001 &&
			math.Abs(float64(layer.Fill.Green-want.Green)) < 0.0001 &&
			math.Abs(float64(layer.Fill.Blue-want.Blue)) < 0.0001 &&
			math.Abs(float64(layer.Fill.Alpha-want.Alpha)) < 0.0001 {
			return true
		}
	}
	return false
}

func TestAboutCardsAdaptAndKeepLightDarkLanguages(t *testing.T) {
	state := panelTestDefaultState(t)
	for _, test := range []struct {
		name   string
		width  float32
		height float32
		theme  ui.Theme
		lang   string
		two    bool
		want   string
	}{
		{name: "wide light Chinese", width: 1372, height: 1500, theme: ui.LightTheme(), lang: i18n.Chinese, two: true, want: "构建信息"},
		{name: "narrow dark Chinese", width: 820, height: 1900, theme: ui.DarkTheme(), lang: i18n.Chinese, two: false, want: "构建信息"},
		{name: "wide dark English", width: 1372, height: 1600, theme: ui.DarkTheme(), lang: i18n.English, two: true, want: "Build information"},
		{name: "narrow light English", width: 820, height: 2100, theme: ui.LightTheme(), lang: i18n.English, two: false, want: "Build information"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := aboutTestOpen(t, state, test.theme, test.lang, ui.Rect{
				X: 68, Y: 0, Width: test.width, Height: test.height,
			})
			if !h.hasText(test.want) {
				t.Fatalf("about page did not render in %s", test.lang)
			}
			first, ok := h.findHit(h.panels.ids.aboutCards.At(0))
			if !ok {
				t.Fatal("first build card is missing")
			}
			second, ok := h.findHit(h.panels.ids.aboutCards.At(1))
			if !ok {
				t.Fatal("second build card is missing")
			}
			if test.two {
				if abs(first.Bounds.Y-second.Bounds.Y) > 0.01 || second.Bounds.X <= first.Bounds.X {
					t.Fatalf("wide cards are not two columns: %+v %+v", first.Bounds, second.Bounds)
				}
			} else if abs(first.Bounds.X-second.Bounds.X) > 0.01 || second.Bounds.Y <= first.Bounds.Y {
				t.Fatalf("narrow cards are not one column: %+v %+v", first.Bounds, second.Bounds)
			}
			h.requireLayout()
		})
	}
}

func TestAboutExportsPrivacySafeDiagnostics(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Peers = []domain.Peer{{
		ID: "peer-private", Name: "Private Contact", Address: "192.168.50.20:47842",
	}}
	state.Messages = []domain.Message{{ID: "message-private", Kind: "text", Text: "private chat body"}}
	state.Error = "connection to 192.168.50.20 failed for Private Contact"
	state.Settings.DownloadDir = t.TempDir()
	h := aboutTestOpen(t, state, ui.LightTheme(), i18n.Chinese, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1800,
	})
	h.panels.diagnosticDataDir = func() string { return t.TempDir() }
	destination := t.TempDir() + string(os.PathSeparator) + "goim-diagnostics.txt"
	if err := h.driver.QueueSaveFile(destination, nil); err != nil {
		t.Fatal(err)
	}

	h.click(h.panels.ids.aboutDiagnostics)
	if err := h.driver.Pump(); err != nil {
		t.Fatal(err)
	}
	request, ok := h.driver.LastSaveFileRequest()
	if !ok || request.DefaultExtension != "txt" || !strings.HasPrefix(request.SuggestedName, "goim-diagnostics-") {
		t.Fatalf("save request = %+v, observed=%v", request, ok)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	for _, private := range []string{"Private Contact", "private chat body", "192.168.50.20", state.Settings.DownloadDir} {
		if strings.Contains(report, private) {
			t.Errorf("diagnostic report leaked %q:\n%s", private, report)
		}
	}
	for _, want := range []string{"== GoIM diagnostics ==", "kind: network", "peers: total=1", "Excluded: message text"} {
		if !strings.Contains(report, want) {
			t.Errorf("diagnostic report lacks %q:\n%s", want, report)
		}
	}
}

func TestAboutAutomaticUpdateCheckSwitchDefaultsOn(t *testing.T) {
	state := panelTestDefaultState(t)
	h := aboutTestOpen(t, state, ui.LightTheme(), i18n.Chinese, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1800,
	})
	hit, ok := h.findHit(h.panels.ids.aboutAutoUpdate)
	if !ok || hit.Checked {
		t.Fatalf("automatic update switch = %+v, found=%v", hit, ok)
	}
	h.requireLayout()
	h.click(h.panels.ids.aboutAutoUpdate)
	command := h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "disable_auto_update_check" || !command.Bool {
		t.Fatalf("automatic update command = %+v", command)
	}
}

func abs(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
