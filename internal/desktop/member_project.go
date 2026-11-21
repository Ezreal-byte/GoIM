package desktop

import (
	"context"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/i18n"
	"goim/internal/shareclient"
)

type memberProjectResult struct {
	page shareclient.Page
	err  error
}
type memberProjectView struct {
	id, address, title string
	checked            map[string]bool
	batchUnlock        bool
	ctx                context.Context
	cancel             context.CancelFunc
	client             *shareclient.Client
	results            chan memberProjectResult
	invalidate         func()
	page               shareclient.Page
	password           ui.Field
	busy               bool
	status, problem    string
}

func (p *Panels) closeMemberProject() {
	p.shareBatch = nil
	p.shareUnlock = nil
	p.dismissMemberProject()
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
	v := &memberProjectView{id: file.ID, address: file.MemberURL, title: file.Name, ctx: ctx, cancel: cancel, client: client, results: make(chan memberProjectResult, 1), invalidate: p.invalidate, checked: make(map[string]bool)}
	v.password.Secure = true
	p.memberProject = v
	p.shareMenu = shareMenuState{}
	v.list("", "")
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
func (v *memberProjectView) list(ref, password string) {
	v.run("正在读取共享目录…", func() memberProjectResult {
		page, err := v.client.List(v.ctx, ref, password)
		return memberProjectResult{page: page, err: err}
	})
}
func (p *Panels) drawMemberProject(f ui.C, theme ui.Theme, bounds ui.Rect, submit func(domain.Command)) {
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
			v.page = result.page
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
	pad := panelPageInset(bounds.Width)
	ui.Column(f).Fill().PadSides(panelPageTop, pad, 18, pad).Gap(10).Children(func() {
		if ui.Button(f.Part("back-projects"), i18n.T("返回成员共享")).Ghost().Clicked() {
			p.closeMemberProject()
			return
		}
		title := v.page.Current
		if title == "" || v.page.ParentURL == "" {
			title = v.page.Title
		}
		if title == "" {
			title = v.title
		}
		ui.Text(f, title).FontSize(21).Weight(gfx.WeightSemibold).Ellipsis()
		ui.Divider(f)
		if v.problem != "" {
			ui.Wrap(f, v.problem).Color(theme.Danger)
		}
		if v.busy {
			ui.Text(f, i18n.T(v.status)).Muted()
		}
		if v.page.Locked {
			field := ui.TextFieldID(f, p.ids.sharePasswordField, &v.password, i18n.T("分享密码"), i18n.T("输入分享密码")).Width(min(float32(320), bounds.Width-2*pad))
			submit := ui.Button(f.Part("unlock-project"), i18n.T("打开共享")).Disabled(v.busy || v.password.Text() == "")
			if !v.busy && (submit.Clicked() || field.Submitted()) && v.password.Text() != "" {
				pass := v.password.Text()
				v.password.SetText("")
				v.list(v.page.Path, pass)
			}
			return
		}
		if v.problem != "" && !v.busy && ui.Button(f.Part("refresh-project"), i18n.T("刷新")).Outline().Clicked() {
			v.list(v.page.Path, "")
		}
		if v.page.ParentURL != "" && ui.Button(f.Part("up-project"), i18n.T("返回上一级")).Ghost().Disabled(v.busy).Clicked() {
			v.list(v.page.ParentURL, "")
		}
		if v.page.Path != "" && len(v.page.Items) == 0 && !v.busy {
			ui.Text(f, i18n.T("这个文件夹是空的")).Muted()
		}
		if v.page.Path != "" {
			ui.Row(f.Part("project-downloads")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
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
		ui.Scroll(f.Key(v.page.Path)).FillWidth().Grow(1).Scrollbars().Children(func() {
			for _, e := range v.page.Items {
				row := f.Key(e.URL)
				ui.Row(row).FillWidth().Height(44).Items(ui.Center).Gap(10).Children(func() {
					if selectionCheckbox(row, v.checked[e.URL], e.Name, v.busy) {
						toggleShareSelection(v.checked, e.URL)
					}
					drawFileIcon(row, theme, fileTypeIcon(domain.File{Name: e.Name, Folder: e.Folder}), 28, 20)
					ui.Text(row, e.Name).MinWidth(0).Grow(1).Ellipsis()
					ui.Text(row, e.Size).Muted().FontSize(12)
					if e.Folder && ui.Button(row.Part("open"), i18n.T("打开")).Small().Ghost().Disabled(v.busy).Clicked() {
						v.list(e.URL, "")
					}
					if ui.Button(row.Part("download"), i18n.T("下载")).Small().Ghost().Disabled(v.busy).Clicked() {
						p.submitShareBatch([]domain.ShareDownload{v.selection(e)}, submit)
					}
				})
			}
		})
	})
}
