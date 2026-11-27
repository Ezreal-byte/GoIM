package desktop

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

const (
	interfaceFontSample         = "The quick brown fox 敏捷的棕色狐狸 0123456789"
	interfaceFontSampleSymbols  = "Aa Bb Cc 你好，世界！ (1) [2] {3} @ # % &"
	interfaceFontRowStep        = float32(34)
	interfaceFontVisibleRows    = 8
	interfaceFontListHeight     = interfaceFontRowStep * interfaceFontVisibleRows
	interfaceFontDialogListGap  = float32(10)
	interfaceFontDialogSidePad  = float32(4)
	interfaceFontPickerLoadKind = 1
)

type interfaceFontEntry struct {
	label  string
	family string
}

type panelInterfaceFontPicker struct {
	trigger, scope, dismiss, close, search, cancel, confirm gfx.ID
	list                                                    gfx.Scope

	open         bool
	searchText   ui.Field
	load         *ui.EffectState[[]string, int]
	families     []string
	known        bool
	loading      bool
	supported    bool
	supportKnown bool

	entries      []interfaceFontEntry
	filtered     []int
	selected     int
	scroll       float32
	rowStep      float32
	scrollManual bool
	built        bool
	builtRaw     string
	builtList    int
	missing      bool

	state    domain.State
	submit   func(domain.Command)
	settings domain.Settings
}

func (f *panelInterfaceFontPicker) init(tree *gfx.Tree, scope gfx.Scope) {
	f.rowStep = interfaceFontRowStep
	f.trigger = tree.ID(scope, "interface-font")
	f.scope = tree.ID(scope, "interface-font-dialog")
	f.dismiss = tree.ID(scope, "interface-font-dismiss")
	f.close = tree.ID(scope, "interface-font-close")
	f.search = tree.ID(scope, "interface-font-search")
	f.list = tree.Sub(scope, tree.Name("interface-font-list"))
	f.cancel = tree.ID(scope, "interface-font-cancel")
	f.confirm = tree.ID(scope, "interface-font-confirm")
}

func (p *Panels) interfaceFontDescription(settings domain.Settings) string {
	font := &p.settingsExtra.interfaceFont
	family := strings.TrimSpace(settings.InterfaceFont)
	if family != "" && font.supportKnown && !font.supported {
		return i18n.T("当前平台不支持更改界面字体")
	}
	if family != "" && font.missing {
		return i18n.T("已保存的字体未安装，暂时使用系统字体")
	}
	if family == "" {
		return fmt.Sprintf(i18n.T("当前：%s"), i18n.T("系统默认"))
	}
	return fmt.Sprintf(i18n.T("当前：%s"), family)
}

func (p *Panels) openInterfaceFontPicker() {
	font := &p.settingsExtra.interfaceFont
	font.open = true
	font.searchText = ui.Field{}
	font.built = false
	font.builtList++
	if font.known {
		font.loading = false
		font.load = nil
		return
	}
	font.families = nil
	font.loading = true
	font.load = ui.NewEffect[[]string, int](func(context.Context) ([]string, error) {
		if p.fontFamilies == nil {
			return nil, nil
		}
		return p.fontFamilies(), nil
	})
}

func (p *Panels) closeInterfaceFontPicker() {
	font := &p.settingsExtra.interfaceFont
	font.open = false
	font.loading = false
	font.load = nil
}

func (p *Panels) confirmInterfaceFont() {
	font := &p.settingsExtra.interfaceFont
	font.rebuild(strings.TrimSpace(font.settings.InterfaceFont), false)
	if font.selected < 0 || font.selected >= len(font.filtered) || font.submit == nil {
		return
	}
	entry := font.entries[font.filtered[font.selected]]
	if p.setInterfaceFont != nil {
		font.supported = p.setInterfaceFont(entry.family)
		font.supportKnown = true
	}
	p.submitString(font.submit, font.state, "interface_font", entry.family)
	p.closeInterfaceFontPicker()
}

func (f *panelInterfaceFontPicker) rebuild(current string, selectCurrent bool) {
	raw := strings.TrimSpace(f.searchText.Text())
	if f.built && raw == f.builtRaw && f.builtList == len(f.families) {
		return
	}
	previousFamily := ""
	if f.selected >= 0 && f.selected < len(f.filtered) {
		previousFamily = f.entries[f.filtered[f.selected]].family
	}
	f.built, f.builtRaw = true, raw
	f.entries = f.entries[:0]
	f.entries = append(f.entries, interfaceFontEntry{label: i18n.T("系统默认")})
	for _, family := range f.families {
		f.entries = append(f.entries, interfaceFontEntry{label: family, family: family})
	}
	query := strings.ToLower(raw)
	f.filtered = f.filtered[:0]
	for index, entry := range f.entries {
		if query != "" && !strings.Contains(strings.ToLower(entry.label), query) {
			continue
		}
		f.filtered = append(f.filtered, index)
	}
	f.selected = min(max(f.selected, 0), max(len(f.filtered)-1, 0))
	if selectCurrent {
		f.selectFamily(current)
	} else if previousFamily != "" {
		f.selectFamily(previousFamily)
	}
	f.builtList = len(f.families)
	f.scroll = min(max(f.scroll, 0), f.maxScroll())
}

