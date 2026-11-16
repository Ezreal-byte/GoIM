package desktop

import (
	"github.com/vivaui/viva/gfx"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"goim/internal/flex"

	"goim/internal/buildinfo"
	"goim/internal/domain"
	"goim/internal/i18n"
)

type panelBuildInfo struct {
	version     string
	goVersion   string
	vivaVersion string
	vivaDirty   bool
	platform    string
	source      string
	revision    string
	revisionDay string
	buildTime   string
	ciRef       string
	ciRunID     string
	ciURL       string
}

// The project's stable destinations live together so the cards and their copy
// text cannot drift. QQ and WeChat deliberately have no URL until a real group
// is opened.
var aboutProject = struct {
	repository  string
	issues      string
	communities []aboutCommunity
}{
	repository: buildinfo.RepositoryURL,
	issues:     buildinfo.IssuesURL,
	communities: []aboutCommunity{
		{name: "QQ 群"},
		{name: "微信群"},
	},
}

type aboutCommunity struct {
	name string
	url  string
}

type aboutFact struct {
	label string
	value string
	url   string
}

const aboutTwoColumnMin = float32(panelSettingsWidth + 700)

func panelReadBuildInfo() panelBuildInfo {
	facts := buildinfo.Current()
	return panelBuildInfo{
		version:     facts.Version,
		goVersion:   facts.GoVersion,
		vivaVersion: facts.VivaVersion,
		vivaDirty:   facts.VivaDirty,
		platform:    facts.Platform,
		source:      facts.Source,
		revision:    facts.Revision,
		revisionDay: facts.CommitTime,
		buildTime:   facts.BuildTime,
		ciRef:       facts.CIRef,
		ciRunID:     facts.CIRunID,
		ciURL:       facts.CIURL,
	}
}

func (p *Panels) drawSettingsAbout(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	if p.aboutLicense {
		p.drawLicense(f, theme)
		return
	}
	version := panelDisplayVersion(state.Version, p.build.version)
	f.Row().FillWidth().PadY(12).Gap(16).Items(flex.Center).Children(func() {
		f.Stack().Size(64, 64).Radius(16).Clip().Children(func() {
			f.Image(titleMark).Fill()
		})
		f.Column().Grow(1).Gap(4).Children(func() {
			f.Text("GoIM").FontSize(23).Weight(gfx.WeightSemibold)
			f.Paragraph(i18n.T("局域网内的原生即时通讯与文件传输工具。"), 2).Muted().
				FontSize(theme.Metrics.FontSizeS + 1)
			f.Text(version).Mono().FontSize(theme.Metrics.FontSizeS).Muted()
		})
	})
	f.Row().FillWidth().PadSides(0, 0, 8, 0).Gap(10).Items(flex.Center).Children(func() {
		if f.Button(p.ids.aboutCopyVersion, i18n.T("复制版本信息")).Small().Outline().Clicked() {
			panelCopy(f.Canvas(), p, panelVersionReport(state, p.build))
		}
		if f.Button(p.ids.aboutLicense, i18n.T("开源许可")).Small().Outline().Clicked() {
			p.aboutLicense = true
			p.localError = ""
			p.localNotice = ""
		}
	})

	panelSectionGap(f)
	panelSection(f, i18n.T("构建信息"), i18n.T("点击任一卡片可复制完整的构建信息。"), func() {
		facts := []aboutFact{
			{label: i18n.T("应用版本"), value: version},
			{label: i18n.T("平台与架构"), value: panelValue(p.build.platform)},
			{label: i18n.T("构建来源"), value: panelBuildSource(p.build)},
			{label: i18n.T("源码提交"), value: panelRevision(p.build)},
			{label: "Go", value: panelValue(p.build.goVersion)},
			{label: "Viva", value: panelViva(p.build)},
			{label: i18n.T("构建时间"), value: panelValue(p.build.buildTime)},
			{label: i18n.T("CI 运行"), value: panelCIValue(p.build), url: p.build.ciURL},
		}
		p.aboutFactGrid(f, theme, facts, func() {
			panelCopy(f.Canvas(), p, panelVersionReport(state, p.build))
		})
	})

	panelSectionGap(f)
	panelSection(f, i18n.T("项目与支持"), i18n.T("源码与问题跟踪使用 GitHub；群聊入口确认后再开放。"), func() {
		items := []aboutLinkItem{
			{id: "project-repository", name: i18n.T("GitHub 仓库"), desc: i18n.T("查看源码、版本与发布记录"), url: aboutProject.repository},
			{id: "project-issues", name: i18n.T("问题反馈"), desc: i18n.T("报告问题并跟进修复"), url: aboutProject.issues},
		}
		for _, community := range aboutProject.communities {
			items = append(items, aboutLinkItem{
				id:   "community-" + community.name,
				name: i18n.T(community.name),
				desc: i18n.T("待开放"),
				url:  community.url,
			})
		}
		p.aboutLinkGrid(f, theme, items)
	})

	f.Paragraph(i18n.T("复制的内容只包含版本与构建信息，不包含聊天内容、联系人、文件路径或本机地址。"), 3).
		Muted().FontSize(theme.Metrics.FontSizeS)
}

