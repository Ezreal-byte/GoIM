package core

import (
	"errors"
	"time"
	"unicode/utf8"

	"goim/internal/domain"
	"goim/internal/neiwangtong"
)

// A native room is keyed separately from GoIM's signed groups. Membership
// updates are accepted only on the established connection from its stock owner.
type nativeGroup struct {
	WireID, Owner               string
	InfoVersion, MembersVersion uint64
}

func nativeGroupID(wireID string) string { return legacyStableID("nwt-group", wireID) }

func (n *Node) isNativeGroup(id string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.data.NativeGroups[id].WireID != ""
}

func (n *Node) receiveNeiwangtongGroupRoster(peerID string, roster neiwangtong.GroupRoster) {
	if roster.OwnerID != peerID {
		return
	}
	_ = n.durable(func(p *persisted) []pending {
		s := &p.State
		if n.compatibilitySenderLocked(transportNWT, peerID).ID == "" {
			return nil
		}
		id := nativeGroupID(roster.ID)
		current, known := p.NativeGroups[id]
		if known && (current.Owner != peerID || roster.MembersVersion < current.MembersVersion ||
			roster.InfoVersion < current.InfoVersion ||
			roster.MembersVersion == current.MembersVersion && roster.InfoVersion == current.InfoVersion) {
			return nil
		}
		self := keyIDFromNative(nativeIDForGoIM(s.Self.ID))
		found := false
		for _, member := range roster.Members {
			found = found || member.ID == self
		}
		i := groupIndex(s, id)
		if !found && !known {
			return nil // an unrelated group's announcement must not create a conversation
		}
		members := make([]string, 0, len(roster.Members))
		for _, member := range roster.Members {
			if member.ID == self {
				members = append(members, s.Self.ID)
				continue
			}
			if peerIndex(s, member.ID) < 0 {
				name := member.Name
				if name == "" {
					name = "内网通联系人"
				}
				s.Peers = append(s.Peers, domain.Peer{ID: member.ID, Name: name,
					Transport: transportNWT, ClientType: domain.ClientNeiwangtong})
			}
			members = append(members, member.ID)
		}
		if p.NativeGroups == nil {
			p.NativeGroups = make(map[string]nativeGroup)
		}
		p.NativeGroups[id] = nativeGroup{roster.ID, roster.OwnerID, roster.InfoVersion, roster.MembersVersion}
		g := domain.Group{ID: id, Name: roster.Name, OwnerID: peerID, Members: members,
			Epoch: max(roster.InfoVersion, roster.MembersVersion), Left: !found}
		if i < 0 {
			s.Groups = append(s.Groups, g)
		} else {
			s.Groups[i] = g
		}
		ensureGroup(s, g)
		return nil
	})
}

func (n *Node) receiveNeiwangtongGroupText(peerID string, event neiwangtong.GroupMessage) error {
	m := event.Message
	body := m.Text()
	if body == "" || len(body) > 256<<10 || !utf8.ValidString(body) {
		return errors.New("invalid native group text")
	}
	id := nativeGroupID(event.GroupID)
	var accepted bool
	err := n.durable(func(p *persisted) []pending {
		s := &p.State
		room := s.Group(id)
		if p.NativeGroups[id].WireID == "" || room.Left || room.Dissolved || !contains(room.Members, s.Self.ID) ||
			!contains(room.Members, peerID) || n.compatibilitySenderLocked(transportNWT, peerID).ID == "" {
			return nil
		}
		accepted = true
		key := legacyStableID("nwt-group-message", id, peerID, nativeMessageIdentity(m))
		if !hasNativeMessage(s, key) {
			addMessageState(s, domain.Message{ID: key, ConversationID: "group:" + id,
				SenderID: peerID, Kind: "text", Text: body, CreatedAt: time.Now(), Status: "delivered"})
		}
		return nil
	})
	if err == nil && !accepted {
		err = errors.New("sender is not a member of the native group")
	}
	return err
}
