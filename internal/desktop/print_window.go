package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

const (
	printWindowWidth        = 680
	printWindowHeight       = 760
	printWindowMinWidth     = 580
	printWindowMinHeight    = 660
	printWindowTitlebar     = 44
	printWindowContentInset = 26
)

var printMinusIcon = gfx.Icon{
	ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{gfx.MoveTo(5, 12), gfx.LineTo(19, 12)},
}

type printWindowIDs struct {
	chooseFile  gfx.ID
	replace     gfx.ID
	copiesDown  gfx.ID
	copiesUp    gfx.ID
	copiesField gfx.ID
	rangeField  gfx.ID
	cancel      gfx.ID
	send        gfx.ID
	password    gfx.ID
}

type printWindowView struct {
	backend domain.Backend
	shareID string
	state   domain.State

	tree      gfx.Tree
	titlePage *ui.Page
	page      *ui.Page
	theme     ui.Theme
	palette   palette
	wake      app.Invalidator
	ids       printWindowIDs

	avatar   func(f ui.C, peerID, name, path string, size float32, online bool)
	pickFile func(app.OpenFileOptions) ([]string, error)
	submit   func(domain.Command)
	close    func()

	path         string
	fileName     string
	fileSize     int64
	fileErr      string
	paper        string
	copies       ui.Field
	orientation  string
	pageMode     string
	pageRange    ui.Field
	color        string
	duplex       string
	err          string
	password     ui.Field
	authRequest  uint64
	pendingPrint bool
}

func newPrintWindowView(backend domain.Backend, shareID string) *printWindowView {
	v := &printWindowView{
		backend: backend, shareID: shareID,
		pickFile: app.OpenFiles,
		pageMode: "all",
	}
	v.theme = ui.LightTheme()
	v.palette = paletteFor(v.theme, v.state.Settings.ColorScheme)
	v.copies.SetText("1")
	v.password.Secure = true
	v.initIDs()
	v.titlePage = ui.NewPage(ui.PageConfig{Root: "print-window-title", Tree: &v.tree, Theme: &v.theme})
	v.page = ui.NewPage(ui.PageConfig{Root: "print-window", Tree: &v.tree, Theme: &v.theme})
	v.refresh()
	return v
}

func (v *printWindowView) initIDs() {
	page := v.tree.Feature("print-window")
	v.ids.chooseFile = v.tree.ID(page, "choose-file")
	v.ids.replace = v.tree.ID(page, "replace-file")
	v.ids.copiesDown = v.tree.ID(page, "copies-down")
	v.ids.copiesUp = v.tree.ID(page, "copies-up")
	v.ids.copiesField = v.tree.ID(page, "copies")
	v.ids.rangeField = v.tree.ID(page, "page-range")
	v.ids.cancel = v.tree.ID(page, "cancel")
	v.ids.send = v.tree.ID(page, "send")
	v.ids.password = v.tree.ID(page, "password")
}

func (v *printWindowView) refresh() {
	if v.backend == nil {
		return
	}
	v.state = v.backend.Snapshot()
	i18n.SetLanguage(v.state.Settings.Language)
	v.theme = themeForSettings(v.state.Settings)
	v.palette = paletteFor(v.theme, v.state.Settings.ColorScheme)
	v.normalizeOptions()
	if v.pendingPrint {
		result := v.state.PrinterAuth[v.shareID]
		if result.Request == v.authRequest && !result.Pending {
			v.pendingPrint = false
			v.password.SetText("")
			if result.Error != "" {
				v.err = i18n.T(result.Error)
			} else if !v.needsPassword(v.printer()) {
				v.sendJob(v.printer())
			}
		}
	}
}

func (v *printWindowView) needsPassword(share domain.PrinterShare) bool {
	return share.PasswordRequired && (share.AuthRevision == "" || v.state.PrinterAuthorized[share.ID] != share.AuthRevision)
}

func (v *printWindowView) printer() domain.PrinterShare {
	return v.state.Printer(v.shareID)
}

