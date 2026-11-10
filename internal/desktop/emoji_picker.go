package desktop

import (
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (u *UI) drawEmojiPicker(c *ui.Canvas, viewport ui.Rect) {
	if !u.emojiOpen {
		return
	}
	anchor := u.emojiButton.Rect()
	if anchor.Width <= 0 || u.selected == "" {
		u.emojiOpen = false
		return
	}

	size := ui.Size{Width: 466, Height: min(350, max(anchor.Y-viewport.Y-16, 175))}
	panel, _ := components.PlaceOverlay(anchor, size, viewport, components.OverlayPlacement{
		Side: components.OverlayTop, Align: components.OverlayAlignStart, SideOffset: 8, Margin: 8,
	})
	u.emojiPanel = panel
	c.PushLayer(58)
	defer c.PopLayer()
	c.PushFocusScope(u.ids.emojiScope)
	defer c.PopFocusScope()
	c.HitWithOptions(u.ids.emojiDismiss, viewport, i18n.T("关闭表情面板"), ui.HitOptions{
		Role: ui.RoleButton, SkipFocus: true, Backdrop: true, PassPrimary: true, PassSecondary: true,
	})
	c.HitWithOptions(u.ids.emojiDismiss, anchor, i18n.T("关闭表情面板"), ui.HitOptions{Role: ui.RoleButton, SkipFocus: true, Backdrop: true})
	c.SetEscapeTarget(u.ids.emojiDismiss)
	c.RoundedBorder(panel, 12, 1, u.palette.border, u.theme.Popover)
	c.Block(panel)
	columns := max(1, min(10, int((panel.Width-28)/42)))

	var picked string
	u.emojiView.Draw(c, u.theme, panel, func(f *flex.Frame) {
		f.ScrollArea(u.ids.emojiScroll).Fill().PadSides(12, 12, 14, 12).Children(func() {
			f.Column().FillWidth().Gap(7).Children(func() {
				if len(u.recentEmoji) > 0 {
					f.Text(i18n.T("最近使用")).FontSize(12).Muted().PadX(6)
					for start := 0; start < len(u.recentEmoji); start += columns {
						f.Row().Height(41).Children(func() {
							for _, value := range u.recentEmoji[start:min(start+columns, len(u.recentEmoji))] {
								for _, choice := range emojiChoices {
									if choice.Value == value {
										if u.emojiTile(f, choice, u.emojiRecentIDs[choice.Name]) {
											picked = choice.Value
										}
										break
									}
								}
							}
						})
					}
					f.Divider()
				}
				f.Text(i18n.T("所有表情")).FontSize(12).Muted().PadX(6)
				for start := 0; start < len(emojiChoices); start += columns {
					f.Row().Height(41).Children(func() {
						for _, choice := range emojiChoices[start:min(start+columns, len(emojiChoices))] {
							if u.emojiTile(f, choice, u.emojiIDs[choice.Name]) {
								picked = choice.Value
							}
						}
					})
				}
			})
		})
	})
	u.drawEmojiScrollbar(c, panel)
	if picked != "" {
		u.insertChatText(picked)
		u.recentEmoji = slices.DeleteFunc(u.recentEmoji, func(old string) bool { return old == picked })
		u.recentEmoji = append([]string{picked}, u.recentEmoji...)
		u.recentEmoji = u.recentEmoji[:min(10, len(u.recentEmoji))]
		u.submit(domain.Command{Kind: domain.RememberEmoji, Text: picked})
		u.emojiOpen = false
		u.emojiScrollDrag = false
		c.RequestFocus(u.ids.chatEditor)
	}
	if c.Clicked(u.ids.emojiDismiss) {
		u.emojiOpen = false
		u.emojiScrollDrag = false
	}
}

func (u *UI) emojiTile(f *flex.Frame, choice emojiChoice, id ui.ID) bool {
	label := i18n.T(choice.Label)
	tile := f.Row().Size(42, 41).Radius(7).Items(flex.Center).Justify(flex.Center).
		Hover(flex.Look{}.Background(u.palette.hover)).
		HitWith(id, i18n.T("表情 ")+label, ui.HitOptions{Role: ui.RoleButton, Hint: label})
	if f.Canvas().FocusVisible() == id {
		tile.Stroke(1.5, u.theme.Ring)
	}
	return tile.Children(func() { f.Icon(choice.Art).Size(30, 30) }).Clicked()
}

func (u *UI) drawEmojiScrollbar(c *ui.Canvas, panel ui.Rect) {
	state := u.emojiView.ScrollState(u.ids.emojiScroll)
	if state == nil || state.Max() <= 0 {
		return
	}
	bounds := ui.Rect{X: panel.X + 3, Y: panel.Y + 12, Width: panel.Width - 6, Height: state.Viewport}
	theme := u.theme
	theme.Border = u.palette.secondaryText
	theme.Border.Alpha = .45
	area := components.BeginScrollArea(c, theme, bounds, ui.Size{Width: bounds.Width, Height: state.Content}).
		Offset(0, state.Offset).HoverScrollbars(true).VerticalScrollbar(u.ids.emojiScrollbar, i18n.T("滚动表情"), "")
	geometry := area.EndContent()
	event, ok := c.PointerOn(u.ids.emojiScrollbar)
	if !ok {
		return
	}
	switch event.Kind {
	case ui.PointerDown:
		if event.Button != ui.PointerButtonPrimary {
			return
		}
		thumb := geometry.VerticalThumb
		u.emojiScrollDrag = true
		u.emojiScrollGrab = thumb.Height / 2
		if pointInside(event.X, event.Y, thumb) {
			u.emojiScrollGrab = event.Y - thumb.Y
		}
	case ui.PointerMove:
		if !u.emojiScrollDrag {
			return
		}
	case ui.PointerUp:
		if !u.emojiScrollDrag {
			return
		}
		u.emojiScrollDrag = false
	default:
		return
	}
	state.ScrollTo(geometry.VerticalOffsetForThumb(event.Y - u.emojiScrollGrab))
	c.Consume()
	c.RequestFrame()
}