func (f *panelInterfaceFontPicker) selectFamily(family string) {
	for row, index := range f.filtered {
		if f.entries[index].family == family {
			f.selected = row
			f.scroll = min(float32(max(row-f.visibleRows()/2, 0))*f.rowStep, f.maxScroll())
			f.scrollManual = true
			return
		}
	}
	f.selected = 0
	f.scroll = 0
	f.scrollManual = true
}

func (f *panelInterfaceFontPicker) move(delta int) {
	if len(f.filtered) == 0 {
		return
	}
	f.selected = min(max(f.selected+delta, 0), len(f.filtered)-1)
	top := float32(f.selected) * f.rowStep
	f.scroll = min(max(f.scroll, top+f.rowStep-interfaceFontListHeight), top)
	f.scroll = min(max(f.scroll, 0), f.maxScroll())
	f.scrollManual = true
}

func (f *panelInterfaceFontPicker) contentHeight() float32 {
	return max(0, float32(len(f.filtered))*f.rowStep-2)
}

func (f *panelInterfaceFontPicker) visibleRows() int {
	return max(1, int(interfaceFontListHeight/f.rowStep))
}

func (f *panelInterfaceFontPicker) maxScroll() float32 {
	return max(0, f.contentHeight()-interfaceFontListHeight)
}

func (f *panelInterfaceFontPicker) key(event gfx.KeyEvent, confirm func()) (gfx.EventResult, bool) {
	handled := gfx.EventResult{Handled: true, Repaint: true}
	switch event.Key {
	case gfx.KeyEscape:
		f.open = false
		f.load = nil
		f.loading = false
		return handled, true
	case gfx.KeyUp:
		f.move(-1)
		return handled, true
	case gfx.KeyDown:
		f.move(1)
		return handled, true
	case gfx.KeyPageUp:
		f.move(-f.visibleRows())
		return handled, true
	case gfx.KeyPageDown:
		f.move(f.visibleRows())
		return handled, true
	case gfx.KeyEnter:
		_, onList := event.Target.In(f.list)
		if event.Target != f.search && !onList && !event.Target.IsZero() {
			return gfx.EventResult{}, false
		}
		if !event.Repeat {
			confirm()
		}
		return handled, true
	}
	return gfx.EventResult{}, false
}

func (p *Panels) drawInterfaceFontPicker(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	font := &p.settingsExtra.interfaceFont
	if !font.open {
		return
	}
	font.state, font.submit, font.settings = state, submit, state.Settings
	font.rowStep = max(interfaceFontRowStep, theme.Metrics.ScaledText(20)+8)

	if load := font.load; load != nil {
		ui.Effect(f, load, interfaceFontPickerLoadKind)
		if families, _, ok := load.Take(); ok {
			font.load = nil
			font.loading = false
			font.known = true
			font.families = append(font.families[:0], families...)
			font.builtList++
			font.supported = len(families) > 0
			font.supportKnown = true
			family := strings.TrimSpace(state.Settings.InterfaceFont)
			font.missing = family != "" && len(families) > 0 && !slices.Contains(families, family)
		}
	}
	family := strings.TrimSpace(state.Settings.InterfaceFont)
	if font.known {
		font.missing = family != "" && len(font.families) > 0 && !slices.Contains(font.families, family)
	}
	font.rebuild(family, !font.built)

	if len(font.filtered) > 0 {
		if row, event, ok := f.Canvas().PointerIn(font.list); ok &&
			event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && event.ClickCount == 2 {
			font.selected = row
			f.Canvas().Consume()
			p.confirmInterfaceFont()
			return
		}
	}

	dialog := p.dialog(f, ui.DialogSpec{
		Title:          i18n.T("界面字体"),
		Description:    i18n.T("更改 GoIM 窗口、菜单和设置页使用的字体"),
		Width:          560,
		Layer:          80,
		Scope:          font.scope,
		DismissID:      font.dismiss,
		CloseID:        font.close,
		InitialFocusID: font.search,
	})
	var canceled, confirmed bool
	dialog.Body(func() {
		ui.Column(f).FillWidth().Gap(interfaceFontDialogListGap).Children(func() {
			ui.TextField(f, &font.searchText, i18n.T("界面字体"), i18n.T("搜索已安装的字体"), ui.WithID(font.search)).
				Height(38).FillWidth()
			ui.Column(f).Height(interfaceFontListHeight).Shrink(0).Children(func() {
				switch {
				case font.loading:
					ui.Text(f, i18n.T("正在读取已安装的字体…")).Muted().FontSize(theme.Metrics.FontSizeS + 1).
						TextCenter().PadY(24)
				case len(font.filtered) == 0 && font.known && len(font.families) == 0:
					ui.Text(f, i18n.T("当前平台没有可枚举的系统字体。")).Muted().FontSize(theme.Metrics.FontSizeS + 1).
						TextCenter().PadY(24)
				case len(font.filtered) == 0:
					ui.Text(f, i18n.T("未找到匹配的字体。")).Muted().FontSize(theme.Metrics.FontSizeS + 1).
						TextCenter().PadY(24)
				default:
					p.drawInterfaceFontRows(f, theme, font)
				}
			})
			if len(font.filtered) > 0 {
				p.drawInterfaceFontPreview(f, theme, font)
			}
		})
	})
	dialog.Footer(func() {
		canceled = dialogActionButton(f, font.cancel, i18n.T("取消")).Outline().Clicked()
		confirmed = dialogActionButton(f, font.confirm, i18n.T("应用所选字体")).
			Disabled(font.loading || len(font.filtered) == 0).Clicked()
	})
	if canceled || dialog.Dismissed() {
		p.closeInterfaceFontPicker()
		return
	}
	if confirmed {
		p.confirmInterfaceFont()
	}
}