func (v *printWindowView) normalizeOptions() {
	share := v.printer()
	if share.ID == "" {
		return
	}
	capabilities := printWindowCapabilities(share.Capabilities)
	if !containsString(capabilities.Papers, v.paper) {
		v.paper = capabilities.Papers[0]
	}
	if !containsString(capabilities.Orientations, v.orientation) {
		v.orientation = capabilities.Orientations[0]
	}
	if !containsString(capabilities.ColorModes, v.color) {
		v.color = capabilities.ColorModes[0]
	}
	if !containsString(capabilities.DuplexModes, v.duplex) {
		v.duplex = capabilities.DuplexModes[0]
	}
}

func printWindowCapabilities(capabilities domain.PrintCapabilities) domain.PrintCapabilities {
	out := domain.PrintCapabilities{
		Papers:            uniquePrintChoices(capabilities.Papers, []string{"A4", "A3", "Letter"}),
		Orientations:      uniquePrintChoices(capabilities.Orientations, []string{domain.PrintOrientationPortrait, domain.PrintOrientationLandscape}),
		ColorModes:        uniquePrintChoices(capabilities.ColorModes, []string{domain.PrintColorMonochrome, domain.PrintColorColor}),
		DuplexModes:       uniquePrintChoices(capabilities.DuplexModes, []string{domain.PrintDuplexSimplex, domain.PrintDuplexLongEdge, domain.PrintDuplexShortEdge}),
		SupportsPageRange: capabilities.SupportsPageRange,
	}
	return out
}

func uniquePrintChoices(values, fallback []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !containsString(out, value) {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), fallback...)
	}
	return out
}

func (v *printWindowView) Config() app.Config {
	clear := colorToApp(v.palette.sidebar)
	return app.Config{
		Title: i18n.T("打印文档"), Width: printWindowWidth, Height: printWindowHeight,
		MinWidth: printWindowMinWidth, MinHeight: printWindowMinHeight,
		WindowMode: app.WindowCustomTitlebar, TitlebarHeight: printWindowTitlebar,
		ClearColor: clear, Tree: &v.tree, Paint: v.paint, TextInput: v.textInput,
		Invalidator: &v.wake, OnWake: v.refresh, OnPointer: v.pointer, OnScroll: v.scroll,
		OnKey: v.key, OnShortcut: v.key, WindowControls: func() app.WindowControlsStyle {
			return windowControlsFor(v.palette)
		},
	}
}

func (v *printWindowView) Close() {
	v.pendingPrint = false
	v.password.SetText("")
	if v.titlePage != nil {
		v.titlePage.Close()
	}
	if v.page != nil {
		v.page.Close()
	}
}

func (v *printWindowView) textInput(context app.TextInputContext) *app.TextInput {
	return v.page.TextInput(context)
}

func (v *printWindowView) pointer(event gfx.PointerEvent) gfx.EventResult {
	return v.page.Pointer(event)
}

func (v *printWindowView) scroll(event gfx.ScrollEvent) gfx.EventResult {
	return v.page.Wheel(event)
}

func (v *printWindowView) key(event gfx.KeyEvent) gfx.EventResult {
	if !event.Release && event.Key == gfx.KeyEscape {
		if v.close != nil {
			v.close()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	return v.page.Key(event)
}

func (v *printWindowView) paint(canvas *gfx.Canvas, size ui.Size) {
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, v.palette.chat)
	titlebar := ui.Rect{Width: bounds.Width, Height: min(printWindowTitlebar, bounds.Height)}
	body := ui.Rect{Y: titlebar.Bottom(), Width: bounds.Width, Height: max(bounds.Height-titlebar.Height, 0)}
	v.drawTitlebar(canvas, titlebar)
	v.drawBody(canvas, body)
}

func (v *printWindowView) drawTitlebar(canvas *gfx.Canvas, bounds ui.Rect) {
	if bounds.Height <= 0 {
		return
	}
	canvas.Fill(bounds, v.palette.rail)
	leading, trailing := app.TitlebarInsets()
	v.titlePage.Draw(canvas, bounds, func(f ui.C) {
		ui.Row(f).Fill().MovesWindow().
			PadSides(12, trailing+12, 6, leading+10).Items(ui.Center).Gap(9).Children(func() {
			ui.Icon(f, panelPrintIcon).Size(20, 20).Color(v.palette.icon)
			ui.Text(f, i18n.T("打印文档")).FontSize(textSize(f, 15)).Weight(gfx.WeightSemibold)
		})
	})
	canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Bottom() - 1, Width: bounds.Width, Height: 1}, v.palette.border)
}

