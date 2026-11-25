package desktop

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/shareclient"
)

func TestUnreadTrayLinksAndPrivacy(t *testing.T) {
	s := shellTestState(t)
	s.Conversations[1].Unread = 3
	s.Peers[0].Remark = "研发同事"
	h := newShellTestHarness(t, s)
	r := desktopRuntime{ui: h.ui, trayPanelHover: true}
	h.ui.section = sectionSettings
	items := r.unreadTrayItems()
	if len(items) != 4 || items[0].ID != "goim-unread-panel" || !strings.Contains(items[1].Title, "产品讨论组 · 研发同事") || !strings.Contains(items[2].Title, "研发同事（3 条未读）") {
		t.Fatal(items)
	}
	items = items[1:]
	items[1].OnSelect()
	if h.ui.selected != "conv-alice" || h.ui.section != sectionMessages {
		t.Fatal("wrong tray target")
	}
	h.ui.state.Conversations[0].Muted = true
	if len(r.unreadTrayItems()) != 3 {
		t.Fatal("muted group still shown")
	}
	items[0].OnSelect()
	h.ui.state.Conversations[0].Muted = false
	h.ui.state.Conversations[0].Hidden = true
	items[0].OnSelect()
	h.ui.state.Conversations[0].Hidden = false
	h.ui.state.Conversations[0].Unread = 0
	items[0].OnSelect()
	if h.ui.selected != "conv-alice" {
		t.Fatal("stale tray item reopened a muted, hidden or read conversation")
	}
	h.ui.state.Conversations[0].Unread = 1
	h.ui.confirm.open = true
	items[0].OnSelect()
	if h.ui.selected != "conv-alice" || !h.ui.confirm.open {
		t.Fatal("tray navigation changed the conversation behind a dialog")
	}
	h.ui.confirm.open = false
	h.ui.state.Settings.HideNotificationText = true
	privateItems := r.unreadTrayItems()[1:]
	if len(privateItems) != 3 || privateItems[0].Title != "新消息（1 条未读）" || privateItems[1].Title != "新消息（3 条未读）" {
		t.Fatal("privacy mode must keep anonymous unread actions", privateItems)
	}
	privateItems[0].OnSelect()
	if h.ui.selected != s.Conversations[0].ID {
		t.Fatal("explicit private notification activation did not open its chat")
	}
	h.ui.selected = "conv-alice"
	h.ui.state.Settings.HideNotificationText = false
	h.ui.state.Locked = true
	items[0].OnSelect()
	if h.ui.selected != "conv-alice" || len(r.unreadTrayItems()) != 4 {
		t.Fatal("stale tray action bypassed lock")
	}
}

func TestChatFileActionCoalescesWhileTheSystemOpenerIsPending(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	var jobs []mediaJob
	h.ui.panels.mediaSubmit = func(job mediaJob) { jobs = append(jobs, job) }
	path := "example.pdf"
	h.ui.openChatFile(path, false)
	h.ui.openChatFile(path, false)
	if len(jobs) != 1 || !h.ui.panels.localActionPending(path, false) {
		t.Fatalf("opening the same file queued %d requests", len(jobs))
	}
	h.ui.openChatFile(path, true)
	if len(jobs) != 2 || !jobs[1].reveal {
		t.Fatal("revealing the file must remain a separate action")
	}
	h.ui.panels.applyMediaActions([]mediaActionResult{{id: jobs[0].actionID, path: path, available: true}})
	h.ui.openChatFile(path, false)
	if len(jobs) != 3 || jobs[2].actionID == jobs[0].actionID {
		t.Fatal("opening the file again after completion must work")
	}
}

func TestMemberFolderToolbarOrderAndSelection(t *testing.T) {
	s := panelTestDefaultState(t)
	s.Peers = []domain.Peer{{ID: "owner", Online: true, Name: "Owner"}}
	file := domain.File{ID: "project", SenderID: "owner", Name: "资料包", ShareFolder: true, Shared: true, MemberURL: "http://127.0.0.1:1234/m/11111111111111111111111111111111"}
	s.Files = []domain.File{file}
	h := panelTestNewHarnessBounds(t, panelModuleShares, s, ui.Rect{Width: 1250, Height: 740})
	v := &memberProjectView{id: file.ID, address: file.MemberURL, location: file.Name, checked: map[string]bool{}, page: shareclient.Page{Title: file.Name, Path: "/s/test", Items: []shareclient.Entry{{Name: "a.pdf", URL: "/s/test/download?file=1"}}}}
	h.panels.memberProject = v
	h.publish(s)
	back, ok := h.findHitLabelRole("返回", gfx.RoleButton)
	if !ok {
		t.Fatal("Back missing")
	}
	path, ok := h.findHit(h.panels.ids.shareLocation)
	if !ok || path.Bounds.X <= back.Bounds.X {
		t.Fatal("path must follow Back")
	}
	last := path.Bounds.X
	for _, title := range []string{"全选", downloadSelectedLabel(0), "下载整个文件夹", "刷新"} {
		hit, ok := h.findHitLabelRole(title, gfx.RoleButton)
		if !ok || hit.Bounds.X <= last || hit.Bounds.Y != back.Bounds.Y {
			t.Fatalf("toolbar %q %+v after %v", title, hit, last)
		}
		last = hit.Bounds.X
	}
	h.clickLabelRole("全选", gfx.RoleButton)
	if hit, ok := h.findHitLabelRole(downloadSelectedLabel(1), gfx.RoleButton); !ok || hit.Disabled {
		t.Fatal("selection didn't enable batch download")
	}
}

func TestPrintRecordCancelButtonHonorsPhase(t *testing.T) {
	s := printTestState(t)
	s.Peers[0].PrintCancelSupported = true
	h := panelTestNewHarness(t, panelModulePrint, s)
	h.panels.print.tab = printTabRecords
	h.publish(s)
	h.clickLabelRole("取消打印", gfx.RoleButton)
	if c := h.requireLastCommand(); c.Kind != domain.CancelPrintJob || c.PrintJobID != s.PrintJobs[0].ID {
		t.Fatal(c)
	}
	s.PrintJobs[0].Status = domain.PrintStatusSubmitting
	s.Revision++
	h.publish(s)
	if hit, ok := h.findHitLabelRole("取消打印", gfx.RoleButton); !ok || !hit.Disabled {
		t.Fatal("submission is not cancellable")
	}
	s.PrintJobs[0].Status = domain.PrintStatusCancelled
	s.Revision++
	h.publish(s)
	if _, ok := h.findHitLabelRole("取消打印", gfx.RoleButton); ok || !h.hasText("已取消") {
		t.Fatal("cancelled record still actionable")
	}
}
