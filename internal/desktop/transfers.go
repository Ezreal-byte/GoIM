package desktop

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func (p *Panels) drawTransfers(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	f.Column().Fill().PadSides(26, 32, 14, 32).Gap(14).Children(func() {
		panelPageHeader(f, theme, "传输", "逐位查看接收方进度，或恢复已结束的任务", func() {
			f.Text(fmt.Sprintf("%d 进行中", len(p.transferPending))).Muted().
				FontSize(theme.Metrics.FontSizeS + 1)
		})
		p.panelFeedback(f, theme, state, submit, p.ids.transfersDismiss, p.ids.transfersClearError)

		f.Row().FillWidth().Items(flex.Center).Children(func() {
			choice := f.Choice(p.ids.transferFilters, p.transferLabels, p.transferFilter).Width(300)
			if index, ok := choice.ChosenIndex(); ok {
				p.transferFilter = index
			}
			f.Spacer()
			f.Text("每位接收者拥有独立状态").Muted().FontSize(theme.Metrics.FontSizeS)
		})

		f.ScrollArea(p.ids.transferScroll).Grow(1).FillWidth().Children(func() {
			f.Column().FillWidth().PadSides(0, 0, 18, 0).Children(func() {
				entries := p.transferPending
				emptyTitle, emptyDescription := "暂无进行中的传输", "收到文件邀请或开始发送后，任务会显示在这里。"
				if p.transferFilter == panelTransferFinished {
					entries = p.transferFinished
					emptyTitle, emptyDescription = "暂无已结束的传输", "已完成、已拒绝、已取消和失败的任务会显示在这里。"
				}
				if len(entries) == 0 {
					panelEmpty(f, theme, emptyTitle, emptyDescription)
					return
				}
				for index := range entries {
					p.drawTransferRow(f, theme, index, entries[index], submit)
				}
			})
		})
	})
}

func (p *Panels) drawTransferRow(f *flex.Frame, theme ui.Theme, index int, entry panelTransferEntry, submit func(domain.Command)) {
	tone := panelStatusColor(theme, entry.status)
	f.Column().FillWidth().PadY(15).Gap(9).Children(func() {
		f.Row().FillWidth().Gap(12).Items(flex.Start).Children(func() {
			icon := panelIconUpload
			if entry.transfer.Direction == "receive" {
				icon = panelIconDownload
			}
			f.Row().Width(38).Height(38).Radius(9).Background(theme.Muted).Items(flex.Center).
				Justify(flex.Center).Children(func() {
				f.Icon(icon).Size(18, 18).Color(panelAccentText(theme))
			})
			f.Column().Grow(1).Gap(4).Children(func() {
				f.Text(panelTransferName(entry)).FontSize(theme.Metrics.FontSizeM + 1).
					Weight(ui.WeightMedium).Ellipsis()
				f.Paragraph(entry.meta, 2).Muted().FontSize(theme.Metrics.FontSizeS)
			})
			panelPill(f, theme, entry.status, tone)
		})

		if panelTransferActive(entry.transfer.Status) && entry.transfer.Size > 0 {
			f.Progress(entry.fraction).Height(7)
			f.Row().FillWidth().Children(func() {
				f.Text(fmt.Sprintf("%s / %s · %d%%",
					panelFormatBytes(entry.transfer.Done),
					panelFormatBytes(entry.transfer.Size),
					int(entry.fraction*100))).Muted().FontSize(theme.Metrics.FontSizeS).Grow(1)
				if entry.transfer.Speed > 0 {
					f.Text(panelFormatBytes(int64(entry.transfer.Speed)) + "/s").Muted().
						FontSize(theme.Metrics.FontSizeS)
				}
			})
		}

		if entry.err != "" {
			f.Paragraph(entry.err, 3).FontSize(theme.Metrics.FontSizeS).Color(theme.Danger)
		}
		if entry.path != "" && entry.transfer.Status == "completed" {
			f.Row().FillWidth().Gap(7).Items(flex.Center).Children(func() {
				f.Icon(panelIconFolder).Size(14, 14).Color(theme.MutedForeground)
				f.Text(entry.path).FontSize(theme.Metrics.FontSizeS).Muted().Ellipsis().Grow(1)
			})
		}

		if entry.accept || entry.saveAs || entry.reject || entry.cancel || entry.retry || entry.open || entry.reveal {
			f.Row().FillWidth().Gap(8).Items(flex.Center).Children(func() {
				f.Spacer()
				if entry.retry {
					if f.Button(p.ids.transferRetry.At(index), "重试").Small().Outline().Clicked() {
						submit(domain.Command{Kind: domain.RetryTransfer, TransferID: entry.transfer.ID})
					}
				}
				if entry.reject {
					if f.Button(p.ids.transferReject.At(index), "拒绝").Small().Outline().Clicked() {
						submit(domain.Command{Kind: domain.RejectTransfer, TransferID: entry.transfer.ID})
					}
				}
				if entry.cancel {
					if f.Button(p.ids.transferCancel.At(index), "取消").Small().DestructiveOutline().Clicked() {
						submit(domain.Command{Kind: domain.CancelTransfer, TransferID: entry.transfer.ID})
					}
				}
				if entry.open {
					if f.Button(p.ids.transferOpen.At(index), "打开").Small().Outline().Clicked() {
						panelOpenLocal(f.Canvas(), p, entry.path, false)
					}
				}
				if entry.reveal {
					if f.Button(p.ids.transferReveal.At(index), "打开文件夹").Small().Outline().Clicked() {
						panelOpenLocal(f.Canvas(), p, entry.path, true)
					}
				}
				if entry.saveAs {
					if f.Button(p.ids.transferSaveAs.At(index), "另存为…").Small().Outline().Clicked() {
						p.saveTransferAs(f.Canvas(), submit, entry.transfer)
					}
				}
				if entry.accept {
					if f.Button(p.ids.transferAccept.At(index), "接收").Small().Clicked() {
						submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: entry.transfer.ID})
					}
				}
			})
		}
	})
	f.Divider().FillWidth()
}