func (v *printWindowView) drawBody(canvas *gfx.Canvas, bounds ui.Rect) {
	share := v.printer()
	pad := float32(printWindowContentInset)
	if bounds.Width < 640 {
		pad = 18
	}
	v.page.Draw(canvas, bounds, func(f ui.C) {
		ui.Column(f).Fill().PadSides(22, pad, 0, pad).Children(func() {
			ui.Scroll(f.KeyInt(0)).FillWidth().Grow(1).Children(func() {
				ui.Column(f).FillWidth().PadY(20).Gap(20).Children(func() {
					v.drawPrinterHeader(f, share)
					v.drawAuthorization(f, share)
					v.drawFileSection(f)
					v.drawOptions(f, share)
					if v.err != "" {
						ui.Paragraph(f, v.err, 4).FillWidth().FontSize(textSize(f, 12)).Color(v.palette.danger)
					}
				})
			})
			ui.Divider(f).FillWidth()
			v.drawFooter(f, share)
		})
	})
}

func (v *printWindowView) drawAuthorization(f ui.C, share domain.PrinterShare) {
	if !share.PasswordRequired {
		return
	}
	ui.Column(f).FillWidth().Pad(14).Gap(8).Radius(radiusCard).Background(v.theme.Muted).Children(func() {
		ui.Row(f).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			ui.Icon(f, Lock).Size(16, 16).Color(v.palette.icon)
			label := i18n.T("已验证打印密码")
			if v.needsPassword(share) {
				label = i18n.T("此打印机需要验证密码")
			}
			ui.Text(f, label).Grow(1).FontSize(textSize(f, 13)).Weight(gfx.WeightMedium)
		})
		if v.needsPassword(share) {
			ui.TextFieldID(f, v.ids.password, &v.password, i18n.T("打印密码"), i18n.T("输入共享人设置的密码")).FillWidth().MinHeight(printControlHeight(v.theme)).Disabled(v.pendingPrint)
		}
		ui.Paragraph(f, i18n.T("仅首次使用或共享人修改密码后需要验证，不保存明文密码。"), 3).FillWidth().Muted().FontSize(textSize(f, 11.5))
	})
}

func (v *printWindowView) drawPrinterHeader(f ui.C, share domain.PrinterShare) {
	owner := v.state.Peer(share.OwnerID)
	ownerName := share.OwnerName
	if ownerName == "" {
		ownerName = owner.Name
	}
	if ownerName == "" {
		ownerName = i18n.T("未知联系人")
	}
	available := share.ID != "" && share.Available && owner.Online
	ui.Row(f).FillWidth().MinHeight(96).PadSides(18, 17, 18, 17).Gap(14).Items(ui.Center).
		Background(v.palette.chat).Border(1, v.palette.border).Radius(radiusControl).Children(func() {
		if v.avatar != nil {
			v.avatar(f, share.OwnerID, ownerName, owner.Avatar, 54, owner.Online)
		} else {
			ui.Row(f).Size(54, 54).Shrink(0).Radius(27).Background(v.palette.mutedAvatar).
				Items(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(f, initialOf(ownerName)).FontSize(min(v.theme.Metrics.ScaledText(18), 54*.42)).Weight(gfx.WeightSemibold)
			})
		}
		ui.Column(f).Grow(1).MinWidth(0).Gap(4).Children(func() {
			name := share.Name
			if name == "" {
				name = i18n.T("共享打印机已不可用")
			}
			ui.Text(f, name).FillWidth().FontSize(textSize(f, 18)).Weight(gfx.WeightSemibold).Ellipsis()
			ui.Text(f, i18n.T("共享人 ")+ownerName).FillWidth().Muted().FontSize(textSize(f, 13)).Ellipsis()
		})
		ui.Row(f).Shrink(0).Gap(7).Items(ui.Center).Children(func() {
			tone := v.palette.success
			label := i18n.T("可用")
			if !available {
				tone = v.palette.mutedAvatar
				label = i18n.T("离线")
			}
			ui.Row(f).Size(8, 8).Radius(4).Background(tone)
			ui.Text(f, label).FontSize(textSize(f, 13)).Color(tone)
		})
	})
}

