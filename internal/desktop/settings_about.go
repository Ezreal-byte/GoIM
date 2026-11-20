package desktop

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/buildinfo"
	"goim/internal/diagnostics"
	"goim/internal/domain"
	"goim/internal/i18n"
)

type panelBuildInfo struct {
	version     string
	goVersion   string
	vivaVersion string
	platform    string
	source      string
	revision    string
	revisionDay string
	buildTime   string
}

const (
	aboutSite   = "https://ezreal-byte.github.io/GoIM/"
	aboutQQ     = "1084364974"
	aboutQQURL  = "https://qm.qq.com/cgi-bin/qm/qr?k=&group_code=" + aboutQQ
	aboutWeChat = "https://www.kdocs.cn/l/cdcdb0XI3TTt"
)

type aboutFact struct {
	label string
	value string
}

const aboutTwoColumnMin = float32(panelSettingsWidth + 700)

func panelReadBuildInfo() panelBuildInfo {
	facts := buildinfo.Current()
	return panelBuildInfo{
		version:     facts.Version,
		goVersion:   facts.GoVersion,
		vivaVersion: facts.VivaVersion,
		platform:    facts.Platform,
		source:      facts.Source,
		revision:    facts.Revision,
		revisionDay: facts.CommitTime,
		buildTime:   facts.BuildTime,
	}
}

func (p *Panels) drawSettingsAbout(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	if p.aboutLicense {
		p.drawLicense(f, theme)
		return
	}
	version := panelDisplayVersion(state.Version, p.build.version)
	ui.Row(f).FillWidth().PadY(12).Gap(16).Items(ui.Center).Children(func() {
		ui.Stack(f).Size(64, 64).Radius(16).Clip().Children(func() {
			ui.Image(f, titleMark).Fill()
		})
		ui.Column(f).Grow(1).Gap(4).Children(func() {
			ui.Text(f, "GoIM").FontSize(23).Weight(gfx.WeightSemibold)
			ui.Paragraph(f, i18n.T("局域网内的原生即时通讯与文件传输工具。"), 2).Muted().
				FontSize(theme.Metrics.FontSizeS + 1)
			ui.Text(f, version).Mono().FontSize(fontToken(theme, theme.Metrics.FontSizeS)).Muted()
		})
	})
	ui.Row(f).FillWidth().PadSides(0, 0, 8, 0).Gap(10).Items(ui.Center).Children(func() {
		if namedButton(f, p.ids.aboutCopyVersion, i18n.T("复制版本信息")).Small().Outline().Clicked() {
			panelCopy(f.Canvas(), p, panelVersionReport(state, p.build))
		}
		if namedButton(f, p.ids.aboutLicense, i18n.T("开源许可")).Small().Outline().Clicked() {
			p.aboutLicense = true
			p.localError = ""
		}
	})

	panelSectionGap(f)
	p.drawUpdateSettings(f, theme, state, submit)
	panelSectionGap(f)
	p.settingsRow(f, theme, i18n.T("诊断与反馈"),
		i18n.T("遇到问题时导出脱敏报告，仅保留版本、协议状态、适配器和数据计数，不含聊天内容、联系人、完整地址、文件路径或凭据。"),
		func() {
			ui.Column(f).Shrink(0).Gap(6).Items(ui.End).Children(func() {
				if namedButton(f, p.ids.aboutDiagnostics, i18n.T("导出脱敏报告")).Small().Outline().Clicked() {
					p.exportDiagnostics(f.Canvas(), state)
				}
				if namedButton(f, p.ids.aboutCopyDiagnostics, i18n.T("复制脱敏报告")).Small().Outline().Clicked() {
					p.copyDiagnostics(f.Canvas(), state)
				}
			})
		})
	panelSectionGap(f)
	p.settingsRow(f, theme, i18n.T("清除所有用户数据"),
		i18n.T("清除本机身份、联系人、聊天记录、密码、设置及应用缓存，不删除已下载或分享的原始文件。清除后需重新启动 GoIM。"), func() {
			if namedButton(f, p.ids.aboutResetData, i18n.T("清除用户数据…")).Small().Outline().Disabled(state.DataResetPending).Clicked() {
				p.resetDataOpen = true
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
			{label: i18n.T("构建时间"), value: panelBuildTime(p.build.buildTime)},
		}
		p.aboutFactGrid(f, theme, facts, func() {
			panelCopy(f.Canvas(), p, panelVersionReport(state, p.build))
		})
	})

	panelSectionGap(f)
	panelSection(f, i18n.T("加入交流"), "", func() {
		p.aboutLinkGrid(f, theme, []aboutLinkItem{
			{id: "community-qq", name: i18n.T("QQ 群"), desc: aboutQQ, url: aboutQQURL, mark: aboutQQLinkMark},
			{id: "community-wechat", name: i18n.T("微信群"), desc: i18n.T("查看入群方式"), url: aboutWeChat, mark: aboutWeChatLinkMark},
		})
	})
	panelSectionGap(f)
	panelSection(f, i18n.T("项目"), "", func() {
		p.aboutLinkGrid(f, theme, []aboutLinkItem{
			{id: "project-site", name: i18n.T("项目官网"), desc: i18n.T("了解和下载 GoIM"), url: aboutSite},
			{id: "project-repository", name: i18n.T("GitHub 仓库"), desc: i18n.T("查看源码、版本与发布记录"), url: buildinfo.RepositoryURL, mark: aboutGitHubLinkMark},
			{
				id: "project-issues", name: i18n.T("问题反馈"), desc: i18n.T("报告问题并跟进修复"), url: buildinfo.IssuesURL,
				mark: &aboutLinkMark{
					icon: CircleAlert, chip: theme.Background.Mix(theme.Primary, 0.15), ink: theme.Primary, size: 19,
				},
			},
		})
	})

	ui.Paragraph(f, i18n.T("版本信息与诊断报告均不包含聊天内容、联系人、文件路径或本机地址。"), 3).
		Muted().FontSize(fontToken(theme, theme.Metrics.FontSizeS))
}