func (p *Panels) drawInterfaceFontRows(f ui.C, theme ui.Theme, font *panelInterfaceFontPicker) {
	area := ui.Scroll(f.Part("interface-font-list")).Scrollbars()
	if font.scrollManual {
		area.ScrollState().ScrollTo(font.scroll)
		font.scrollManual = false
	}
	font.scroll = area.ScrollState().Offset
	first := int(font.scroll / font.rowStep)
	last := min(len(font.filtered), first+font.visibleRows()+2)
	area.Fill().Children(func() {
		if top := float32(first) * font.rowStep; top > 0 {
			ui.Slot(f).Height(top)
		}
		for row := first; row < last; row++ {
			entry := font.entries[font.filtered[row]]
			selected := row == font.selected
			color := theme.Foreground
			line := ui.Row(f).Height(font.rowStep-2).Shrink(0).PadSides(interfaceFontDialogSidePad, 12, 0, 10).
				Items(ui.Center).Radius(theme.Metrics.RadiusControl).
				HitWith(font.list.At(row), entry.label, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected})
			if selected {
				line.Background(panelAccentSurface(theme))
				color = panelAccentText(theme)
			} else {
				line.Hover(ui.Look{}.Background(theme.Hover))
			}
			if line.Clicked() {
				font.selected = row
			}
			line.Children(func() {
				ui.Text(f, entry.label).FontSize(theme.Metrics.FontSizeM + 1).Weight(gfx.WeightMedium).
					Color(color).LineHeight(textSize(f, 20)).Grow(1).Ellipsis()
				if font.entries[font.filtered[row]].family == strings.TrimSpace(font.settings.InterfaceFont) {
					ui.Text(f, i18n.T("当前")).Muted().FontSize(theme.Metrics.FontSizeS)
				}
			})
		}
		if rest := font.contentHeight() - float32(last)*font.rowStep; rest > 0 {
			ui.Slot(f).Height(rest)
		}
	})
	font.scroll = area.ScrollState().Offset
}

func (p *Panels) drawInterfaceFontPreview(f ui.C, theme ui.Theme, font *panelInterfaceFontPicker) {
	entry := font.entries[font.filtered[font.selected]]
	lineHeight := theme.Metrics.ScaledText(22)
	ui.Column(f).Height(max(66, 2*lineHeight+20)).Shrink(0).PadSides(12, 12, 8, 8).
		Background(theme.Muted).Border(1, theme.Border).Radius(theme.Metrics.RadiusControl).Children(func() {
		first := ui.Text(f, interfaceFontSample).FontSize(theme.Metrics.FontSizeM).
			Color(theme.Foreground).LineHeight(lineHeight).Height(lineHeight).Ellipsis()
		second := ui.Text(f, interfaceFontSampleSymbols).FontSize(theme.Metrics.FontSizeM).
			Color(theme.MutedForeground).LineHeight(lineHeight).Height(lineHeight).Ellipsis()
		if entry.family != "" {
			face := gfx.MonoFamily(entry.family)
			first.MonoFace(face)
			second.MonoFace(face)
		}
	})
}