func (v *printWindowView) drawFileSection(f ui.C) {
	ui.Column(f).FillWidth().Gap(9).Children(func() {
		ui.Text(f, i18n.T("打印文件")).FontSize(textSize(f, 21)).Weight(gfx.WeightSemibold)
		if v.path == "" {
			box := ui.Row(f).FillWidth().MinHeight(86).Pad(16).Gap(12).Items(ui.Center).
				Background(v.palette.chat).Border(1, v.palette.border).Radius(radiusControl).
				Hover(ui.Look{}.Background(v.palette.hover)).
				HitWith(v.ids.chooseFile, i18n.T("选择文件"), gfx.HitOptions{Role: gfx.RoleButton})
			box.Children(func() {
				ui.Row(f).Size(42, 42).Radius(radiusControl).Background(v.palette.selected).
					Items(ui.Center).Justify(ui.Center).Children(func() {
					ui.Icon(f, Upload).Size(20, 20).Color(v.palette.selectedText)
				})
				ui.Column(f).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(f, i18n.T("选择要打印的文件")).FillWidth().FontSize(textSize(f, 14)).Weight(gfx.WeightMedium).Ellipsis()
					ui.Text(f, i18n.T("支持 PDF、图片和文本文件")).FillWidth().Muted().FontSize(textSize(f, 11.5)).Ellipsis()
				})
				ui.Text(f, i18n.T("选择文件")).Shrink(0).FontSize(textSize(f, 13)).Color(v.palette.selectedText)
			})
			if box.Clicked() {
				v.chooseFile(f.Canvas())
			}
			return
		}
		ui.Row(f).FillWidth().MinHeight(86).Pad(16).Gap(13).Items(ui.Center).
			Background(v.palette.chat).Border(1, v.palette.border).Radius(radiusControl).Children(func() {
			ui.Row(f).Size(44, 44).Radius(radiusControl).Background(panelTint(v.theme.Primary, .10, v.palette.chat)).
				Items(ui.Center).Justify(ui.Center).Children(func() {
				ui.Icon(f, printFileIcon(domain.PrintJob{MIME: "", FileName: v.fileName})).
					Size(22, 22).Color(v.theme.Primary)
			})
			ui.Column(f).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(f, v.fileName).FontSize(textSize(f, 14)).Weight(gfx.WeightSemibold).Ellipsis()
				ui.Text(f, printWindowFileMeta(v.fileSize, v.fileName)).Muted().FontSize(textSize(f, 11.5)).Ellipsis()
			})
			if namedButton(f, v.ids.replace, i18n.T("更换文件")).Small().Outline().Clicked() {
				v.chooseFile(f.Canvas())
			}
		})
		if v.fileErr != "" {
			ui.Text(f, v.fileErr).FontSize(textSize(f, 11.5)).Color(v.palette.danger)
		}
	})
}