func panelTransferName(entry panelTransferEntry) string {
	if strings.TrimSpace(entry.transfer.Name) != "" {
		return entry.transfer.Name
	}
	return "未命名文件"
}

func panelTransferActive(status string) bool {
	switch status {
	case "transferring", "verifying", "waiting":
		return true
	default:
		return false
	}
}

func (p *Panels) saveTransferAs(canvas *ui.Canvas, submit func(domain.Command), transfer domain.Transfer) {
	canvas.AfterPaint(func() {
		path, err := app.SaveFile(app.SaveFileOptions{
			Title:         "选择接收文件位置",
			SuggestedName: transfer.Name,
		})
		switch {
		case errors.Is(err, app.ErrCanceled):
			p.localError = ""
			return
		case err != nil:
			p.localError = "无法选择保存位置：" + err.Error()
			return
		case strings.TrimSpace(path) == "":
			return
		}
		p.localError = ""
		submit(domain.Command{
			Kind:       domain.AcceptTransfer,
			TransferID: transfer.ID,
			Paths:      []string{path},
		})
	})
}

func (p *Panels) drawShares(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	f.Column().Fill().PadSides(26, 32, 14, 32).Gap(14).Children(func() {
		panelPageHeader(f, theme, "文件", "整理自己分享的文件，并获取其他成员提供的只读文件", func() {
			f.Row().Gap(8).Items(flex.Center).Children(func() {
				if f.Button(p.ids.sharesRefresh, "刷新").Small().Outline().Clicked() {
					submit(domain.Command{Kind: domain.RefreshShares})
				}
				if f.Button(p.ids.sharesAdd, "分享文件…").Small().Clicked() {
					p.chooseShareFiles(f.Canvas(), submit)
				}
			})
		})
		p.panelFeedback(f, theme, state, submit, p.ids.sharesDismiss, p.ids.sharesClearError)

		f.Row().FillWidth().Items(flex.Center).Children(func() {
			choice := f.Choice(p.ids.sharesFilters, p.shareLabels, p.shareFilter).Width(420)
			if index, ok := choice.ChosenIndex(); ok {
				p.shareFilter = index
			}
			f.Spacer()
			f.Text("他人共享为只读").Muted().FontSize(theme.Metrics.FontSizeS)
		})

		f.ScrollArea(p.ids.sharesScroll).Grow(1).FillWidth().Children(func() {
			f.Column().FillWidth().PadSides(0, 0, 18, 0).Children(func() {
				entries := p.sharesAll
				switch p.shareFilter {
				case panelSharesMine:
					entries = p.sharesMine
				case panelSharesRemote:
					entries = p.sharesRemote
				}
				if len(entries) == 0 {
					panelEmpty(f, theme, "暂无共享文件", "分享本机文件后，可在这里管理分享；他人共享的文件也会出现在这里。")
					return
				}
				for index := range entries {
					p.drawShareRow(f, theme, index, entries[index], submit)
				}
			})
		})
	})
}

