package desktop

import (
	"strings"
	"testing"

	"goim/internal/domain"
)

func TestContactMenuHidesAndRestoresOriginalGroup(t *testing.T) {
	state := shellMinimalState(t)
	state.ContactGroups = []string{"工程组"}
	state.Peers = []domain.Peer{{ID: "peer", Name: "小王", Remark: "王工", ContactGroup: "工程组", Online: true}}
	h := newShellTestHarnessAtSize(t, state, 1000, 700)
	h.clickID(h.ui.ids.navContacts)
	row := h.ui.peerRowIDs["peer"]
	h.clickID(row)
	h.rightClickID(row)
	h.settle()
	if hit, ok := h.findHit(h.ui.ids.shellMenuDeleteContact); !ok || !hit.Disabled {
		t.Fatal("an online contact can be deleted")
	}
	h.clickID(h.ui.ids.shellMenuHideContact)
	if cmd := h.lastCommand(); cmd.Kind != domain.SetContactHidden || !cmd.Bool || cmd.PeerID != "peer" {
		t.Fatalf("hide command = %+v", cmd)
	}
	state.Peers[0].ContactHidden = true
	state.Revision++
	h.publish(state)
	if h.hasHit(row) || h.hasText("隐藏的联系人") || h.ui.contactPeer != "" || !h.hasText("全部  1") {
		t.Fatal("the default contacts view still lists, selects or counts the hidden peer")
	}
	h.typeInto(h.ui.ids.sidebarSearch, "小王")
	if h.hasHit(row) {
		t.Fatal("search in the regular contacts view revealed a hidden peer")
	}
	h.clickID(h.ui.ids.filterHidden)
	if !h.hasText("隐藏的联系人") || !h.hasHit(row) || !h.hasText("王工") || h.hasHit(h.ui.ids.selfRow) {
		t.Fatal("the hidden group did not show the matching contact")
	}
	requireShellLayout(t, h.snapshot())
	h.rightClickID(row)
	h.settle()
	if !h.hasText("移回原分组") {
		t.Fatal("hidden contact has no restore action")
	}
	h.clickID(h.ui.ids.shellMenuHideContact)
	if cmd := h.lastCommand(); cmd.Kind != domain.SetContactHidden || cmd.Bool || cmd.PeerID != "peer" {
		t.Fatalf("restore command = %+v", cmd)
	}
	state.Peers[0].ContactHidden = false
	state.Revision++
	h.publish(state)
	if !h.hasHit(row) || !h.hasText("工程组") || h.hasHit(h.ui.ids.filterHidden) || !h.hasText("全部  2") {
		t.Fatal("restoring the last hidden contact did not return to the normal group")
	}
}

func TestDeleteContactMenuTracksPresenceWhileOpen(t *testing.T) {
	state := shellMinimalState(t)
	state.Peers = []domain.Peer{{ID: "peer", Name: "小王", Online: false}}
	h := newShellTestHarness(t, state)
	h.clickID(h.ui.ids.navContacts)
	row := h.ui.peerRowIDs["peer"]
	h.rightClickID(row)
	h.settle()
	for _, online := range []bool{true, false} {
		state.Peers[0].Online = online
		state.Revision++
		h.publish(state)
		if hit, ok := h.findHit(h.ui.ids.shellMenuDeleteContact); !ok || hit.Disabled != online {
			t.Fatalf("delete availability did not track online=%v", online)
		}
	}
	h.clickID(h.ui.ids.shellMenuDeleteContact)
	h.settle()
	if !h.hasHit(h.ui.ids.confirmRun) || !strings.Contains(h.ui.confirm.body, "聊天记录会保留") {
		t.Fatalf("delete confirmation = %+v", h.ui.confirm)
	}
	h.clickID(h.ui.ids.confirmRun)
	if cmd := h.lastCommand(); cmd.Kind != domain.DeleteContact || cmd.PeerID != "peer" {
		t.Fatalf("delete command = %+v", cmd)
	}
	state.Peers[0].ContactDeleted = true
	state.Revision++
	h.publish(state)
	if h.hasHit(row) || h.hasHit(h.ui.ids.filterHidden) || !h.hasText("全部  1") {
		t.Fatal("a deleted contact is still listed or counted")
	}
}

func TestHiddenContactRecoveryIncludesOfflineAndExcludesCompatibilityAliases(t *testing.T) {
	state := shellMinimalState(t)
	state.Settings.HideOfflineContacts = true
	state.Peers = []domain.Peer{
		{ID: "offline", Name: "小王", ContactHidden: true},
		{ID: "alias", Name: "旧账号", CompatibilityAliasOf: "offline", ContactHidden: true},
		{ID: "deleted", Name: "旧联系人", ContactDeleted: true},
	}
	h := newShellTestHarness(t, state)
	h.clickID(h.ui.ids.navContacts)
	h.clickID(h.ui.ids.filterHidden)
	if !h.hasHit(h.ui.peerRowIDs["offline"]) || h.hasHit(h.ui.peerRowIDs["alias"]) || h.hasHit(h.ui.peerRowIDs["deleted"]) {
		t.Fatal("hidden group lost an offline contact or included a suppressed identity")
	}
	h.clickID(h.ui.peerRowIDs["offline"])
	h.clickID(h.ui.ids.filterAll)
	if h.ui.contactPeer != "" || h.hasText("小王") {
		t.Fatal("the hidden contact's profile remained visible in the regular list")
	}
}

func TestHiddenContactFiltersFitLargestEnglishFont(t *testing.T) {
	state := shellMinimalState(t)
	state.Settings.InterfaceFontSize = domain.InterfaceFontSizeMaximum
	state.Settings.Language = "en"
	state.Peers = []domain.Peer{{ID: "offline", Name: "Alice", ContactHidden: true}}
	h := newShellTestHarnessAtSize(t, state, 1000, 700)
	h.clickID(h.ui.ids.navContacts)
	h.clickID(h.ui.ids.filterHidden)
	if !h.hasText("Hidden  1") || !h.hasText("Online  1") || !h.hasText("All  1") {
		t.Fatal("the contact filters did not show their full counts")
	}
	requireShellLayout(t, h.snapshot())
	h.rightClickID(h.ui.peerRowIDs["offline"])
	h.settle()
	if !h.hasText("Restore to original group") {
		t.Fatal("the English restore action was truncated")
	}
	requireShellLayout(t, h.snapshot())
}
