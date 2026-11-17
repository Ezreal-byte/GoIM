package core

import (
	"errors"
	"time"

	"goim/internal/domain"
	"goim/internal/neiwangtong"
)

// Native rooms are separate from signed GoIM groups. An incoming roster can
// only come from the original client that owns that room.
type nativeGroup struct {
	WireID, Owner               string
	InfoVersion, MembersVersion uint64
	SelfLeft                    bool `json:",omitempty"`
}

const (
	nativeGroupRosterDelivery = "native_group_roster"
	nativeGroupExitDelivery   = "native_group_exit"
)

func nativeGroupID(wireID string) string { return legacyStableID("nwt-group", wireID) }

func (n *Node) isNativeGroup(id string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.data.NativeGroups[id].WireID != ""
}

func (n *Node) createNativeGroup(s domain.State, name string, targets []string) error {
	wireID := newID()
	g := domain.Group{ID: nativeGroupID(wireID), Name: name, OwnerID: s.Self.ID, Transport: transportNWT,
		Members: append([]string{s.Self.ID}, targets...), Epoch: 2}
	room := nativeGroup{WireID: wireID, Owner: keyIDFromNative(nativeIDForGoIM(s.Self.ID)),
		InfoVersion: g.Epoch, MembersVersion: g.Epoch}
	return n.durable(func(p *persisted) []pending {
		if p.NativeGroups == nil {
			p.NativeGroups = make(map[string]nativeGroup)
		}
		p.NativeGroups[g.ID] = room
		installGroupState(&p.State, g)
		p.State.Notice = "内网通群聊已创建"
		return nativeRosterDeliveries(g)
	})
}

func nativeRosterDeliveries(group domain.Group) []pending {
	var out []pending
	for _, peer := range group.Members {
		if peer != group.OwnerID {
			out = append(out, pending{PeerID: peer, Envelope: envelope{Type: nativeGroupRosterDelivery, Target: group.ID}})
		}
	}
	return out
}

func (n *Node) renameNativeGroup(g domain.Group, selfID, name string) error {
	if g.OwnerID != selfID {
		return errors.New("只有群主可以执行此操作")
	}
	if g.Name == name {
		return nil
	}
	return n.durable(func(p *persisted) []pending {
		room := p.NativeGroups[g.ID]
		room.InfoVersion++
		p.NativeGroups[g.ID] = room
		g.Name, g.Epoch = name, max(room.InfoVersion, room.MembersVersion)
		installGroupState(&p.State, g)
		return nativeRosterDeliveries(g)
	})
}

func (n *Node) addNativeGroupMembers(group domain.Group, state domain.State, ids []string) error {
	previousSize := len(group.Members)
	for _, id := range ids {
		if contains(group.Members, id) {
			continue
		}
		peer := state.Peer(id)
		if !peer.Online || peer.Transport != transportNWT || !peer.SupportsGroups() {
			return errors.New("请选择在线的内网通联系人")
		}
		group.Members = append(group.Members, id)
	}
	if len(group.Members) == previousSize {
		return errors.New("请选择尚未加入的联系人")
	}
	if len(group.Members) > 256 {
		return errors.New("群聊人数已达上限")
	}
	return n.durable(func(p *persisted) []pending {
		room := p.NativeGroups[group.ID]
		room.MembersVersion++
		p.NativeGroups[group.ID] = room
		group.Epoch = max(room.InfoVersion, room.MembersVersion)
		installGroupState(&p.State, group)
		return nativeRosterDeliveries(group)
	})
}

func (n *Node) nativeRosterForDelivery(groupID, recipient string) (neiwangtong.GroupRoster, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	state := &n.data.State
	room, group := n.data.NativeGroups[groupID], state.Group(groupID)
	if room.WireID == "" || group.OwnerID != state.Self.ID || group.Left || group.Dissolved || !contains(group.Members, recipient) {
		return neiwangtong.GroupRoster{}, errors.New("这个成员已无法收到内网通群名单")
	}
	roster := neiwangtong.GroupRoster{ID: room.WireID, Name: group.Name, OwnerID: room.Owner,
		InfoVersion: room.InfoVersion, MembersVersion: room.MembersVersion,
		Members: make([]neiwangtong.GroupMember, 0, len(group.Members))}
	for _, id := range group.Members {
		if id == state.Self.ID {
			roster.Members = append(roster.Members, neiwangtong.GroupMember{ID: room.Owner, Name: state.Self.Name})
			continue
		}
		wire := wireCompatibilityID(&n.data, id, transportNWT)
		if wire == "" {
			return neiwangtong.GroupRoster{}, errors.New("内网通群成员的连接身份已变更")
		}
		roster.Members = append(roster.Members, neiwangtong.GroupMember{ID: wire, Name: state.Peer(id).Name})
	}
	return roster, nil
}