func (p *Panels) drawShareRow(f *flex.Frame, theme ui.Theme, index int, entry panelShareEntry, submit func(domain.Command)) {
	f.Column().FillWidth().PadY(15).Gap(9).Children(func() {
		f.Row().FillWidth().Gap(12).Items(flex.Start).Children(func() {
			f.Row().Width(38).Height(38).Radius(9).Background(theme.Muted).Items(flex.Center).
				Justify(flex.Center).Children(func() {
				f.Icon(panelIconFile).Size(18, 18).Color(panelAccentText(theme))
			})
			f.Column().Grow(1).Gap(4).Children(func() {
				f.Text(panelShareName(entry.file)).FontSize(theme.Metrics.FontSizeM + 1).
					Weight(ui.WeightMedium).Ellipsis()
				f.Paragraph(entry.meta, 2).Muted().FontSize(theme.Metrics.FontSizeS)
				f.Text(entry.state).FontSize(theme.Metrics.FontSizeS).
					Color(panelStatusColor(theme, entry.state))
			})
			if entry.own {
				panelPill(f, theme, "我分享的", panelAccentText(theme))
			} else {
				panelPill(f, theme, "只读共享", theme.MutedForeground)
			}
		})

		if entry.path != "" {
			f.Row().FillWidth().Gap(7).Items(flex.Center).Children(func() {
				f.Icon(panelIconFolder).Size(14, 14).Color(theme.MutedForeground)
				f.Text(entry.path).FontSize(theme.Metrics.FontSizeS).Muted().Ellipsis().Grow(1)
			})
		}

		f.Row().FillWidth().Gap(8).Items(flex.Center).Children(func() {
			f.Spacer()
			if entry.path != "" {
				open := f.Button(p.ids.shareOpen.At(index), "打开").Small().Outline().
					Disabled(!entry.available)
				if open.Clicked() && entry.available {
					panelOpenLocal(f.Canvas(), p, entry.path, false)
				}
				reveal := f.Button(p.ids.shareReveal.At(index), "打开文件夹").Small().Outline().
					Disabled(!entry.available)
				if reveal.Clicked() && entry.available {
					panelOpenLocal(f.Canvas(), p, entry.path, true)
				}
			}
			if entry.own {
				if f.Button(p.ids.shareRemove.At(index), "移除分享").Small().DestructiveOutline().Clicked() {
					submit(domain.Command{Kind: domain.RemoveShare, FileID: entry.file.ID})
				}
			} else {
				if f.Button(p.ids.shareGet.At(index), "获取").Small().Clicked() {
					submit(domain.Command{Kind: domain.GetShared, FileID: entry.file.ID})
				}
			}
		})
	})
	f.Divider().FillWidth()
}

func panelShareName(file domain.File) string {
	if strings.TrimSpace(file.Name) != "" {
		return file.Name
	}
	return "未命名文件"
}

func (p *Panels) chooseShareFiles(canvas *ui.Canvas, submit func(domain.Command)) {
	canvas.AfterPaint(func() {
		paths, err := app.OpenFiles(app.OpenFileOptions{
			Title:         "选择要共享的文件",
			AllowMultiple: true,
		})
		switch {
		case errors.Is(err, app.ErrCanceled):
			p.localError = ""
			return
		case err != nil:
			p.localError = "无法选择文件：" + err.Error()
			return
		case len(paths) == 0:
			return
		}
		p.localError = ""
		submit(domain.Command{Kind: domain.ShareFiles, Paths: append([]string(nil), paths...)})
	})
}
