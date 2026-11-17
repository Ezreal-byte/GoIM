package desktop

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
	"goim/internal/selfupdate"
)

type updateControls struct {
	chrome, scope, dismiss, close, action, notes, download, cancel gfx.ID
}

func (c *updateControls) init(tree *gfx.Tree, scope gfx.Scope) {
	c.chrome = tree.ID(scope, "update-titlebar")
	c.scope = tree.ID(scope, "update-dialog")
	c.dismiss = tree.ID(scope, "update-dismiss")
	c.close = tree.ID(scope, "update-close")
	c.action = tree.ID(scope, "update-primary")
	c.notes = tree.ID(scope, "update-notes")
	c.download = tree.ID(scope, "update-manual")
	c.cancel = tree.ID(scope, "update-secondary")
}

func (u *UI) showUpdates(check bool) {
	u.updateOpen = true
	if check && u.updater != nil {
		u.updater.checkNow()
		u.observeUpdate()
	}
}

func (u *UI) observeUpdate() {
	u.update = u.updater.snapshot()
	u.panels.update = u.update
	if offer := u.update.offer.Version; offer != "" && u.updateAnnounced != offer {
		u.updateAnnounced = offer
		u.showToast(i18n.T("发现新版本"), "GoIM "+offer, components.ToastInfo)
	}
	if previous := u.update.previous; previous != "" && !u.updateResultShown {
		u.updateResultShown = true
		if u.update.success {
			u.showToast(i18n.T("GoIM 已升级"), "GoIM "+previous, components.ToastSuccess)
		} else {
			u.showToast(i18n.T("上次升级未完成"), i18n.T("可在关于中重试或手动下载。"), components.ToastWarning)
		}
	}
}

func (u *UI) drawUpdateTitlebar(f ui.C, wide bool) {
	if u.update.offer.Version == "" || u.state.Locked {
		return
	}
	label := i18n.T("有更新")
	if !wide {
		if u.iconButton(f, u.updateIDs.chrome, u.iconDownload, label, 26).Clicked() {
			u.showUpdates(false)
		}
		return
	}
	if namedButton(f, u.updateIDs.chrome, label).Small().Ghost().Clicked() {
		u.showUpdates(false)
	}
}

func (v updateView) status() string {
	if v.problem != "" {
		return i18n.T(v.problem)
	}
	switch v.phase {
	case updateChecking:
		return i18n.T("正在检查更新…")
	case updateCurrent:
		return i18n.T("当前已是最新版本")
	case updateOffered:
		if !v.offer.Automatic {
			return i18n.T("请下载与当前安装方式对应的软件包。")
		}
		return i18n.T("下载完成后可安装并重启 GoIM。")
	case updateDownloading:
		return i18n.T("正在下载更新…")
	case updateReady:
		return i18n.T("更新已下载，可安装并重启。")
	case updateInstalling:
		return i18n.T("正在准备安装…")
	default:
		return i18n.T("当前构建未配置在线更新")
	}
}

func (p *Panels) drawUpdateSettings(f ui.C, theme ui.Theme) {
	panelSection(f, i18n.T("软件更新"), "", func() {
		p.settingsRow(f, theme, i18n.T("检查更新"), p.update.status(), func() {
			check := p.update.offer.Version == ""
			label := i18n.T("检查更新")
			if !check {
				label = i18n.T("查看更新")
			}
			if p.openUpdates != nil &&
				namedButton(f, p.ids.aboutCards.ID(p.tree.Name("check-updates")), label).Small().Outline().Clicked() {
				p.openUpdates(check)
			}
		})
	})
}