func (p *Panels) drawResetDataDialog(f ui.C, state domain.State, submit func(domain.Command)) {
	if !p.resetDataOpen {
		return
	}
	dialog := ui.Dialog(f, ui.DialogSpec{
		Title:       i18n.T("确定清除所有用户数据？"),
		Description: i18n.T("此操作不可撤销。GoIM 将退出并清除本机账号、联系人、聊天记录、密码和设置；已下载或分享的原始文件会保留。请重新启动软件，系统将创建新的身份。"),
		Width:       520, Layer: 75, Scope: p.ids.aboutResetScope,
		DismissID: p.ids.aboutResetDismiss, CloseID: p.ids.aboutResetClose, InitialFocusID: p.ids.aboutResetCancel,
	})
	dialog.Footer(func() {
		if dialogActionButton(f, p.ids.aboutResetCancel, i18n.T("取消")).Outline().Clicked() {
			p.resetDataOpen = false
		}
		if dialogActionButton(f, p.ids.aboutResetConfirm, i18n.T("清除并退出")).Destructive().Disabled(state.DataResetPending).Clicked() {
			p.resetDataOpen = false
			submit(domain.Command{Kind: domain.ResetUserData})
		}
	})
	if dialog.Dismissed() {
		p.resetDataOpen = false
	}
}

func (p *Panels) exportDiagnostics(canvas *gfx.Canvas, state domain.State) {
	if p.diagnosticPickerOpen {
		return
	}
	p.diagnosticPickerOpen = true
	canvas.AfterPaint(func() {
		defer func() { p.diagnosticPickerOpen = false; p.invalidateDiagnostics() }()
		now := time.Now()
		name := "goim-diagnostics-" + now.In(aboutTZ).Format("20060102-1504")
		path, err := app.SaveFile(app.SaveFileOptions{
			Title:            i18n.T("导出脱敏诊断报告"),
			SuggestedName:    name + ".txt",
			DefaultExtension: "txt",
			Filters:          []app.FileFilter{{Name: i18n.T("文本文件"), Pattern: "*.txt"}},
		})
		if errors.Is(err, app.ErrCanceled) || errors.Is(err, app.ErrNoDriverResponse) {
			return
		}
		fallback := errors.Is(err, app.ErrUnsupported)
		if err != nil && !fallback {
			p.localError = i18n.T("无法创建诊断报告：") + filePickerProblem(err)
			p.invalidateDiagnostics()
			return
		}
		report := p.diagnosticReport(state, now)
		if fallback {
			directory := state.Settings.DownloadDir
			if directory == "" && p.diagnosticDataDir != nil {
				if dataDir := p.diagnosticDataDir(); dataDir != "" {
					directory = filepath.Join(dataDir, "diagnostics")
				}
			}
			path, err = diagnostics.WritePrivateTemp(directory, name, report)
		} else {
			err = diagnostics.WritePrivateFile(path, report)
		}
		if err != nil {
			p.localError = i18n.T("无法保存诊断报告：") + err.Error()
			p.invalidateDiagnostics()
			return
		}
		p.localError = ""
		if p.toast != nil {
			detail := i18n.T("可发送给开发者协助排查问题。")
			if fallback {
				detail = i18n.T("系统选择器不可用，报告已自动保存到：") + path
			}
			p.toast(i18n.T("诊断报告已保存"), detail, components.ToastSuccess)
		} else {
			p.invalidateDiagnostics()
		}
	})
}

