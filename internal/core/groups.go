package core

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"goim/internal/domain"
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
func (n *Node) createGroup(name string, ids []string) error {
	n.groupMu.Lock()
	defer n.groupMu.Unlock()
	s := n.Snapshot()
	selected := []string{}
	for _, id := range ids {
		if id != s.Self.ID && !contains(selected, id) {
			p := s.Peer(id)
			if !p.Online {
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
	n.change(func(s *domain.State) {
		s.Groups = append(s.Groups, g)
		ensureGroup(s, g)
		s.Notice = "群聊已创建，等待邀请确认"
	})
	for _, id := range selected {
		n.sendInvite(g, id)
	}
	return nil
}
func (n *Node) sendInvite(g domain.Group, peer string) {
	s := n.Snapshot()
	inv := domain.Invitation{ID: newID(), GroupID: g.ID, GroupName: g.Name, FromID: s.Self.ID, FromName: s.Self.Name, Kind: "join", Status: "pending", CreatedAt: time.Now()}
	n.reliable(peer, envelope{Type: "group_invite", Group: g, Invitation: inv, Peers: groupPeers(s, g)})
}
func (n *Node) inviteMembers(id string, ids []string) error {
	n.groupMu.Lock()
	defer n.groupMu.Unlock()
	s := n.Snapshot()
	g := s.Group(id)
	if g.ID == "" || g.OwnerID != s.Self.ID || g.Dissolved {
		return errors.New("只有群主可以邀请成员")
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
	n.installGroup(g)
	n.broadcastGroup(g)
	for _, peer := range selected {
		n.sendInvite(g, peer)
	}
	return nil
}
func (n *Node) installGroup(g domain.Group) {
	n.change(func(s *domain.State) {
		i := groupIndex(s, g.ID)
		if i < 0 {
			s.Groups = append(s.Groups, g)
		} else {
			g.Left = s.Groups[i].Left
			s.Groups[i] = g
		}
		if contains(g.Members, s.Self.ID) {
			ensureGroup(s, g)
		}
	})
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
func (n *Node) broadcastGroup(g domain.Group) {
	s := n.Snapshot()
	for _, id := range append(append([]string(nil), g.Members...), g.Pending...) {
		if id != s.Self.ID {
			n.reliable(id, envelope{Type: "group_state", Group: g, Peers: groupPeers(s, g)})
		}
	}
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
	if inv.ID == "" || inv.Status != "pending" {
		return errors.New("邀请已失效")
	}
	status := "rejected"
	if accept {
		status = "accepted"
	}
	n.change(func(s *domain.State) {
		for i := range s.Invitations {
			if s.Invitations[i].ID == id {
				s.Invitations[i].Status = status
			}
		}
	})
	if inv.Kind == "ownership" {
		n.reliable(inv.FromID, envelope{Type: "ownership_accept", Group: s.Group(inv.GroupID), Invitation: inv, Status: status})
		return nil
	}
	n.reliable(inv.FromID, envelope{Type: "invite_reply", Invitation: inv, Status: status})
	return nil
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
		n.reliable(g.OwnerID, envelope{Type: "group_leave", Group: domain.Group{ID: g.ID}})
		n.change(func(s *domain.State) {
			if i := groupIndex(s, g.ID); i >= 0 {
				s.Groups[i].Left = true
			}
		})
		return nil
	}
	if g.OwnerID != s.Self.ID {
		return errors.New("只有群主可以执行此操作")
	}
	switch c.Kind {
	case domain.RenameGroup:
		name := strings.TrimSpace(c.Name)
		if name == "" || utf8.RuneCountInString(name) > 30 {
			return errors.New("群名需为 1 至 30 个字符")
		}
		g.Name = name
	case domain.RemoveMember:
		if c.PeerID == s.Self.ID {
			return errors.New("不能移除群主")
		}
		g.Members = without(g.Members, c.PeerID)
		g.Pending = without(g.Pending, c.PeerID)
	case domain.DissolveGroup:
		g.Dissolved = true
	case domain.TransferOwner:
		p := s.Peer(c.PeerID)
		if !contains(g.Members, p.ID) || p.ID == s.Self.ID || !p.Online {
			return errors.New("请选择一位在线群成员")
		}
		inv := domain.Invitation{ID: newID(), GroupID: g.ID, GroupName: g.Name, FromID: s.Self.ID, FromName: s.Self.Name, Kind: "ownership", Status: "pending", CreatedAt: time.Now()}
		n.mu.Lock()
		n.data.OwnerOffers[inv.ID] = p.ID
		_ = saveStore(n.db, &n.data)
		n.mu.Unlock()
		n.reliable(p.ID, envelope{Type: "ownership_offer", Group: g, Invitation: inv})
		n.notice("已发送群主转让请求")
		return nil
	}
	g.Epoch++
	signGroup(&g, n.key)
	n.installGroup(g)
	n.broadcastGroup(g)
	if c.Kind == domain.RemoveMember {
		n.reliable(c.PeerID, envelope{Type: "group_state", Group: g})
	}
	return nil
}
func (n *Node) receiveGroup(peer string, e envelope) error {
	n.groupMu.Lock()
	defer n.groupMu.Unlock()
	s := n.Snapshot()
	g := e.Group
	switch e.Type {
	case "group_invite", "ownership_offer":
		if !verifyGroup(g) || peer != g.OwnerID || e.Invitation.FromID != peer || e.Invitation.GroupID != g.ID {
			return errors.New("群邀请身份校验失败")
		}
		if e.Type == "group_invite" && !contains(g.Pending, s.Self.ID) {
			return errors.New("邀请未包含此设备")
		}
		if e.Type == "ownership_offer" && !contains(g.Members, s.Self.ID) {
			return errors.New("你不是此群成员")
		}
		n.installGroup(g)
		n.change(func(s *domain.State) {
			for _, i := range s.Invitations {
				if i.ID == e.Invitation.ID {
					return
				}
			}
			s.Invitations = append(s.Invitations, e.Invitation)
			s.Notice = "收到群聊邀请"
		})
	case "invite_reply":
		g = s.Group(e.Invitation.GroupID)
		if g.OwnerID != s.Self.ID || !contains(g.Pending, peer) {
			return nil
		}
		g.Pending = without(g.Pending, peer)
		if e.Status == "accepted" {
			g.Members = append(g.Members, peer)
		}
		g.Epoch++
		signGroup(&g, n.key)
		n.installGroup(g)
		n.broadcastGroup(g)
		if e.Status != "accepted" {
			n.reliable(peer, envelope{Type: "group_state", Group: g})
		}
	case "group_state":
		if !verifyGroup(g) {
			return errors.New("群状态签名无效")
		}
		old := s.Group(g.ID)
		if old.ID == "" {
			return errors.New("未知群聊")
		}
		if !bytes.Equal(g.RootKey, old.RootKey) {
			return errors.New("群状态身份不匹配")
		}
		if g.Epoch > old.Epoch {
			n.installGroup(g)
		}
	case "group_leave":
		g = s.Group(g.ID)
		if g.OwnerID != s.Self.ID || !contains(g.Members, peer) {
			return nil
		}
		g.Members = without(g.Members, peer)
		g.Epoch++
		signGroup(&g, n.key)
		n.installGroup(g)
		n.broadcastGroup(g)
		n.reliable(peer, envelope{Type: "group_state", Group: g})
	case "ownership_accept":
		g = s.Group(e.Invitation.GroupID)
		n.mu.Lock()
		offered := n.data.OwnerOffers[e.Invitation.ID] == peer
		delete(n.data.OwnerOffers, e.Invitation.ID)
		_ = saveStore(n.db, &n.data)
		n.mu.Unlock()
		if !offered || g.OwnerID != s.Self.ID || !contains(g.Members, peer) || e.Status != "accepted" {
			return nil
		}
		p := s.Peer(peer)
		proof := domain.Ownership{FromID: s.Self.ID, ToID: peer, ToKey: p.PublicKey, Epoch: g.Epoch + 1}
		proof.Signature = ed25519.Sign(n.key, proofBody(g.ID, proof))
		n.reliable(peer, envelope{Type: "group_state", Group: g, Proof: proof})
	}
	// The receiving new owner signs the next group snapshot, carrying the old
	// owner's transition certificate so disconnected members can verify it later.
	if e.Proof.ToID == s.Self.ID && e.Type == "group_state" {
		old := s.Group(g.ID)
		p := e.Proof
		if old.OwnerID != peer || p.FromID != peer || !ed25519.Verify(old.OwnerKey, proofBody(g.ID, p), p.Signature) {
			return errors.New("群主转让证书无效")
		}
		g = old
		g.Ownership = append(g.Ownership, p)
		g.OwnerID = s.Self.ID
		g.OwnerKey = s.Self.PublicKey
		g.Epoch = p.Epoch
		signGroup(&g, n.key)
		n.installGroup(g)
		n.broadcastGroup(g)
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
				n.Connect(p.Address)
			}
		}
	}
	return nil
}