func (u *UI) drawUpdateDialog(c *gfx.Canvas, viewport gfx.Rect) {
	view, ids := u.update, u.updateIDs
	installBlocked := u.updateBlocked()
	u.dialogPage.Draw(c, viewport, func(f ui.C) {
		dialog := ui.Dialog(f, ui.DialogSpec{Title: i18n.T("软件更新"), Layer: 77, Width: 470,
			Scope: ids.scope, DismissID: ids.dismiss, CloseID: ids.close,
			InitialFocusID: ids.action, NoClose: view.phase == updateInstalling})
		open := func(url string) { u.panels.openAboutURL(c, url) }
		dialog.Body(func() {
			ui.Column(f).FillWidth().Gap(10).Children(func() {
				if view.offer.Version != "" {
					title := "GoIM " + view.offer.Version
					if view.offer.Bytes > 0 {
						title += " · " + formatBytes(view.offer.Bytes)
					}
					ui.Text(f, title).Weight(gfx.WeightSemibold)
				}
				ui.Paragraph(f, view.status(), 3).Muted()
				if view.phase == updateDownloading || view.phase == updateReady {
					ui.Progress(f, view.progress).FillWidth()
					if view.phase == updateDownloading {
						ui.Text(f, fmt.Sprintf("%.0f%%", view.progress*100)).Muted()
					}
				}
				if view.phase == updateReady && installBlocked {
					ui.Paragraph(f, i18n.T("请等文件传输或截图结束，再安装更新。"), 2).Muted()
				}
				if view.offer.NotesURL != "" {
					ui.Row(f).Gap(6).Children(func() {
						if namedButton(f, ids.notes, i18n.T("发布说明")).Small().Ghost().Clicked() {
							open(view.offer.NotesURL)
						}
						if view.offer.Automatic && (view.phase == updateOffered || view.phase == updateDownloadFailed) &&
							namedButton(f, ids.download, i18n.T("官方下载")).Small().Ghost().Clicked() {
							open(view.offer.DownloadURL)
						}
					})
				}
			})
		})
		dialog.Footer(func() {
			if view.phase != updateInstalling && dialogActionButton(f, ids.cancel, i18n.T("关闭")).Outline().Clicked() {
				u.updateOpen = false
			}
			switch view.phase {
			case updateChecking, updateInstalling:
			case updateDisabled:
				if dialogActionButton(f, ids.action, i18n.T("打开发布页")).Clicked() {
					open(selfupdate.Releases + "/latest")
				}
			case updateOffered, updateDownloadFailed:
				if !view.offer.Automatic {
					if dialogActionButton(f, ids.action, i18n.T("官方下载")).Clicked() {
						open(view.offer.DownloadURL)
					}
				} else {
					label := i18n.T("下载更新")
					if view.problem != "" {
						label = i18n.T("继续下载")
					}
					if dialogActionButton(f, ids.action, label).Clicked() {
						u.updater.download()
					}
				}
			case updateDownloading:
				if dialogActionButton(f, ids.action, i18n.T("暂停下载")).Clicked() {
					u.updater.pauseDownload()
				}
			case updateReady:
				if dialogActionButton(f, ids.action, i18n.T("安装并重启")).Disabled(installBlocked).Clicked() {
					c.AfterPaint(func() {
						// Consult the backend again after the paint pass.
						// Incoming work can have started since the last wake.
						if u.backend != nil && hasActiveTransfers(u.backend.Snapshot().Transfers) || u.updateBlocked() {
							u.invalidator.Invalidate()
							return
						}
						u.updater.install(u.restartArguments)
					})
				}
			default:
				if dialogActionButton(f, ids.action, i18n.T("检查更新")).Clicked() {
					u.updater.checkNow()
				}
			}
		})
		if view.phase != updateInstalling && dialog.Dismissed() {
			u.updateOpen = false
		}
	})
}

func (u *UI) updateBlocked() bool {
	return u.screenshotActive != nil && u.screenshotActive() || hasActiveTransfers(u.state.Transfers)
}

func hasActiveTransfers(transfers []domain.Transfer) bool {
	for _, transfer := range transfers {
		if transfer.Status == "transferring" || transfer.Status == "verifying" {
			return true
		}
	}
	return false
}