func (n *Node) receiveNeiwangtongGroupRoster(peerID string, roster neiwangtong.GroupRoster) {
	if roster.OwnerID != peerID {
		return
	}
	_ = n.durable(func(p *persisted) []pending {
		s := &p.State
		owner := n.compatibilitySenderLocked(transportNWT, peerID)
		if owner.ID == "" {
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
			memberID := member.ID
			if bridge, ok := bridgeFor(p, memberID); ok {
				memberID = bridge.CanonicalID
			}
			if peerIndex(s, memberID) < 0 {
				name := member.Name
				if name == "" {
					name = "内网通联系人"
				}
				s.Peers = append(s.Peers, domain.Peer{ID: memberID, Name: name,
					Transport: transportNWT, ClientType: domain.ClientNeiwangtong})
			}
			if !contains(members, memberID) {
				members = append(members, memberID)
			}
		}
		if p.NativeGroups == nil {
			p.NativeGroups = make(map[string]nativeGroup)
		}
		p.NativeGroups[id] = nativeGroup{WireID: roster.ID, Owner: roster.OwnerID,
			InfoVersion: roster.InfoVersion, MembersVersion: roster.MembersVersion, SelfLeft: current.SelfLeft && found}
		if current.SelfLeft {
			members = without(members, s.Self.ID)
		}
		g := domain.Group{ID: id, Name: roster.Name, OwnerID: owner.ID, Transport: transportNWT, Members: members,
			Epoch: max(roster.InfoVersion, roster.MembersVersion), Left: !found || current.SelfLeft}
		if i < 0 {
			s.Groups = append(s.Groups, g)
		} else {
			s.Groups[i] = g
		}
		ensureGroup(s, g)
		return nil
	})
}

func (n *Node) leaveNativeGroup(groupID string) error {
	var rejected error
	err := n.durable(func(p *persisted) []pending {
		s := &p.State
		room, group := p.NativeGroups[groupID], s.Group(groupID)
		index := groupIndex(s, groupID)
		if index < 0 || room.WireID == "" || group.Left || group.Dissolved || !contains(group.Members, s.Self.ID) {
			rejected = errors.New("群聊已不可用")
			return nil
		}
		if group.OwnerID == s.Self.ID {
			rejected = errors.New("群主需先转让群主或解散群聊")
			return nil
		}
		room.SelfLeft = true
		p.NativeGroups[groupID] = room
		group.Left, group.Members = true, without(group.Members, s.Self.ID)
		s.Groups[index] = group
		out := n.cancelInvalidGroup(p, group)
		for _, peer := range group.Members {
			out = append(out, pending{PeerID: peer, WireTransport: transportNWT, WirePeerID: wireCompatibilityID(p, peer, transportNWT),
				Envelope: envelope{Type: nativeGroupExitDelivery, Target: room.WireID}})
		}
		return out
	})
	return errors.Join(err, rejected)
}

func (n *Node) receiveNeiwangtongGroupExit(peerID, wireGroupID string) {
	id := nativeGroupID(wireGroupID)
	_ = n.durable(func(p *persisted) []pending {
		s := &p.State
		room, group := p.NativeGroups[id], s.Group(id)
		sender := n.compatibilitySenderLocked(transportNWT, peerID).ID
		if room.WireID == "" || group.Left || group.Dissolved || sender == "" || sender == group.OwnerID ||
			!contains(group.Members, sender) {
			return nil
		}
		group.Members = without(group.Members, sender)
		var out []pending
		if group.OwnerID == s.Self.ID {
			room.MembersVersion++
			p.NativeGroups[id] = room
			group.Epoch = max(room.InfoVersion, room.MembersVersion)
			out = nativeRosterDeliveries(group)
		}
		installGroupState(s, group)
		return append(out, n.cancelInvalidGroup(p, group)...)
	})
}

func (n *Node) receiveNeiwangtongGroupText(peerID string, event neiwangtong.GroupMessage) error {
	message := event.Message
	if len(message.Parts) == 0 && message.RecallID == "" || !validNativeChatParts(message) {
		return errors.New("invalid native group message")
	}
	id := nativeGroupID(event.GroupID)
	var accepted bool
	var downloads, cancelled []string
	err := n.durable(func(p *persisted) []pending {
		s := &p.State
		room := s.Group(id)
		sender := n.compatibilitySenderLocked(transportNWT, peerID)
		if p.NativeGroups[id].WireID == "" || room.Left || room.Dissolved || !contains(room.Members, s.Self.ID) ||
			sender.ID == "" || !contains(room.Members, sender.ID) && !contains(room.Members, peerID) {
			return nil
		}
		accepted = true
		now := time.Now()
		downloads, cancelled = applyNativeChat(p, nativeChatTarget{
			conversation: "group:" + id, sender: sender.ID, wirePeer: peerID, group: id,
		}, message, now, now)
		return nil
	})
	if err == nil && !accepted {
		err = errors.New("sender is not a member of the native group")
	}
	if err == nil {
		n.cancelRecalledTransfers(cancelled)
		for _, id := range downloads {
			n.startReceive(id, "")
		}
	}
	return err
}

func (n *Node) activeNativeGroup(id, recipient string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	room := n.data.NativeGroups[id]
	state := &n.data.State
	group := state.Group(id)
	if room.WireID == "" || group.Left || group.Dissolved || !contains(group.Members, state.Self.ID) ||
		!contains(group.Members, recipient) {
		return "", errors.New("该联系人不在此内网通群中")
	}
	return room.WireID, nil
}
