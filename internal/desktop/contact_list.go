package desktop

import (
	"sort"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (u *UI) rebuildContactGroups() {
	groups := map[string][]domain.Peer{"未分组": nil}
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

// Render in the shell so the preview follows the pointer beyond the list
// without intercepting hits on the group underneath it.
func (u *UI) drawContactDragGhost(canvas *gfx.Canvas, bounds ui.Rect) {
	if u.section != sectionContacts || !u.contactDrag.Dragging() || u.lockVisible() {
		return
	}
	for _, group := range u.contacts {
		for _, peer := range group.Peers {
			if u.peerRowIDs[peer.ID] != u.contactDrag.Source() {
				continue
			}
			at := u.contactDrag.At()
			card := ui.Rect{Width: min(conversationPreviewWidth-8, bounds.Width), Height: conversationPreviewHeight - 9}
			card.X = max(bounds.X, min(at.X+14, bounds.Right()-card.Width))
			card.Y = max(bounds.Y, min(at.Y+12, bounds.Bottom()-card.Height))
			canvas.PushLayer(65)
			u.paintConversationDragCard(canvas, card, conversationCardData{title: peer.DisplayName(), username: remarkUsername(peer), avatarName: peer.Name, avatarPath: peer.Avatar, online: peer.VisibleOnline(), preview: displayContactGroup(group.Name)}, false)
			canvas.PopLayer()
			return
		}
	}
}

func (u *UI) contactDropTarget(box ui.Box, id gfx.ID) {
	box.DropTarget(&u.contactDrag, u.acceptContactDrop, u.moveDroppedContact)
	if u.contactDrag.Target() == id {
		box.Background(u.palette.selected).Hover(ui.Look{}.Background(u.palette.selected))
	}
}

// Keep distant groups reachable without releasing the mouse. Recheck the
// target after scrolling, even if the pointer itself has stayed still.
func (u *UI) scrollDraggedContact(c *gfx.Canvas, list ui.Box) {
	at, bounds := u.contactDrag.At(), u.contactListBounds
	if !u.contactDrag.Dragging() || !pointInside(at.X, at.Y, bounds) {
		u.contactScrollTime = 0
		return
	}
	target := u.contactDrag.Target()
	u.sidebarPage.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: at.X, Y: at.Y})
	if target != u.contactDrag.Target() {
		c.RequestFrame()
	}
	scroll := list.ScrollState()
	edge := min(float32(32), bounds.Height/4)
	direction := float32(0)
	switch {
	case at.Y < bounds.Y+edge && scroll.Offset > 0:
		direction = -1
	case at.Y > bounds.Bottom()-edge && scroll.Offset < scroll.Max():
		direction = 1
	default:
		u.contactScrollTime = 0
		return
	}
	now := c.Now()
	if u.contactScrollTime > 0 {
		elapsed := min(now-u.contactScrollTime, 100*time.Millisecond)
		scroll.ScrollTo(max(0, min(scroll.Max(), scroll.Offset+direction*500*float32(elapsed.Seconds()))))
	}
	u.contactScrollTime = now
	c.RequestFrameIn(16 * time.Millisecond)
}

// Both the group header and its visible members accept a contact. Resolve
// against the current list so a refresh during a drag cannot move a hidden
// or removed contact.
func (u *UI) contactMove(source, target gfx.ID) (peerID, name string, ok bool) {
	var from, to string
	for _, group := range u.contacts {
		if u.groupHeadIDs[group.Name] == target {
			to = group.Name
		}
		for _, peer := range group.Peers {
			switch u.peerRowIDs[peer.ID] {
			case source:
				peerID, from = peer.ID, group.Name
			case target:
				to = group.Name
			}
		}
	}
	if peerID == "" || to == "" || to == from {
		return "", "", false
	}
	if to == "未分组" {
		to = ""
	}
	return peerID, to, true
}

func (u *UI) canDropContact(source, target gfx.ID) bool {
	_, _, ok := u.contactMove(source, target)
	return ok
}

func (u *UI) dropContact(source, target gfx.ID) {
	if peerID, name, ok := u.contactMove(source, target); ok {
		u.submit(domain.Command{Kind: domain.SetContactGroup, PeerID: peerID, Name: name})
	}
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
