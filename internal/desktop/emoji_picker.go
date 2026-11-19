package desktop

import (
	"github.com/vivaui/viva/gfx"
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (u *UI) drawEmojiPicker(c *gfx.Canvas, viewport ui.Rect) {
	if !u.emojiOpen {
		return
	}
	if !u.emojiButton.Valid() || u.selected == "" {
		u.emojiOpen = false
		return
	}
	anchor := u.emojiButton.Rect()

	size := ui.Size{Width: 466, Height: min(350, max(anchor.Y-viewport.Y-16, 175))}
	panel, _ := components.PlaceOverlay(anchor, size, viewport, components.OverlayPlacement{
		Side: components.OverlayTop, Align: components.OverlayAlignStart, SideOffset: 8, Margin: 8,
	})
	u.emojiPanel = panel
	c.PushLayer(58)
	defer c.PopLayer()
	c.PushFocusScope(u.ids.emojiScope)
	defer c.PopFocusScope()
	c.HitWithOptions(u.ids.emojiDismiss, viewport, i18n.T("关闭表情面板"), gfx.HitOptions{
		Role: gfx.RoleButton, SkipFocus: true, Backdrop: true, PassPrimary: true, PassSecondary: true,
	})
	c.HitWithOptions(u.ids.emojiDismiss, anchor, i18n.T("关闭表情面板"), gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true, Backdrop: true})
	c.SetEscapeTarget(u.ids.emojiDismiss)
	c.RoundedBorder(panel, 12, 1, u.palette.border, u.theme.Popover)
	c.Block(panel)
	columns := max(1, min(10, int((panel.Width-28)/42)))

	var picked string
	u.emojiPage.Draw(c, panel, func(f ui.C) {
		ui.Scroll(f).Scrollbars().Fill().PadSides(12, 12, 14, 12).Children(func() {
			ui.Column(f).FillWidth().Gap(7).Children(func() {
				if len(u.recentEmoji) > 0 {
					ui.Text(f, i18n.T("最近使用")).FontSize(12).Muted().PadX(6)
					for start := 0; start < len(u.recentEmoji); start += columns {
						ui.Row(f).Height(41).Children(func() {
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
					ui.Divider(f)
				}
				ui.Text(f, i18n.T("所有表情")).FontSize(12).Muted().PadX(6)
				for start := 0; start < len(emojiChoices); start += columns {
					ui.Row(f).Height(41).Children(func() {
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
	if picked != "" {
		u.insertChatText(picked)
		u.recentEmoji = slices.DeleteFunc(u.recentEmoji, func(old string) bool { return old == picked })
		u.recentEmoji = append([]string{picked}, u.recentEmoji...)
		u.recentEmoji = u.recentEmoji[:min(10, len(u.recentEmoji))]
		u.submit(domain.Command{Kind: domain.RememberEmoji, Text: picked})
		u.emojiOpen = false
		c.AfterPaint(func() { c.RequestFocus(u.ids.chatEditorField) })
	}
	if c.Clicked(u.ids.emojiDismiss) {
		u.emojiOpen = false
	}
}

func (u *UI) emojiTile(f ui.C, choice emojiChoice, id gfx.ID) bool {
	label := i18n.T(choice.Label)
	tile := ui.Row(f).Size(42, 41).Radius(7).Items(ui.Center).Justify(ui.Center).
		Hover(ui.Look{}.Background(u.palette.hover)).
		HitWith(id, i18n.T("表情 ")+label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label})
	if f.Canvas().FocusVisible() == id {
		tile.Stroke(1.5, u.theme.Ring)
	}
	return tile.Children(func() { ui.Icon(f, choice.Art).Size(30, 30) }).Clicked()
}