func (p *Panels) aboutFactGrid(f *flex.Frame, theme ui.Theme, facts []aboutFact, copyAll func()) {
	columns := f.Bounds().Width >= aboutTwoColumnMin
	f.Column().FillWidth().Gap(10).Children(func() {
		for index := 0; index < len(facts); index += 2 {
			left, right := index, index+1
			if columns {
				f.Row().FillWidth().Gap(10).Children(func() {
					f.Column().Grow(1).Basis(0).Children(func() {
						p.aboutFactCard(f, theme, p.ids.aboutCards.At(left), facts[left], copyAll)
					})
					if right < len(facts) {
						f.Column().Grow(1).Basis(0).Children(func() {
							p.aboutFactCard(f, theme, p.ids.aboutCards.At(right), facts[right], copyAll)
						})
					}
				})
			} else {
				p.aboutFactCard(f, theme, p.ids.aboutCards.At(left), facts[left], copyAll)
				if right < len(facts) {
					p.aboutFactCard(f, theme, p.ids.aboutCards.At(right), facts[right], copyAll)
				}
			}
		}
	})
}

func (p *Panels) aboutFactCard(f *flex.Frame, theme ui.Theme, id gfx.ID, fact aboutFact, copyAll func()) {
	role, hint := gfx.RoleButton, i18n.T("复制构建信息")
	if fact.url != "" {
		role, hint = gfx.RoleLink, i18n.T("在浏览器中打开")
	}
	card := f.Column().FillWidth().MinHeight(74).PadSides(12, 12, 12, 12).Gap(4).
		Background(theme.Muted).Border(1, theme.Border).Radius(theme.Metrics.RadiusControl).
		Hover(flex.Look{}.Background(theme.Hover)).
		HitWith(id, fact.label+"："+fact.value, gfx.HitOptions{
			Role: role, Cursor: gfx.PointerCursorHand, Hint: hint,
		})
	card.Children(func() {
		f.Text(fact.label).Muted().FontSize(theme.Metrics.FontSizeS)
		f.Paragraph(fact.value, 2).FontSize(theme.Metrics.FontSizeS + 2).Weight(gfx.WeightMedium)
	})
	if f.Canvas().FocusVisible() == id {
		card.Stroke(1.5, theme.Ring)
	}
	if card.Clicked() {
		if fact.url != "" {
			p.openAboutURL(f.Canvas(), fact.url)
		} else {
			copyAll()
		}
	}
}

type aboutLinkItem struct {
	id   string
	name string
	desc string
	url  string
}

func (p *Panels) aboutLinkGrid(f *flex.Frame, theme ui.Theme, items []aboutLinkItem) {
	columns := f.Bounds().Width >= aboutTwoColumnMin
	f.Column().FillWidth().Gap(10).Children(func() {
		for index := 0; index < len(items); index += 2 {
			left, right := index, index+1
			if columns {
				f.Row().FillWidth().Gap(10).Children(func() {
					f.Column().Grow(1).Basis(0).Children(func() {
						p.aboutLinkCard(f, theme, items[left])
					})
					if right < len(items) {
						f.Column().Grow(1).Basis(0).Children(func() {
							p.aboutLinkCard(f, theme, items[right])
						})
					}
				})
			} else {
				p.aboutLinkCard(f, theme, items[left])
				if right < len(items) {
					p.aboutLinkCard(f, theme, items[right])
				}
			}
		}
	})
}

func (p *Panels) aboutLinkCard(f *flex.Frame, theme ui.Theme, item aboutLinkItem) {
	id := p.ids.aboutCards.ID(p.tree.Name(item.id))
	available := item.url != ""
	card := f.Row().FillWidth().MinHeight(62).PadSides(12, 12, 12, 12).Gap(10).Items(flex.Center).
		Background(theme.Muted).Border(1, theme.Border).Radius(theme.Metrics.RadiusControl)
	if available {
		card = card.Hover(flex.Look{}.Background(theme.Hover)).
			HitWith(id, item.name+"："+item.desc, gfx.HitOptions{
				Role: gfx.RoleLink, Cursor: gfx.PointerCursorHand, Hint: i18n.T("在浏览器中打开"),
			})
	} else {
		card = card.HitWith(id, item.name+"："+item.desc, gfx.HitOptions{
			Role: gfx.RoleButton, Disabled: true, Hint: i18n.T("待开放"),
		})
	}
	card.Children(func() {
		f.Column().Grow(1).Gap(2).Children(func() {
			f.Text(item.name).FontSize(theme.Metrics.FontSizeS + 2).Weight(gfx.WeightMedium)
			f.Text(item.desc).Muted().FontSize(theme.Metrics.FontSizeS).Ellipsis()
		})
		if available {
			f.Icon(ExternalLink).Size(15, 15).Color(theme.MutedForeground)
		}
	})
	if f.Canvas().FocusVisible() == id {
		card.Stroke(1.5, theme.Ring)
	}
	if available && card.Clicked() {
		p.openAboutURL(f.Canvas(), item.url)
	}
}