func (p *Panels) diagnosticReport(state domain.State, now time.Time) string {
	dataDir := ""
	if p.diagnosticDataDir != nil {
		dataDir = p.diagnosticDataDir()
	}
	return diagnostics.Build(diagnostics.Input{
		Now: now, Build: diagnosticsBuildInfo(p.build), State: state,
		Autostart: p.autostartDiagnosticState(), InterfaceFontSupported: p.settingsExtra.interfaceFont.supported,
		DataDir: dataDir,
	})
}

func (p *Panels) copyDiagnostics(canvas *gfx.Canvas, state domain.State) {
	canvas.AfterPaint(func() {
		if err := app.SetClipboardText(p.diagnosticReport(state, time.Now())); err != nil {
			p.localError = i18n.T("复制失败：") + err.Error()
			p.invalidateDiagnostics()
			return
		}
		p.localError = ""
		if p.toast != nil {
			p.toast(i18n.T("脱敏报告已复制"), i18n.T("可粘贴到反馈中协助排查问题。"), components.ToastSuccess)
		}
		p.invalidateDiagnostics()
	})
}

func (p *Panels) invalidateDiagnostics() {
	if p.invalidate != nil {
		p.invalidate()
	}
}

func diagnosticsBuildInfo(build panelBuildInfo) buildinfo.Info {
	return buildinfo.Info{
		Version:     build.version,
		Platform:    build.platform,
		Source:      build.source,
		Revision:    build.revision,
		CommitTime:  build.revisionDay,
		GoVersion:   build.goVersion,
		VivaVersion: build.vivaVersion,
		BuildTime:   build.buildTime,
	}
}

