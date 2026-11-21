package desktop

import (
	"fmt"

	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/i18n"
	"goim/internal/shareclient"
)

func (e panelShareEntry) canDownload() bool {
	return !e.memberWebProject() || e.online && e.file.MemberURL != ""
}

func selectionCheckbox(f ui.C, selected bool, name string, disabled bool) bool {
	value := ui.State[bool](f, "checked")
	*value = selected
	return ui.Checkbox(f, value, "").Label(i18n.T("选择 ") + name).Disabled(disabled).Changed()
}
func toggleShareSelection(selected map[string]bool, key string) {
	if selected[key] {
		delete(selected, key)
	} else {
		selected[key] = true
	}
}
func shareSelectAllLabel(all bool) string {
	if all {
		return i18n.T("取消全选")
	}
	return i18n.T("全选")
}

func downloadSelectedLabel(count int) string {
	return fmt.Sprintf(i18n.T("下载所选（%d）"), count)
}
func (v *memberProjectView) selection(e shareclient.Entry) domain.ShareDownload {
	return domain.ShareDownload{ProjectID: v.id, Name: e.Name, Index: e.Index(), Folder: e.Folder, Session: v.client.Session()}
}
func (p *Panels) downloadProjects(files []domain.File, submit func(domain.Command)) {
	p.closeMemberProject()
	for _, f := range files {
		if f.ShareProtected {
			p.shareUnlock = append(p.shareUnlock, f)
		} else {
			p.shareBatch = append(p.shareBatch, domain.ShareDownload{ProjectID: f.ID, Name: f.Name, Folder: f.ShareFolder})
		}
	}
	p.continueShareBatch(submit)
}
func (p *Panels) continueShareBatch(submit func(domain.Command)) {
	if len(p.shareUnlock) > 0 {
		file := p.shareUnlock[0]
		p.shareUnlock = p.shareUnlock[1:]
		p.openMemberProject(file)
		if p.memberProject != nil {
			p.memberProject.batchUnlock = true
		} else {
			p.closeMemberProject()
		}
		return
	}
	batch := p.shareBatch
	p.submitShareBatch(batch, submit)
}
func (p *Panels) submitShareBatch(batch []domain.ShareDownload, submit func(domain.Command)) {
	if len(batch) == 0 {
		return
	}
	p.closeMemberProject()
	clear(p.shareChecked)
	p.transferFilter = panelTransferReceived
	p.transferSearch.SetText("")
	submit(domain.Command{Kind: domain.GetShared, FileID: batch[0].ProjectID, ShareDownloads: batch})
	if p.showIncoming != nil {
		p.showIncoming()
	}
}
