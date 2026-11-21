package desktop

import (
	"context"
	"path"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/i18n"
	"goim/internal/shareclient"
)

type memberProjectResult struct {
	page     shareclient.Page
	location string
	err      error
}
type memberProjectView struct {
	sort                  panelTableSort
	id, address, location string
	version               uint64
	checked               map[string]bool
	batchUnlock           bool
	ctx                   context.Context
	cancel                context.CancelFunc
	client                *shareclient.Client
	results               chan memberProjectResult
	invalidate            func()
	page                  shareclient.Page
	password              ui.Field
	busy                  bool
	status, problem       string
}

func (p *Panels) closeMemberProject() {
	p.shareBatch = nil
	p.shareUnlock = nil
	p.dismissMemberProject()
}
func (p *Panels) backMemberProject() {
	v := p.memberProject
	if v.page.ParentURL == "" {
		p.closeMemberProject()
	} else {
		v.list(v.page.ParentURL, "", path.Dir(v.location))
	}
}

func (p *Panels) dismissMemberProject() {
	if v := p.memberProject; v != nil {
		v.cancel()
		v.client.Close()
		v.password.SetText("")
		p.memberProject = nil
	}
}
func (p *Panels) openMemberProject(file domain.File) {
	p.dismissMemberProject()
	client, err := shareclient.New(file.MemberURL)
	if err != nil {
		p.setShareToast("无法打开共享页面", i18n.T(err.Error()), components.ToastError)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	v := &memberProjectView{id: file.ID, version: file.ShareVersion, address: file.MemberURL, ctx: ctx, cancel: cancel, client: client, results: make(chan memberProjectResult, 1), invalidate: p.invalidate, checked: make(map[string]bool)}
	v.password.Secure = true
	p.memberProject = v
	p.shareMenu = shareMenuState{}
	v.list("", "", "")
}
func (v *memberProjectView) run(status string, work func() memberProjectResult) {
	if v.busy {
		return
	}
	v.busy = true
	v.problem = ""
	v.status = status
	go func() {
		result := work()
		select {
		case <-v.ctx.Done():
		case v.results <- result:
			if v.invalidate != nil {
				v.invalidate()
			}
		}
	}()
}
func (v *memberProjectView) list(ref, password, location string) {
	v.run("正在读取共享目录…", func() memberProjectResult {
		page, err := v.client.List(v.ctx, ref, password)
		return memberProjectResult{page: page, location: location, err: err}
	})
}
func (v *memberProjectView) refresh() {
	ref, location, subdir := v.page.Path, v.location, v.page.ParentURL != ""
	v.run("正在读取共享目录…", func() memberProjectResult {
		page, err := v.client.List(v.ctx, ref, "")
		if err != nil && subdir && v.ctx.Err() == nil {
			page, err = v.client.List(v.ctx, "", "")
		}
		return memberProjectResult{page: page, location: location, err: err}
	})
}

func (p *Panels) drawMemberProject(f ui.C, theme ui.Theme, width float32, entry panelShareEntry, submit func(domain.Command)) {
	v := p.memberProject
	select {
	case result := <-v.results:
		v.busy = false
		v.status = ""
		if result.err != nil {
			v.problem = i18n.T(result.err.Error())
		}
		if result.page.Path != "" {
			if result.page.Path != v.page.Path {
				clear(v.checked)
			}
			if len(v.checked) > 0 {
				selected := make(map[string]bool)
				for _, e := range result.page.Items {
					if v.checked[e.URL] {
						selected[e.URL] = true
					}
				}
				v.checked = selected
			}
			v.page = result.page
			v.sortItems()
			v.location = result.location
			if v.page.ParentURL == "" {
				v.location = v.page.Title
			} else if v.page.Current != "" {
				v.location = path.Join(path.Dir(v.location), v.page.Current)
			}
		}
		if v.batchUnlock && result.err == nil && result.page.Path != "" && !result.page.Locked {
			p.shareBatch = append(p.shareBatch, v.selection(result.page.Whole()))
			p.continueShareBatch(submit)
			if p.invalidate != nil {
				p.invalidate()
			}
			return
		}
	default:
	}
	if !v.busy && v.version != entry.file.ShareVersion {
		v.version = entry.file.ShareVersion
		if v.page.Path != "" {
			v.refresh()
		}
	}
	location := v.location
	if location == "" {
		location = entry.title
	}
	ui.Column(f.Part("member-folder")).FillWidth().Grow(1).MinHeight(0).PadSides(8, 0, 0, 0).Gap(8).Children(func() {
		ui.Row(f).FillWidth().Items(ui.Center).Gap(12).Children(func() {
			if ui.Button(f.Part("back-projects"), i18n.T("返回")).Icon(viewerBackIcon).Ghost().Disabled(v.busy && v.page.ParentURL != "").Clicked() {
				p.backMemberProject()
			}
			ui.Text(f, location).MinWidth(0).Grow(1).TextEnd().Ellipsis().Weight(gfx.WeightMedium).
				HitWith(p.ids.shareLocation, location, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true}).Tooltip(location)
		})
		if v.problem != "" {
			ui.Wrap(f, v.problem).Color(theme.Danger)
		}
		if v.busy {
			ui.Text(f, i18n.T(v.status)).Muted()
		}
		if !v.busy && v.page.Path == "" && v.problem != "" && ui.Button(f.Part("refresh-project"), i18n.T("刷新")).Outline().Clicked() {
			v.refresh()
		}
		if v.page.Locked {
			field := ui.TextFieldID(f, p.ids.sharePasswordField, &v.password, i18n.T("分享密码"), i18n.T("输入分享密码")).Width(min(float32(320), width))
			submit := ui.Button(f.Part("unlock-project"), i18n.T("打开共享")).Disabled(v.busy || v.password.Text() == "")
			if !v.busy && (submit.Clicked() || field.Submitted()) && v.password.Text() != "" {
				pass := v.password.Text()
				v.password.SetText("")
				v.list(v.page.Path, pass, v.location)
			}
			return
		}
		if v.page.Path != "" && len(v.page.Items) == 0 && !v.busy {
			ui.Text(f, i18n.T("这个文件夹是空的")).Muted()
		}
		if v.page.Path != "" {
			ui.Row(f.Part("project-downloads")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				if ui.Button(f.Part("reload-directory"), i18n.T("刷新")).Ghost().Disabled(v.busy).Clicked() {
					v.refresh()
				}
				whole := v.page.Whole()
				if whole.Folder && ui.Button(f.Part("download-folder"), i18n.T("下载整个文件夹")).Outline().Disabled(v.busy).Clicked() {
					p.submitShareBatch([]domain.ShareDownload{v.selection(whole)}, submit)
					return
				}
				if len(v.page.Items) == 0 {
					return
				}
				if ui.Button(f.Part("select-all"), shareSelectAllLabel(len(v.checked) == len(v.page.Items))).Ghost().Disabled(v.busy).Clicked() {
					all := len(v.checked) != len(v.page.Items)
					clear(v.checked)
					if all {
						for _, e := range v.page.Items {
							v.checked[e.URL] = true
						}
					}
				}
				if ui.Button(f.Part("download-selected"), downloadSelectedLabel(len(v.checked))).Outline().Disabled(v.busy || len(v.checked) == 0).Clicked() {
					var batch []domain.ShareDownload
					for _, e := range v.page.Items {
						if v.checked[e.URL] {
							batch = append(batch, v.selection(e))
						}
					}
					p.submitShareBatch(batch, submit)
				}
			})
		}
		sizeWidth, actionsWidth := float32(84), theme.Metrics.ScaledText(148)
		if len(v.page.Items) > 0 {
			ui.Row(f).FillWidth().Height(panelTableHeadHeight).Gap(10).Items(ui.Center).Children(func() {
				v.sort.header(f, "名称", 0, v.sortItems)
				v.sort.header(f, "大小", sizeWidth, v.sortItems)
				ui.Row(f).Width(actionsWidth)
			})
		}
		ui.Scroll(f.Key(v.page.Path)).FillWidth().Grow(1).AlwaysScrollbars().Children(func() {
			for _, e := range v.page.Items {
				row := f.Key(e.URL)
				ui.Row(row).FillWidth().Height(44).Items(ui.Center).Gap(10).Children(func() {
					if selectionCheckbox(row, v.checked[e.URL], e.Name, v.busy) {
						toggleShareSelection(v.checked, e.URL)
					}
					drawFileIcon(row, theme, fileTypeIcon(domain.File{Name: e.Name, Folder: e.Folder}), 28, 20)
					ui.Text(row, e.Name).MinWidth(0).Grow(1).Ellipsis()
					ui.Text(row, e.Size).Width(sizeWidth).Muted().FontSize(textSize(f, 12)).Ellipsis()
					ui.Row(row).Width(actionsWidth).Gap(4).Justify(ui.End).Items(ui.Center).Children(func() {
						if e.Folder && ui.Button(row.Part("open"), i18n.T("打开")).Small().Ghost().Disabled(v.busy).Clicked() {
							v.list(e.URL, "", path.Join(location, e.Name))
						}
						if ui.Button(row.Part("download"), i18n.T("下载")).Small().Ghost().Disabled(v.busy).Clicked() {
							p.submitShareBatch([]domain.ShareDownload{v.selection(e)}, submit)
						}
					})
				})
			}
		})
	})
}
