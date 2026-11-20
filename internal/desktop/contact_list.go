package desktop

import (
	"sort"
	"strings"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (u *UI) rebuildContactGroups() {
	groups := make(map[string][]domain.Peer)
	for _, name := range u.state.ContactGroups {
		groups[name] = nil
	}
	hidden := u.hiddenContacts.Peers[:0]
	for _, peer := range u.state.Peers {
		if peer.ID == u.state.Self.ID || !peer.ListedContact() {
			continue
		}
		if peer.ContactHidden {
			hidden = append(hidden, peer)
			continue
		}
		if u.state.Settings.HideOfflineContacts && !peer.VisibleOnline() {
			continue
		}
		name := strings.TrimSpace(peer.ContactGroup)
		if name == "" {
			name = "未分组"
		}
		groups[name] = append(groups[name], peer)
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.SliceStable(names, func(i, j int) bool {
		if (names[i] == "未分组") != (names[j] == "未分组") {
			return names[j] == "未分组"
		}
		return names[i] < names[j]
	})
	u.contacts = u.contacts[:0]
	for _, name := range names {
		u.contacts = append(u.contacts, makeContactGroup(name, groups[name]))
	}
	u.hiddenContacts = makeContactGroup("隐藏的联系人", hidden)
	if u.hiddenContacts.Total == 0 {
		u.hiddenCollapsed = false
		if u.sidebarFilter == "hidden" {
			u.sidebarFilter = "all"
		}
	}
	u.reconcileContactSelection()
}

func makeContactGroup(name string, peers []domain.Peer) contactGroupView {
	sort.SliceStable(peers, func(i, j int) bool {
		if peers[i].VisibleOnline() != peers[j].VisibleOnline() {
			return peers[i].VisibleOnline()
		}
		return peers[i].Name < peers[j].Name
	})
	group := contactGroupView{Name: name, Peers: peers, Total: len(peers)}
	for _, peer := range peers {
		if peer.VisibleOnline() {
			group.Online++
		}
	}
	return group
}

func (u *UI) reconcileContactSelection() {
	hidden := u.sidebarFilter == "hidden"
	if hidden {
		u.contactGroup = ""
	}
	if u.contactPeer != "" {
		peer, ok := u.state.LookupPeer(u.contactPeer)
		if !ok || !peer.ListedContact() || peer.ContactHidden != hidden {
			u.contactPeer = ""
		}
	}
}

func (u *UI) drawHiddenContacts(f ui.C, query string) {
	collapsed := u.hiddenCollapsed && query == ""
	u.contactGroupHeader(f, u.hiddenContacts, collapsed)
	if collapsed {
		return
	}
	peers := u.filteredPeers(u.hiddenContacts.Peers, query)
	for _, peer := range peers {
		u.contactRow(f, peer)
	}
	if len(peers) == 0 && query != "" {
		u.emptyListMessage(f, i18n.T("没有匹配的联系人"), i18n.T("换一个昵称或分组名称试试。"))
	}
}