func (p *Panels) openAboutURL(canvas *gfx.Canvas, target string) {
	canvas.AfterPaint(func() {
		if err := app.OpenURL(target); err != nil {
			p.localError = i18n.T("无法打开链接：") + err.Error()
			return
		}
		p.localError = ""
		p.localNotice = ""
	})
}

func panelVersionReport(state domain.State, build panelBuildInfo) string {
	version := panelDisplayVersion(state.Version, build.version)
	return strings.Join([]string{
		"GoIM " + version,
		i18n.T("平台") + ": " + panelValue(build.platform),
		i18n.T("构建来源") + ": " + panelBuildSource(build),
		i18n.T("源码提交") + ": " + panelValue(build.revision),
		i18n.T("提交日期") + ": " + panelValue(build.revisionDay),
		"Go " + panelValue(build.goVersion),
		"Viva " + panelViva(build),
		i18n.T("构建时间") + ": " + panelValue(build.buildTime),
		i18n.T("CI 链接") + ": " + panelValue(build.ciURL),
	}, "\n")
}

func panelDisplayVersion(fromState, fromBuild string) string {
	version := strings.TrimSpace(fromState)
	if index := strings.LastIndex(version, " ("); index > 0 && strings.HasSuffix(version, ")") {
		version = version[:index]
	}
	if version == "" {
		version = strings.TrimSpace(fromBuild)
	}
	return panelValue(version)
}

func panelBuildSource(build panelBuildInfo) string {
	switch build.source {
	case buildinfo.SourceGitHubActions:
		parts := []string{"GitHub Actions"}
		if build.ciRef != "" {
			parts = append(parts, build.ciRef)
		}
		if build.ciRunID != "" {
			parts = append(parts, "run "+build.ciRunID)
		}
		return strings.Join(parts, " · ")
	case buildinfo.SourceLocal:
		return i18n.T("本地构建")
	case "":
		return i18n.T("未提供")
	default:
		return build.source
	}
}

func panelRevision(build panelBuildInfo) string {
	revision := panelShortRevision(build.revision)
	if revision == "" {
		return i18n.T("未提供")
	}
	if build.revisionDay != "" {
		revision += " · " + build.revisionDay
	}
	return revision
}

func panelViva(build panelBuildInfo) string {
	viva := panelShortFramework(build.vivaVersion)
	if viva == "" {
		return i18n.T("未提供")
	}
	if build.vivaDirty {
		viva += i18n.T(" · 有本地修改")
	}
	return viva
}

func panelCIValue(build panelBuildInfo) string {
	if build.ciURL == "" {
		return i18n.T("未提供")
	}
	return i18n.T("打开 CI 运行")
}

func panelShortRevision(value string) string {
	value = strings.TrimSpace(value)
	dirty := strings.HasSuffix(value, "-dirty")
	value = strings.TrimSuffix(value, "-dirty")
	if len(value) > 12 {
		value = value[:12]
	}
	if dirty && value != "" {
		value += "-dirty"
	}
	return value
}

func panelShortFramework(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if value == "local" {
		return i18n.T("本地替换")
	}
	if plus := strings.IndexByte(value, '+'); plus > 12 {
		return value[:12] + value[plus:]
	}
	if len(value) > 12 && !strings.ContainsAny(value, " .") {
		return value[:12]
	}
	return value
}

func panelValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return i18n.T("未提供")
	}
	return value
}

func (p *Panels) drawLicense(f *flex.Frame, theme ui.Theme) {
	f.Row().FillWidth().Gap(8).PadY(8).Items(flex.Center).Children(func() {
		if f.Button(p.ids.aboutBack, i18n.T("返回关于")).Small().Ghost().Clicked() {
			p.aboutLicense = false
		}
		f.Spacer()
		if f.Button(p.ids.aboutCopyLicense, i18n.T("复制许可摘要")).Small().Outline().Clicked() {
			panelCopy(f.Canvas(), p, panelLicenseText)
		}
	})
	f.Divider().FillWidth()
	for _, paragraph := range strings.Split(panelLicenseText, "\n\n") {
		f.Paragraph(paragraph, 40).FontSize(theme.Metrics.FontSizeS + 1).PadY(10)
	}
}
