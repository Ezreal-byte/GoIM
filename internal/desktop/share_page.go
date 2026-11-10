package desktop

import (
	"fmt"
	"goim/internal/i18n"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

type shareSendDialogState struct {
	open     bool
	folder   bool
	paths    []string
	password flex.Field
}

type sharePasswordDialogState struct {
	open      bool
	fileID    string
	protected bool
	field     flex.Field
}

type shareQRDialogState struct {
	open    bool
	fileID  string
	name    string
	url     string
	key     shareQRKey
	hasKey  bool
	bitmap  ui.Bitmap
	loading bool
	err     string
}

func (p *Panels) closeShareInteractions() {
	p.shareMenu = shareMenuState{}
	p.shareDialog = shareSendDialogState{}
	p.sharePassword = sharePasswordDialogState{}
	p.shareQR = shareQRDialogState{}
}

func (p *Panels) shareModalOpen() bool {
	return p.shareDialog.open || p.sharePassword.open || p.shareQR.open
}

func (p *Panels) applyShareBrowserEvents() {
	for _, result := range p.shareQRWorker.take() {
		if len(p.shareQRCache) >= 64 {
			for key := range p.shareQRCache {
				if key != result.key {
					delete(p.shareQRCache, key)
					break
				}
			}
		}
		p.shareQRCache[result.key] = result
		if p.shareQR.open && p.shareQR.hasKey && p.shareQR.key == result.key {
			p.shareQR.loading = false
			p.shareQR.bitmap = result.bitmap
			if !result.ok {
				p.shareQR.err = i18n.T("二维码图片无法读取")
			} else {
				p.shareQR.err = ""
			}
		}
	}
}

func panelShareName(file domain.File) string {
	if strings.TrimSpace(file.Name) != "" {
		return file.Name
	}
	return i18n.T("未命名文件")
}

func sharePasswordLabel(shared, protected bool) string {
	if !shared {
		return i18n.T("设置访问密码…")
	}
	if protected {
		return i18n.T("更改访问密码…")
	}
	return i18n.T("设置访问密码…")
}

func (p *Panels) openShareDialogFor(paths []string, folder bool) {
	if len(paths) == 0 {
		return
	}
	p.closeShareInteractions()
	p.shareDialog.open = true
	p.shareDialog.folder = folder
	p.shareDialog.paths = paths
	p.shareDialog.password.Secure = true
	p.shareDialog.password.SetText("")
}

func (p *Panels) closeShareSendDialog() {
	p.shareDialog.open = false
	p.shareDialog.folder = false
	p.shareDialog.paths = nil
	p.shareDialog.password.SetText("")
}

func (p *Panels) submitShareSendDialog(submit func(domain.Command)) {
	if !p.shareDialog.open || len(p.shareDialog.paths) == 0 {
		return
	}
	paths := append([]string(nil), p.shareDialog.paths...)
	password := p.shareDialog.password.Text()
	p.closeShareSendDialog()
	submit(domain.Command{Kind: domain.ShareFiles, Paths: paths, Text: password})
}

func (p *Panels) openSharePassword(file domain.File) {
	if strings.TrimSpace(file.ID) == "" {
		return
	}
	p.closeShareInteractions()
	p.sharePassword.open = true
	p.sharePassword.fileID = file.ID
	p.sharePassword.protected = file.ShareProtected
	p.sharePassword.field.Secure = true
	p.sharePassword.field.SetText("")
}

func (p *Panels) closeSharePassword() {
	p.sharePassword = sharePasswordDialogState{}
}

func (p *Panels) submitSharePassword(submit func(domain.Command)) {
	if !p.sharePassword.open || p.sharePassword.fileID == "" {
		return
	}
	fileID := p.sharePassword.fileID
	password := p.sharePassword.field.Text()
	p.closeSharePassword()
	submit(domain.Command{Kind: domain.SetSharePassword, FileID: fileID, Text: password})
}

func (p *Panels) openShareQR(file domain.File) {
	p.closeShareInteractions()
	p.shareQR = shareQRDialogState{
		open: true, fileID: file.ID, name: panelShareName(file), url: file.ShareURL,
	}
	key, ok := shareQRKeyFor(file)
	if !ok {
		p.shareQR.err = i18n.T("此分享未提供二维码")
		return
	}
	p.shareQR.key = key
	p.shareQR.hasKey = true
	if cached, exists := p.shareQRCache[key]; exists {
		p.shareQR.bitmap = cached.bitmap
		if !cached.ok {
			p.shareQR.err = i18n.T("二维码图片无法读取")
		}
		return
	}
	p.shareQR.loading = true
	p.shareQRWorker.startQR(key, append([]byte(nil), file.ShareQR...))
}

func (p *Panels) closeShareQR() {
	p.shareQR = shareQRDialogState{}
}

func (p *Panels) drawShareDialogs(f *flex.Frame, theme ui.Theme, submit func(domain.Command)) {
	if p.shareDialog.open {
		p.drawShareSendDialog(f, theme, submit)
	}
	if p.sharePassword.open {
		p.drawSharePasswordDialog(f, theme, submit)
	}
	if p.shareQR.open {
		p.drawShareQRDialog(f, theme)
	}
}

func (p *Panels) drawShareSendDialog(f *flex.Frame, theme ui.Theme, submit func(domain.Command)) {
	count := len(p.shareDialog.paths)
	title := i18n.T("分享所选文件")
	description := fmt.Sprintf(i18n.T("将通过 GoIM 共享 %d 个文件；访问者使用链接或二维码下载。"), count)
	if p.shareDialog.folder {
		title = i18n.T("分享文件夹")
		description = i18n.T("访问者可浏览此文件夹的现有内容。新增文件和符号链接不会加入分享。")
	}
	dialog := f.Dialog(flex.DialogSpec{
		Title: title, Description: description, Width: 460,
		Scope: p.ids.shareDialogScope, DismissID: p.ids.shareDialogDismiss,
		CloseID: p.ids.shareDialogClose, InitialFocusID: p.ids.shareDialogPassword,
	})
	var submitted bool
	dialog.Body(func() {
		f.Column().Fill().Gap(8).Children(func() {
			names := make([]string, 0, min(count, 3))
			for _, path := range p.shareDialog.paths {
				if len(names) == 3 {
					break
				}
				names = append(names, filepath.Base(path))
			}
			if count > 3 {
				names = append(names, fmt.Sprintf(i18n.T("等 %d 个文件"), count))
			}
			f.Text(strings.Join(names, "、")).FontSize(theme.Metrics.FontSizeS + 1).
				Weight(ui.WeightMedium).Ellipsis()
			field := f.Field(&p.shareDialog.password, p.ids.shareDialogPassword,
				i18n.T("访问密码（可选）"), i18n.T("留空表示无需密码")).FillWidth()
			submitted = field.Submitted()
			f.Text(i18n.T("不设密码时，持有链接的同网用户可下载。")).Muted().
				FontSize(theme.Metrics.FontSizeS)
		})
	})
	dialog.Footer(func() {
		if f.Button(p.ids.shareDialogCancel, i18n.T("取消")).Outline().Clicked() {
			p.closeShareSendDialog()
		}
		if f.Button(p.ids.shareDialogSubmit, i18n.T("创建分享")).Clicked() {
			submitted = true
		}
	})
	if dialog.Dismissed() {
		p.closeShareSendDialog()
		return
	}
	if submitted {
		p.submitShareSendDialog(submit)
	}
}

func (p *Panels) drawSharePasswordDialog(f *flex.Frame, theme ui.Theme, submit func(domain.Command)) {
	title := i18n.T("设置访问密码")
	if p.sharePassword.protected {
		title = i18n.T("更改访问密码")
	}
	dialog := f.Dialog(flex.DialogSpec{
		Title: title, Description: i18n.T("密码只影响这项分享；留空并保存会清除密码。"), Width: 440,
		Scope: p.ids.sharePasswordScope, DismissID: p.ids.sharePasswordDismiss,
		CloseID: p.ids.sharePasswordClose, InitialFocusID: p.ids.sharePasswordField,
	})
	var submitted bool
	dialog.Body(func() {
		f.Column().Fill().Gap(8).Children(func() {
			field := f.Field(&p.sharePassword.field, p.ids.sharePasswordField,
				i18n.T("访问密码"), i18n.T("留空清除密码")).FillWidth()
			submitted = field.Submitted()
			f.Text(i18n.T("访问者打开分享链接或扫描二维码后需要输入此密码。")).Muted().
				FontSize(theme.Metrics.FontSizeS)
		})
	})
	dialog.Footer(func() {
		if f.Button(p.ids.sharePasswordCancel, i18n.T("取消")).Outline().Clicked() {
			p.closeSharePassword()
		}
		if f.Button(p.ids.sharePasswordSubmit, i18n.T("保存")).Clicked() {
			submitted = true
		}
	})
	if dialog.Dismissed() {
		p.closeSharePassword()
		return
	}
	if submitted {
		p.submitSharePassword(submit)
	}
}

func (p *Panels) drawShareQRDialog(f *flex.Frame, theme ui.Theme) {
	dialog := f.Dialog(flex.DialogSpec{
		Title: i18n.T("分享二维码"), Description: p.shareQR.name, Width: 380,
		Scope: p.ids.shareQRScope, DismissID: p.ids.shareQRDismiss,
		CloseID: p.ids.shareQRClose,
	})
	dialog.Body(func() {
		f.Column().Fill().Gap(10).Items(flex.Center).Children(func() {
			switch {
			case p.shareQR.err != "":
				f.Column().FillWidth().PadY(28).Items(flex.Center).Children(func() {
					f.Paragraph(p.shareQR.err, 3).Muted().TextCenter().
						FontSize(theme.Metrics.FontSizeS + 1).MaxWidth(280)
				})
			case p.shareQR.loading:
				f.Row().Size(224, 224).Radius(10).Background(theme.Muted).
					Items(flex.Center).Justify(flex.Center).Children(func() {
					f.Spinner().Size(20, 20)
				})
			default:
				f.Row().Size(224, 224).Radius(10).Background(ui.RGB(255, 255, 255)).
					Items(flex.Center).Justify(flex.Center).Children(func() {
					f.Image(p.shareQR.bitmap).Size(208, 208)
				})
			}
			if p.shareQR.url != "" {
				f.Text(p.shareQR.url).Muted().FontSize(theme.Metrics.FontSizeS).
					Ellipsis().MaxWidth(300)
			}
			f.Text(i18n.T("手机与电脑连接同一局域网后，即可扫码打开。")).Muted().
				FontSize(theme.Metrics.FontSizeS).TextCenter()
		})
	})
	dialog.Footer(func() {
		if p.shareQR.url != "" {
			if f.Button(p.ids.shareQRCopy, i18n.T("复制链接")).Outline().Clicked() {
				p.copyShareLink(f.Canvas(), p.shareQR.url)
			}
		}
		if f.Button(p.ids.shareQRDismissButton, i18n.T("关闭")).Clicked() {
			p.closeShareQR()
		}
	})
	if dialog.Dismissed() {
		p.closeShareQR()
	}
}