func (v *printWindowView) drawOptions(f ui.C, share domain.PrinterShare) {
	capabilities := printWindowCapabilities(share.Capabilities)
	ui.Column(f).FillWidth().Gap(10).Children(func() {
		ui.Text(f, i18n.T("打印参数")).FontSize(textSize(f, 21)).Weight(gfx.WeightSemibold)
		ui.Grid(f, v.theme.Metrics.ScaledText(250)).FillWidth().Gap(13).Children(func() {
			v.drawOption(f, i18n.T("纸张"), func() {
				ui.Select(f.Part("print-paper"), &v.paper, capabilities.Papers,
					func(value string) string { return printPaperLabel(value) }).FillWidth().MinHeight(printControlHeight(v.theme))
			})
			v.drawOption(f, i18n.T("份数"), func() {
				ui.Row(f).FillWidth().Gap(6).Children(func() {
					if panelActionIcon(f, v.theme, v.ids.copiesDown, printMinusIcon,
						i18n.T("減少份数"), false, false).Clicked() {
						v.changeCopies(-1)
					}
					ui.TextFieldID(f, v.ids.copiesField, &v.copies, i18n.T("份数"),
						i18n.T("1-999")).Grow(1).MinHeight(printControlHeight(v.theme))
					if panelActionIcon(f, v.theme, v.ids.copiesUp, Plus,
						i18n.T("增加份数"), false, false).Clicked() {
						v.changeCopies(1)
					}
				})
			})
			v.drawOption(f, i18n.T("方向"), func() {
				ui.Select(f.Part("print-orientation"), &v.orientation, capabilities.Orientations,
					printOrientationLabel).FillWidth().MinHeight(printControlHeight(v.theme))
			})
			v.drawOption(f, i18n.T("页面范围"), func() {
				modes := []string{"all", "custom"}
				ui.Select(f.Part("print-page-mode"), &v.pageMode, modes,
					func(value string) string {
						if value == "custom" {
							return i18n.T("自定义")
						}
						return i18n.T("全部")
					}).FillWidth().MinHeight(printControlHeight(v.theme)).Disabled(!capabilities.SupportsPageRange)
				if v.pageMode == "custom" && capabilities.SupportsPageRange {
					ui.TextFieldID(f, v.ids.rangeField, &v.pageRange,
						i18n.T("页码范围"), i18n.T("例如：1-3,5")).FillWidth().MinHeight(printControlHeight(v.theme))
				}
			})
			v.drawOption(f, i18n.T("色彩"), func() {
				ui.Select(f.Part("print-color"), &v.color, capabilities.ColorModes,
					printColorLabel).FillWidth().MinHeight(printControlHeight(v.theme))
			})
			v.drawOption(f, i18n.T("双面"), func() {
				ui.Select(f.Part("print-duplex"), &v.duplex, capabilities.DuplexModes,
					printDuplexLabel).FillWidth().MinHeight(printControlHeight(v.theme))
			})
		})
	})
}

func (v *printWindowView) drawOption(f ui.C, label string, body func()) {
	ui.Column(f).FillWidth().Gap(7).Children(func() {
		ui.Text(f, label).FontSize(textSize(f, 12.5)).Weight(gfx.WeightMedium)
		body()
	})
}

func (v *printWindowView) drawFooter(f ui.C, share domain.PrinterShare) {
	ownerName := share.OwnerName
	if ownerName == "" {
		ownerName = v.state.Peer(share.OwnerID).Name
	}
	if ownerName == "" {
		ownerName = i18n.T("共享人")
	}
	ui.Row(f).FillWidth().PadY(14).Gap(12).Items(ui.Center).Children(func() {
		ui.Text(f, fmt.Sprintf(i18n.T("%s 的 GoIM 将自动提交到打印队列。"), ownerName)).
			Muted().FontSize(textSize(f, 11.5)).Grow(1).Ellipsis()
		if namedButton(f, v.ids.cancel, i18n.T("取消")).Shrink(0).MinHeight(printControlHeight(v.theme)).Outline().Clicked() {
			if v.close != nil {
				v.close()
			}
		}
		disabled := v.shareID == "" || v.path == "" || !share.Available || !v.state.Peer(share.OwnerID).Online || v.pendingPrint
		label := i18n.T("发送打印")
		if v.needsPassword(share) {
			label = i18n.T("验证并打印")
		}
		if v.pendingPrint {
			label = i18n.T("正在验证…")
		}
		if namedButton(f, v.ids.send, label).Shrink(0).MinHeight(printControlHeight(v.theme)).Disabled(disabled).Clicked() {
			v.sendJob(share)
		}
	})
}

