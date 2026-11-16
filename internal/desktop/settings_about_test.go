package desktop

import (
	"github.com/vivaui/viva/gfx"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func aboutTestBuildInfo() panelBuildInfo {
	return panelBuildInfo{
		version:     "1.2.3",
		goVersion:   "go1.27.3",
		vivaVersion: "b357903a11e0e5c58c056805617001e73b44b2c9+patch",
		vivaDirty:   true,
		platform:    "Windows · x86-64",
		source:      "github-actions",
		revision:    "fedcba9876543210fedcba9876543210fedcba98",
		revisionDay: "2026-11-07",
		buildTime:   "2026-11-10 04:34 UTC",
		ciRef:       "refs/tags/v1.2.3",
		ciRunID:     "987654321",
		ciURL:       "https://github.com/Ezreal-byte/GoIM/actions/runs/987654321",
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
		"构建来源: GitHub Actions · refs/tags/v1.2.3 · run 987654321",
		"源码提交: fedcba9876543210fedcba9876543210fedcba98",
		"提交日期: 2026-11-07",
		"Go go1.27.3",
		"Viva b357903a11e0+patch · 有本地修改",
		"构建时间: 2026-11-10 04:34 UTC",
		"CI 链接: https://github.com/Ezreal-byte/GoIM/actions/runs/987654321",
	} {
		if !strings.Contains(clipboard, want) {
			t.Errorf("copied build facts do not contain %q:\n%s", want, clipboard)
		}
	}
	if strings.Contains(clipboard, state.Settings.DownloadDir) {
		t.Fatalf("copied build facts contain a user path: %s", clipboard)
	}
	if strings.Contains(clipboard, "聊天内容") || strings.Contains(clipboard, "联系人") {
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
	build.ciURL = ""
	build.ciRunID = ""
	build.vivaVersion = ""
	report := panelVersionReport(state, build)

	if !strings.Contains(report, "提交日期: 2026-11-07") {
		t.Fatalf("commit date is missing: %s", report)
	}
	if !strings.Contains(report, "构建时间: 未提供") {
		t.Fatalf("missing build time was not explicit: %s", report)
	}
	if !strings.Contains(report, "Viva 未提供") || !strings.Contains(report, "CI 链接: 未提供") {
		t.Fatalf("unknown facts were fabricated: %s", report)
	}
}

func TestAboutExternalLinksAndCommunityAvailability(t *testing.T) {
	state := panelTestDefaultState(t)
	h := aboutTestOpen(t, state, ui.LightTheme(), i18n.Chinese, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1500,
	})

	h.click(aboutTestID(h, "project-repository"))
	if target, ok := h.driver.LastOpenedURL(); !ok || target != aboutProject.repository {
		t.Fatalf("repository link = %q, %v", target, ok)
	}
	h.click(aboutTestID(h, "project-issues"))
	if target, ok := h.driver.LastOpenedURL(); !ok || target != aboutProject.issues {
		t.Fatalf("issues link = %q, %v", target, ok)
	}
	h.click(h.panels.ids.aboutCards.At(7))
	if target, ok := h.driver.LastOpenedURL(); !ok || target != aboutTestBuildInfo().ciURL {
		t.Fatalf("CI link = %q, %v", target, ok)
	}

	for _, community := range aboutProject.communities {
		if community.url != "" {
			t.Fatalf("%s has an unverified URL %q", community.name, community.url)
		}
		hit, ok := h.findHit(aboutTestID(h, "community-"+community.name))
		if !ok {
			t.Fatalf("%s card is missing", community.name)
		}
		if !hit.Disabled {
			t.Fatalf("%s card is not disabled", community.name)
		}
	}
	if !h.hasText("待开放") || !h.hasText("QQ 群") || !h.hasText("微信群") {
		t.Fatal("unavailable community cards are not clear")
	}
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

func abs(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
