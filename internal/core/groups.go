package core

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"goim/internal/domain"
	"strings"
	"time"
	"unicode/utf8"
)

func groupID(root []byte, nonce string) string {
	h := sha256.Sum256(append(append([]byte(nil), root...), []byte(nonce)...))
	return hex.EncodeToString(h[:16])
}
func groupBody(g domain.Group) []byte {
	g.Signature = nil
	g.Left = false
	b, _ := json.Marshal(g)
	return b
}
func signGroup(g *domain.Group, key ed25519.PrivateKey) {
	g.Signature = ed25519.Sign(key, groupBody(*g))
}

type ownershipBody struct {
	GroupID string
	FromID  string
	ToID    string
	ToKey   []byte
	Epoch   uint64
}

func proofBody(id string, p domain.Ownership) []byte {
	b, _ := json.Marshal(ownershipBody{id, p.FromID, p.ToID, p.ToKey, p.Epoch})
	return b
}
func verifyGroup(g domain.Group) bool {
	if len(g.RootKey) != ed25519.PublicKeySize || g.ID != groupID(g.RootKey, g.Nonce) || len(g.Members) > 256 || utf8.RuneCountInString(g.Name) > 30 {
		return false
	}
	key := g.RootKey
	epoch := uint64(0)
	for _, p := range g.Ownership {
		if p.FromID != keyID(key) || len(p.ToKey) != ed25519.PublicKeySize || p.ToID != keyID(p.ToKey) || p.Epoch <= epoch || !ed25519.Verify(key, proofBody(g.ID, p), p.Signature) {
			return false
		}
		key = p.ToKey
		epoch = p.Epoch
	}
	return g.Epoch >= epoch && g.OwnerID == keyID(key) && bytes.Equal(g.OwnerKey, key) && contains(g.Members, g.OwnerID) && ed25519.Verify(key, groupBody(g), g.Signature)
}
func groupPeers(s domain.State, g domain.Group) []domain.Peer {
	var list []domain.Peer
	for _, id := range g.Members {
		p := s.Peer(id)
		p.Avatar = ""
		if len(p.PublicKey) == ed25519.PublicKeySize {
			list = append(list, p)
		}
	}
	return list
}
func installGroupState(s *domain.State, g domain.Group) {
	i := groupIndex(s, g.ID)
	g.Left = false
	if i < 0 {
		s.Groups = append(s.Groups, g)
	} else {
		g.Left = s.Groups[i].Left
		s.Groups[i] = g
	}
	if contains(g.Members, s.Self.ID) {
		ensureGroup(s, g)
	}
	for i := range s.Invitations {
		v := &s.Invitations[i]
		if v.GroupID != g.ID || v.Kind != "join" {
			continue
		}
		if v.FromID == s.Self.ID && !contains(g.Pending, v.ToID) && v.Status == "pending" {
			if contains(g.Members, v.ToID) {
				v.Status = "accepted"
			} else {
				v.Status = "withdrawn"
			}
		}
		if v.FromID != s.Self.ID && !contains(g.Pending, s.Self.ID) && !contains(g.Members, s.Self.ID) {
			v.Status = "withdrawn"
		}
	}
}
func groupDeliveries(s domain.State, g domain.Group, extra ...string) []pending {
	var out []pending
	var ids []string
	all := append(append(append([]string(nil), g.Members...), g.Pending...), extra...)
	for _, id := range all {
		if id != "" && id != s.Self.ID && !contains(ids, id) {
			ids = append(ids, id)
			out = append(out, pending{PeerID: id, Envelope: envelope{Type: "group_state", Group: g, Peers: groupPeers(s, g)}})
		}
	}
	return out
}
func (n *Node) cancelInvalidGroup(p *persisted, g domain.Group) []pending {
	var out []pending
	local := p.State.Group(g.ID)
	for i := range p.State.Transfers {
		t := &p.State.Transfers[i]
		if t.ConversationID != "group:"+g.ID || terminalTransfer(t.Status) {
			continue
		}
		if g.Dissolved || local.Left || !contains(g.Members, p.State.Self.ID) || !contains(g.Members, t.PeerID) {
			if cancel := n.transfers[t.ID]; cancel != nil {
				cancel()
			}
			t.Status = "cancelled"
			t.Error = "群成员权限已变更"
			t.Speed = 0
			out = append(out, pending{PeerID: t.PeerID, Envelope: envelope{Type: "file_status", TransferID: t.ID, Status: "cancelled"}})
		}
	}
	for id, d := range p.Outbox {
		conv := d.Envelope.Message.ConversationID
		if d.Envelope.Type == "file_offer" {
			conv = d.Envelope.File.ConversationID
		}
		if conv == "group:"+g.ID && (g.Dissolved || local.Left || !contains(g.Members, d.PeerID)) {
			delete(p.Outbox, id)
		}
	}
	return out
}
func (n *Node) installGroup(g domain.Group) {
	_ = n.durable(func(p *persisted) []pending { installGroupState(&p.State, g); return n.cancelInvalidGroup(p, g) })
}
func (n *Node) broadcastGroup(g domain.Group) {
	_ = n.durable(func(p *persisted) []pending { return groupDeliveries(p.State, g) })
}
func invitationDelivery(s domain.State, g domain.Group, peer string) (domain.Invitation, pending) {
	v := domain.Invitation{ID: newID(), GroupID: g.ID, GroupName: g.Name, FromID: s.Self.ID, FromName: s.Self.Name, ToID: peer, Kind: "join", Status: "pending", CreatedAt: time.Now()}
	return v, pending{PeerID: peer, Envelope: envelope{Type: "group_invite", Group: g, Invitation: v, Peers: groupPeers(s, g)}}
}
func (n *Node) createGroup(name string, ids []string) error {
	n.groupMu.Lock()
	defer n.groupMu.Unlock()
	s := n.Snapshot()
	var selected []string
	for _, id := range ids {
		if id != s.Self.ID && !contains(selected, id) {
			if !s.Peer(id).Online {
				return errors.New("建群时请选择在线联系人")
			}
			selected = append(selected, id)
		}
	}
	if len(selected) < 2 {
		return errors.New("请选择至少两位在线联系人")
	}
	if len(selected) > 255 {
		return errors.New("首期群聊最多 256 人")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = s.Peer(selected[0]).Name + "、" + s.Peer(selected[1]).Name
	}
	r := []rune(name)
	if len(r) > 30 {
		name = string(r[:30])
	}
	root := append([]byte(nil), s.Self.PublicKey...)
	nonce := newID()
	g := domain.Group{ID: groupID(root, nonce), Name: name, OwnerID: s.Self.ID, OwnerKey: root, RootKey: root, Nonce: nonce, Members: []string{s.Self.ID}, Pending: selected, Epoch: 1}
	signGroup(&g, n.key)
	return n.durable(func(p *persisted) []pending {
		installGroupState(&p.State, g)
		p.State.Notice = "群聊已创建，等待邀请确认"
		var out []pending
		for _, id := range selected {
			v, d := invitationDelivery(p.State, g, id)
			p.State.Invitations = append(p.State.Invitations, v)
			out = append(out, d)
		}
		return out
	})
}
func activeOwnership(p persisted, id string) bool {
	for _, v := range p.OwnerOffers {
		if v.GroupID == id && (v.Stage == "committing" || time.Since(v.CreatedAt) < 5*time.Minute) {
			return true
		}
	}
	return false
}
func (n *Node) inviteMembers(id string, ids []string) error {
	n.groupMu.Lock()
	defer n.groupMu.Unlock()
	s := n.Snapshot()
	g := s.Group(id)
	if g.ID == "" || g.OwnerID != s.Self.ID || g.Dissolved {
		return errors.New("只有群主可以邀请成员")
	}
	n.mu.Lock()
	busy := activeOwnership(n.data, id)
	n.mu.Unlock()
	if busy {
		return errors.New("群主转让尚未结束，请稍后管理群聊")
	}
	var selected []string
	for _, peer := range ids {
		if !contains(g.Members, peer) && !contains(g.Pending, peer) && s.Peer(peer).Online {
			g.Pending = append(g.Pending, peer)
			selected = append(selected, peer)
		}
	}
	if len(g.Members)+len(g.Pending) > 256 {
		return errors.New("群聊人数已达上限")
	}
	g.Epoch++
	signGroup(&g, n.key)
	return n.durable(func(p *persisted) []pending {
		installGroupState(&p.State, g)
		out := groupDeliveries(p.State, g)
		for _, id := range selected {
			v, d := invitationDelivery(p.State, g, id)
			p.State.Invitations = append(p.State.Invitations, v)
			out = append(out, d)
		}
		return out
	})
}
func (n *Node) answerInvite(id string, accept bool) error {
	s := n.Snapshot()
	var inv domain.Invitation
	for _, v := range s.Invitations {
		if v.ID == id {
			inv = v
			break
		}
	}
	if inv.ID == "" || inv.Status != "pending" || inv.FromID == s.Self.ID {
		return errors.New("邀请已失效")
	}
	status := "rejected"
	if accept {
		status = "accepted"
	}
	kind := "invite_reply"
	if inv.Kind == "ownership" {
		kind = "ownership_accept"
	}
	return n.durable(func(p *persisted) []pending {
		for i := range p.State.Invitations {
			if p.State.Invitations[i].ID == id {
				p.State.Invitations[i].Status = status
			}
		}
		if accept && inv.Kind == "join" {
			if i := groupIndex(&p.State, inv.GroupID); i >= 0 {
				p.State.Groups[i].Left = false
			}
		}
		return []pending{{PeerID: inv.FromID, Envelope: envelope{Type: kind, Invitation: inv, Status: status}}}
	})
}
func (n *Node) manageGroup(c domain.Command) error {
	n.groupMu.Lock()
	defer n.groupMu.Unlock()
	s := n.Snapshot()
	g := s.Group(c.GroupID)
	if g.ID == "" || g.Dissolved || g.Left {
		return errors.New("群聊已不可用")
	}
	if c.Kind == domain.LeaveGroup {
		if g.OwnerID == s.Self.ID {
			return errors.New("群主需先转让群主或解散群聊")
		}
		return n.durable(func(p *persisted) []pending {
			if i := groupIndex(&p.State, g.ID); i >= 0 {
				p.State.Groups[i].Left = true
			}
			out := n.cancelInvalidGroup(p, g)
			return append(out, pending{PeerID: g.OwnerID, Envelope: envelope{Type: "group_leave", Group: domain.Group{ID: g.ID}}})
		})
	}
	if g.OwnerID != s.Self.ID {
		return errors.New("只有群主可以执行此操作")
	}
	n.mu.Lock()
	busy := activeOwnership(n.data, g.ID)
	n.mu.Unlock()
	if busy {
		return errors.New("群主转让尚未结束，请稍后管理群聊")
	}
	switch c.Kind {
	case domain.RenameGroup:
		name := strings.TrimSpace(c.Name)
		if name == "" || utf8.RuneCountInString(name) > 30 {
			return errors.New("群名需为 1 至 30 个字符")
		}
		g.Name = name
	case domain.RemoveMember, domain.WithdrawInvite:
		if c.PeerID == "" {
			for _, v := range s.Invitations {
				if v.ID == c.InvitationID {
					c.PeerID = v.ToID
				}
			}
		}
		if c.PeerID == s.Self.ID {
			return errors.New("不能移除群主")
		}
		g.Members = without(g.Members, c.PeerID)
		g.Pending = without(g.Pending, c.PeerID)
	case domain.DissolveGroup:
		g.Dissolved = true
	case domain.TransferOwner:
		target := s.Peer(c.PeerID)
		if !contains(g.Members, target.ID) || target.ID == s.Self.ID || !target.Online {
			return errors.New("请选择一位在线群成员")
		}
		inv := domain.Invitation{ID: newID(), GroupID: g.ID, GroupName: g.Name, FromID: s.Self.ID, FromName: s.Self.Name, ToID: target.ID, Kind: "ownership", Status: "pending", CreatedAt: time.Now()}
		return n.durable(func(p *persisted) []pending {
			for id, v := range p.OwnerOffers {
				if v.GroupID == g.ID {
					delete(p.OwnerOffers, id)
				}
			}
			p.OwnerOffers[inv.ID] = ownershipOffer{PeerID: target.ID, GroupID: g.ID, Stage: "offered", CreatedAt: time.Now()}
			p.State.Invitations = append(p.State.Invitations, inv)
			p.State.Notice = "已发送群主转让请求"
			return []pending{{PeerID: target.ID, Envelope: envelope{Type: "ownership_offer", Group: g, Invitation: inv}}}
		})
	default:
		return errors.New("未知群管理操作")
	}
	g.Epoch++
	signGroup(&g, n.key)
	return n.durable(func(p *persisted) []pending {
		installGroupState(&p.State, g)
		out := groupDeliveries(p.State, g, c.PeerID)
		return append(out, n.cancelInvalidGroup(p, g)...)
	})
}
func (n *Node) receiveGroup(peer string, e envelope) error {
	n.groupMu.Lock()
	defer n.groupMu.Unlock()
	s := n.Snapshot()
	g := e.Group
	switch e.Type {
	case "group_invite", "ownership_offer":
		if !verifyGroup(g) || peer != g.OwnerID || e.Invitation.FromID != peer || e.Invitation.GroupID != g.ID || e.Invitation.ToID != "" && e.Invitation.ToID != s.Self.ID {
			return errors.New("群邀请身份校验失败")
		}
		if e.Type == "group_invite" && !contains(g.Pending, s.Self.ID) {
			return errors.New("邀请未包含此设备")
		}
		if e.Type == "ownership_offer" && !contains(g.Members, s.Self.ID) {
			return errors.New("你不是此群成员")
		}
		if old := s.Group(g.ID); old.ID != "" && old.Epoch > g.Epoch {
			return nil
		}
		if err := n.durable(func(p *persisted) []pending {
			installGroupState(&p.State, g)
			for _, v := range p.State.Invitations {
				if v.ID == e.Invitation.ID {
					return nil
				}
			}
			p.State.Invitations = append(p.State.Invitations, e.Invitation)
			p.State.Notice = "收到群聊邀请"
			return nil
		}); err != nil {
			return err
		}
	case "invite_reply":
		g = s.Group(e.Invitation.GroupID)
		if g.OwnerID != s.Self.ID || !contains(g.Pending, peer) {
			return nil
		}
		valid := false
		for _, v := range s.Invitations {
			if v.ID == e.Invitation.ID && v.GroupID == g.ID && v.ToID == peer && v.Status == "pending" {
				valid = true
			}
		}
		if !valid {
			return errors.New("群邀请已撤回或失效")
		}
		n.mu.Lock()
		busy := activeOwnership(n.data, g.ID)
		n.mu.Unlock()
		if busy {
			return errors.New("群主转让中，邀请回复稍后同步")
		}
		g.Pending = without(g.Pending, peer)
		if e.Status == "accepted" {
			g.Members = append(g.Members, peer)
		}
		g.Epoch++
		signGroup(&g, n.key)
		if err := n.durable(func(p *persisted) []pending {
			installGroupState(&p.State, g)
			for i := range p.State.Invitations {
				if p.State.Invitations[i].ID == e.Invitation.ID {
					p.State.Invitations[i].Status = e.Status
				}
			}
			return groupDeliveries(p.State, g, peer)
		}); err != nil {
			return err
		}
	case "group_state":
		if !verifyGroup(g) {
			return errors.New("群状态签名无效")
		}
		old := s.Group(g.ID)
		if old.ID == "" || !bytes.Equal(g.RootKey, old.RootKey) {
			return errors.New("未知群聊或身份不匹配")
		}
		if e.Proof.ToID == s.Self.ID {
			proof := e.Proof
			// The state may have committed before the acknowledgement reached the
			// previous owner. Replaying the same signed handoff is idempotent.
			for _, adopted := range old.Ownership {
				if adopted.FromID == peer && adopted.ToID == proof.ToID && adopted.Epoch == proof.Epoch && bytes.Equal(adopted.Signature, proof.Signature) {
					return nil
				}
			}
			if old.OwnerID != peer || proof.FromID != peer || proof.Epoch != old.Epoch+1 || !bytes.Equal(proof.ToKey, s.Self.PublicKey) || !ed25519.Verify(old.OwnerKey, proofBody(g.ID, proof), proof.Signature) {
				return errors.New("群主转让证书无效")
			}
			accepted := false
			for _, v := range s.Invitations {
				if v.Kind == "ownership" && v.GroupID == g.ID && v.FromID == peer && v.Status == "accepted" {
					accepted = true
				}
			}
			if !accepted {
				return errors.New("未确认群主转让")
			}
			g = old
			g.Ownership = append(g.Ownership, proof)
			g.OwnerID = s.Self.ID
			g.OwnerKey = s.Self.PublicKey
			g.Epoch = proof.Epoch
			signGroup(&g, n.key)
			if err := n.durable(func(p *persisted) []pending { installGroupState(&p.State, g); return groupDeliveries(p.State, g) }); err != nil {
				return err
			}
		} else if g.Epoch > old.Epoch {
			if err := n.durable(func(p *persisted) []pending {
				installGroupState(&p.State, g)
				for id, v := range p.OwnerOffers {
					if v.GroupID == g.ID && g.OwnerID == v.PeerID {
						delete(p.OwnerOffers, id)
					}
				}
				return n.cancelInvalidGroup(p, g)
			}); err != nil {
				return err
			}
		}
	case "group_leave":
		g = s.Group(g.ID)
		if g.OwnerID != s.Self.ID || !contains(g.Members, peer) {
			return nil
		}
		n.mu.Lock()
		busy := activeOwnership(n.data, g.ID)
		n.mu.Unlock()
		if busy {
			return errors.New("群主转让中，退出稍后同步")
		}
		g.Members = without(g.Members, peer)
		g.Epoch++
		signGroup(&g, n.key)
		if err := n.durable(func(p *persisted) []pending {
			installGroupState(&p.State, g)
			out := groupDeliveries(p.State, g, peer)
			return append(out, n.cancelInvalidGroup(p, g)...)
		}); err != nil {
			return err
		}
	case "ownership_accept":
		g = s.Group(e.Invitation.GroupID)
		n.mu.Lock()
		offer, ok := n.data.OwnerOffers[e.Invitation.ID]
		n.mu.Unlock()
		if !ok || offer.PeerID != peer || offer.GroupID != g.ID || offer.Stage != "offered" || time.Since(offer.CreatedAt) > 5*time.Minute || e.Invitation.Kind != "ownership" || e.Invitation.FromID != s.Self.ID || g.OwnerID != s.Self.ID || !contains(g.Members, peer) {
			return nil
		}
		if e.Status != "accepted" {
			return n.durable(func(p *persisted) []pending {
				delete(p.OwnerOffers, e.Invitation.ID)
				for i := range p.State.Invitations {
					if p.State.Invitations[i].ID == e.Invitation.ID {
						p.State.Invitations[i].Status = "rejected"
					}
				}
				return nil
			})
		}
		target := s.Peer(peer)
		proof := domain.Ownership{FromID: s.Self.ID, ToID: peer, ToKey: target.PublicKey, Epoch: g.Epoch + 1}
		proof.Signature = ed25519.Sign(n.key, proofBody(g.ID, proof))
		if err := n.durable(func(p *persisted) []pending {
			v := p.OwnerOffers[e.Invitation.ID]
			v.Stage = "committing"
			p.OwnerOffers[e.Invitation.ID] = v
			for i := range p.State.Invitations {
				if p.State.Invitations[i].ID == e.Invitation.ID {
					p.State.Invitations[i].Status = "accepted"
				}
			}
			return []pending{{PeerID: peer, Envelope: envelope{Type: "group_state", Group: g, Proof: proof}}}
		}); err != nil {
			return err
		}
	case "group_nickname":
		g = s.Group(g.ID)
		if g.ID == "" || !contains(g.Members, peer) || g.Dissolved || utf8.RuneCountInString(e.Peer.Name) > 40 {
			return errors.New("群昵称更新无效")
		}
		if err := n.change(func(s *domain.State) { setGroupNicknameState(s, g.ID, peer, e.Peer.Name) }); err != nil {
			return err
		}
	}
	if verifyGroup(g) {
		for _, p := range e.Peers {
			if p.ID == s.Self.ID || !contains(g.Members, p.ID) || len(p.PublicKey) != ed25519.PublicKeySize || p.ID != keyID(p.PublicKey) {
				continue
			}
			p.Avatar = ""
			p.Online = false
			n.change(func(st *domain.State) {
				if peerIndex(st, p.ID) < 0 {
					st.Peers = append(st.Peers, p)
				}
			})
			n.mu.Lock()
			online := n.connections[p.ID] != nil
			n.mu.Unlock()
			if !online && p.Address != "" {
				n.dialTo(p.Address, p.ID, true)
			}
		}
	}
	return nil
}
func setGroupNicknameState(s *domain.State, id, peer, name string) {
	for i := range s.GroupNicknames {
		v := &s.GroupNicknames[i]
		if v.GroupID == id && v.PeerID == peer {
			v.Name = name
			return
		}
	}
	s.GroupNicknames = append(s.GroupNicknames, domain.GroupNickname{GroupID: id, PeerID: peer, Name: name})
}
func (n *Node) setGroupNickname(id, name string) error {
	s := n.Snapshot()
	g := s.Group(id)
	name = strings.TrimSpace(name)
	if g.ID == "" || g.Left || g.Dissolved || !contains(g.Members, s.Self.ID) {
		return errors.New("你不在此群聊中")
	}
	if utf8.RuneCountInString(name) > 40 {
		return errors.New("群昵称最多 40 个字符")
	}
	return n.durable(func(p *persisted) []pending {
		setGroupNicknameState(&p.State, id, s.Self.ID, name)
		var out []pending
		for _, peer := range g.Members {
			if peer != s.Self.ID {
				out = append(out, pending{PeerID: peer, Envelope: envelope{Type: "group_nickname", Group: domain.Group{ID: id}, Peer: domain.Peer{Name: name}}})
			}
		}
		return out
	})
}