func (v *printWindowView) chooseFile(canvas *gfx.Canvas) {
	if v.pickFile == nil {
		return
	}
	canvas.AfterPaint(func() {
		paths, err := v.pickFile(app.OpenFileOptions{
			Title: i18n.T("选择要打印的文件"),
			Filters: []app.FileFilter{
				{Name: i18n.T("可打印文件"), Pattern: "*.pdf;*.png;*.jpg;*.jpeg;*.bmp;*.gif;*.tif;*.tiff;*.txt"},
				{Name: i18n.T("所有文件"), Pattern: "*.*"},
			},
		})
		if errors.Is(err, app.ErrCanceled) || errors.Is(err, app.ErrNoDriverResponse) {
			return
		}
		if err != nil {
			v.err = i18n.T("无法选择文件：") + filePickerProblem(err)
			v.wake.Invalidate()
			return
		}
		if len(paths) == 0 {
			return
		}
		v.setFile(paths[0])
		v.wake.Invalidate()
	})
}

func (v *printWindowView) setFile(path string) {
	v.path, v.fileName, v.fileSize, v.fileErr = "", "", 0, ""
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		v.err = i18n.T("所选文件已不可用")
		return
	}
	if info.Size() <= 0 {
		v.err = i18n.T("空文件无法打印")
		return
	}
	v.path = path
	v.fileName = filepath.Base(path)
	v.fileSize = info.Size()
	v.err = ""
}

func (v *printWindowView) changeCopies(delta int) {
	value, err := strconv.Atoi(strings.TrimSpace(v.copies.Text()))
	if err != nil {
		value = 1
	}
	value = min(max(value+delta, 1), 999)
	v.copies.SetText(strconv.Itoa(value))
}

func (v *printWindowView) sendJob(share domain.PrinterShare) {
	if v.submit == nil || v.path == "" || share.ID == "" || v.pendingPrint {
		return
	}
	copies, err := strconv.Atoi(strings.TrimSpace(v.copies.Text()))
	if err != nil || copies < 1 || copies > 999 {
		v.err = i18n.T("打印份数需在 1 到 999 之间")
		v.wake.Invalidate()
		return
	}
	pageRange := ""
	if v.pageMode == "custom" {
		pageRange = strings.TrimSpace(v.pageRange.Text())
		if pageRange == "" {
			v.err = i18n.T("请输入页码范围")
			v.wake.Invalidate()
			return
		}
	}
	options := domain.PrintOptions{
		Paper: v.paper, Copies: copies, Orientation: v.orientation,
		PageRange: pageRange, Color: v.color, Duplex: v.duplex,
	}
	if v.needsPassword(share) {
		password := v.password.Text()
		if password == "" {
			v.err = i18n.T("请输入打印密码")
			v.wake.Invalidate()
			return
		}
		v.authRequest = uint64(time.Now().UnixNano())
		v.pendingPrint, v.err = true, ""
		v.submit(domain.Command{Kind: domain.AuthenticatePrinter, PrinterID: share.ID, Password: password, Request: v.authRequest})
		v.wake.Invalidate()
		return
	}
	v.submit(domain.Command{Kind: domain.PrintFile, PrinterID: share.ID,
		Paths: []string{v.path}, PrintOptions: options})
	if v.close != nil {
		v.close()
	}
}

func printWindowFileMeta(size int64, name string) string {
	parts := []string{panelFormatBytes(size)}
	if ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."); ext != "" {
		parts = append(parts, strings.ToUpper(ext))
	}
	return strings.Join(parts, " · ")
}

func printPaperLabel(value string) string {
	switch value {
	case "A4":
		return "A4"
	case "A3":
		return "A3"
	case "Letter":
		return "Letter"
	case "Legal":
		return "Legal"
	default:
		return value
	}
}

func printOrientationLabel(value string) string {
	if value == domain.PrintOrientationLandscape {
		return i18n.T("横向")
	}
	return i18n.T("纵向")
}

func printColorLabel(value string) string {
	if value == domain.PrintColorColor {
		return i18n.T("彩色")
	}
	return i18n.T("黑白")
}

func printDuplexLabel(value string) string {
	switch value {
	case domain.PrintDuplexLongEdge:
		return i18n.T("双面（长边）")
	case domain.PrintDuplexShortEdge:
		return i18n.T("双面（短边）")
	default:
		return i18n.T("单面")
	}
}