func (p *Panels) aboutFactGrid(f ui.C, theme ui.Theme, facts []aboutFact, copyAll func()) {
	columns := f.Bounds().Width >= aboutTwoColumnMin
	ui.Column(f).FillWidth().Gap(10).Children(func() {
		for index := 0; index < len(facts); index += 2 {
			left, right := index, index+1
			if columns {
				ui.Row(f).FillWidth().Gap(10).Children(func() {
					ui.Column(f).Grow(1).Basis(0).Children(func() {
						p.aboutFactCard(f, theme, p.ids.aboutCards.At(left), facts[left], copyAll)
					})
					if right < len(facts) {
						ui.Column(f).Grow(1).Basis(0).Children(func() {
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

func (p *Panels) aboutFactCard(f ui.C, theme ui.Theme, id gfx.ID, fact aboutFact, copyAll func()) {
	card := ui.Column(f).FillWidth().MinHeight(74).PadSides(12, 12, 12, 12).Gap(4).
		Background(theme.Muted).Border(1, theme.Border).Radius(theme.Metrics.RadiusControl).
		Hover(ui.Look{}.Background(theme.Hover)).
		HitWith(id, fact.label+"："+fact.value, gfx.HitOptions{
			Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Hint: i18n.T("复制构建信息"),
		})
	card.Children(func() {
		ui.Text(f, fact.label).Muted().FontSize(fontToken(theme, theme.Metrics.FontSizeS))
		ui.Paragraph(f, fact.value, 2).FontSize(theme.Metrics.FontSizeS + 2).Weight(gfx.WeightMedium)
	})
	if f.Canvas().FocusVisible() == id {
		card.Stroke(1.5, theme.Ring)
	}
	if card.Clicked() {
		copyAll()
	}
}

// aboutLinkMark mirrors Motty's brand marks: each logo keeps the chip color,
// ink and visual size chosen for it, so the artwork is not flattened by one
// shared page tint.
type aboutLinkMark struct {
	icon gfx.Icon
	chip gfx.Color
	ink  gfx.Color
	size float32
}

var (
	aboutBrandMarkTile = gfx.RGB(0xFF, 0xFF, 0xFF)
	aboutBrandMarkInk  = gfx.RGB(0xFF, 0xFF, 0xFF)

	aboutQQLinkMark = &aboutLinkMark{
		icon: AboutQq, chip: aboutBrandMarkTile, ink: aboutBrandMarkInk, size: 21,
	}
	aboutWeChatLinkMark = &aboutLinkMark{
		icon: AboutWechat, chip: gfx.RGB(0x07, 0xC1, 0x60), ink: aboutBrandMarkInk, size: 16,
	}
	aboutGitHubLinkMark = &aboutLinkMark{
		icon: AboutGithub, chip: aboutBrandMarkTile, ink: gfx.RGB(0x18, 0x17, 0x17), size: 17,
	}
)

type aboutLinkItem struct {
	id   string
	name string
	desc string
	url  string
	copy string
	mark *aboutLinkMark
}

func (p *Panels) aboutLinkGrid(f ui.C, theme ui.Theme, items []aboutLinkItem) {
	columns := f.Bounds().Width >= aboutTwoColumnMin
	ui.Column(f).FillWidth().Gap(10).Children(func() {
		for index := 0; index < len(items); index += 2 {
			left, right := index, index+1
			if columns {
				ui.Row(f).FillWidth().Gap(10).Children(func() {
					ui.Column(f).Grow(1).Basis(0).Children(func() {
						p.aboutLinkCard(f, theme, items[left])
					})
					if right < len(items) {
						ui.Column(f).Grow(1).Basis(0).Children(func() {
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

func (p *Panels) aboutLinkCard(f ui.C, theme ui.Theme, item aboutLinkItem) {
	id := p.ids.aboutCards.ID(p.tree.Name(item.id))
	role, hint, action := gfx.RoleLink, i18n.T("在浏览器中打开"), &ExternalLink
	if item.copy != "" {
		role, hint, action = gfx.RoleButton, i18n.T("复制群号"), &Copy
	}
	card := ui.Row(f).FillWidth().MinHeight(62).PadSides(12, 12, 12, 12).Gap(10).Items(ui.Center).
		Background(theme.Muted).Border(1, theme.Border).Radius(theme.Metrics.RadiusControl).
		Hover(ui.Look{}.Background(theme.Hover)).
		HitWith(id, item.name+"："+item.desc, gfx.HitOptions{
			Role: role, Cursor: gfx.PointerCursorHand, Hint: hint,
		})
	card.Children(func() {
		if item.mark == nil {
			ui.Stack(f).Size(34, 34).Radius(radiusControl).Clip().Children(func() {
				ui.Image(f, titleMark).Fill()
			})
		} else {
			ui.Row(f).Size(34, 34).Radius(radiusControl).Items(ui.Center).Justify(ui.Center).
				Background(item.mark.chip).Children(func() {
				ui.Icon(f, item.mark.icon).Size(item.mark.size, item.mark.size).Color(item.mark.ink)
			})
		}
		ui.Column(f).Grow(1).Gap(2).Children(func() {
			ui.Text(f, item.name).FontSize(theme.Metrics.FontSizeS + 2).Weight(gfx.WeightMedium)
			ui.Text(f, item.desc).Muted().FontSize(fontToken(theme, theme.Metrics.FontSizeS)).Ellipsis()
		})
		ui.Icon(f, *action).Size(15, 15).Color(theme.MutedForeground)
	})
	if f.Canvas().FocusVisible() == id {
		card.Stroke(1.5, theme.Ring)
	}
	if card.Clicked() {
		if item.copy != "" {
			panelCopy(f.Canvas(), p, item.copy)
		} else {
			p.openAboutURL(f.Canvas(), item.url)
		}
	}
}

func (p *Panels) openAboutURL(canvas *gfx.Canvas, target string) {
	canvas.AfterPaint(func() {
		p.openAboutURLNow(target)
	})
}

func (p *Panels) openAboutURLNow(target string) {
	if err := app.OpenURL(target); err != nil {
		p.localError = i18n.T("无法打开链接：") + err.Error()
		return
	}
	p.localError = ""
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
		i18n.T("构建时间") + ": " + panelBuildTime(build.buildTime),
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
		return "GitHub Actions"
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
	if build.vivaVersion == "local" {
		return i18n.T("本地替换")
	}
	return panelValue(build.vivaVersion)
}

var aboutTZ = time.FixedZone("GMT+8", 8*60*60)

func panelBuildTime(utc string) string {
	t, err := time.Parse("2006-01-02 15:04 UTC", utc)
	if err != nil {
		return i18n.T("未提供")
	}
	return t.In(aboutTZ).Format("2006-01-02 15:04 MST")
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

func panelValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return i18n.T("未提供")
	}
	return value
}

func (p *Panels) drawLicense(f ui.C, theme ui.Theme) {
	ui.Row(f).FillWidth().Gap(8).PadY(8).Items(ui.Center).Children(func() {
		if namedButton(f, p.ids.aboutBack, i18n.T("返回关于")).Small().Ghost().Clicked() {
			p.aboutLicense = false
		}
		ui.Spacer(f)
		if namedButton(f, p.ids.aboutCopyLicense, i18n.T("复制许可摘要")).Small().Outline().Clicked() {
			panelCopy(f.Canvas(), p, panelLicenseText)
		}
	})
	ui.Divider(f).FillWidth()
	for _, paragraph := range strings.Split(panelLicenseText, "\n\n") {
		ui.Paragraph(f, paragraph, 40).FontSize(fontToken(theme, theme.Metrics.FontSizeS+1)).PadY(10)
	}
}
