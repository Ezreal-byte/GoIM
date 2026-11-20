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
	page     shareclient.Page
	download bool
	path     string
	err      error
}
type memberProjectView struct {
	id, address, title string
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
	if v := p.memberProject; v != nil {
		v.cancel()
		v.client.Close()
		v.password.SetText("")
		p.memberProject = nil
	}
}
func (p *Panels) openMemberProject(file domain.File) {
	p.closeMemberProject()
	client, err := shareclient.New(file.MemberURL)
	if err != nil {
		p.setShareToast("无法打开共享页面", i18n.T(err.Error()), components.ToastError)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	v := &memberProjectView{id: file.ID, address: file.MemberURL, title: file.Name, ctx: ctx, cancel: cancel, client: client, results: make(chan memberProjectResult, 1), invalidate: p.invalidate}
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
func (p *Panels) drawMemberProject(f ui.C, theme ui.Theme, bounds ui.Rect, dest string) {
	v := p.memberProject
	select {
	case result := <-v.results:
		v.busy = false
		v.status = ""
		if result.err != nil {
			v.problem = i18n.T(result.err.Error())
		}
		if result.page.Path != "" {
			v.page = result.page
		}
		if result.download && result.err == nil {
			p.setShareToast("下载完成", result.path, components.ToastSuccess)
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
		ui.Scroll(f.Key(v.page.Path)).FillWidth().Grow(1).Scrollbars().Children(func() {
			for _, e := range v.page.Items {
				row := f.Key(e.URL)
				ui.Row(row).FillWidth().Height(44).Items(ui.Center).Gap(10).Children(func() {
					drawFileIcon(row, theme, fileTypeIcon(domain.File{Name: e.Name, Folder: e.Folder}), 28, 20)
					ui.Text(row, e.Name).MinWidth(0).Grow(1).Ellipsis()
					ui.Text(row, e.Size).Muted().FontSize(12)
					label := "下载"
					if e.Folder {
						label = "打开"
					}
					if ui.Button(row.Part("action"), i18n.T(label)).Small().Ghost().Disabled(v.busy).Clicked() {
						if e.Folder {
							v.list(e.URL, "")
						} else {
							v.run("正在下载…", func() memberProjectResult {
								path, err := v.client.Download(v.ctx, e, dest)
								return memberProjectResult{download: true, path: path, err: err}
							})
						}
					}
				})
			}
		})
	})
}
