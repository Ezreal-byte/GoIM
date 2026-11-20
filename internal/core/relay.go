package core

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"goim/internal/domain"
)

type relayPacket struct {
	From      string
	To        string
	Key       []byte
	Expires   int64
	Payload   []byte
	Signature []byte
}

func relayBody(packet relayPacket) []byte {
	packet.Signature = nil
	body, _ := json.Marshal(packet)
	return body
}

func relayType(kind string) bool {
	switch kind {
	case "message", "message_recall", "group_invite", "group_state", "invite_reply", "group_leave",
		"ownership_offer", "ownership_accept", "ownership_result", "group_nickname", "file_offer", "file_retry", "file_status", "ack":
		return true
	}
	return false
}

func relayGroupAllows(state domain.State, from, to string, message envelope) bool {
	if message.Type == "ack" {
		return true
	}
	groups := state.Groups
	if verifyGroup(message.Group) {
		groups = append(append([]domain.Group(nil), groups...), message.Group)
	}
	for _, group := range groups {
		if group.Dissolved || group.Left {
			continue
		}
		member := func(id string) bool { return contains(group.Members, id) || contains(group.Pending, id) }
		if member(state.Self.ID) && member(from) && member(to) {
			return true
		}
	}
	return false
}

func (n *Node) sendRelayed(target string, message envelope) error {
	if !relayType(message.Type) {
		return errors.New("等待对方上线")
	}
	n.mu.Lock()
	self := n.data.State.Self.ID
	if !relayGroupAllows(n.data.State, self, target, message) {
		n.mu.Unlock()
		return errors.New("等待对方上线")
	}
	var routes []*peerConn
	var ids []string
	for id, connection := range n.connections {
		if connection.peer.RelaySupported && n.relayRoutes[id][target] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		routes = append(routes, n.connections[id])
	}
	n.mu.Unlock()
	if len(routes) == 0 {
		return errors.New("等待群成员直连或局域网中继上线")
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	packet := relayPacket{From: self, To: target, Key: n.key.Public().(ed25519.PublicKey), Expires: time.Now().Add(time.Minute).Unix(), Payload: payload}
	packet.Signature = ed25519.Sign(n.key, relayBody(packet))
	for _, connection := range routes {
		if err := connection.send(envelope{Type: "relay", Relay: &packet}); err == nil {
			return nil
		}
		connection.conn.Close()
	}
	return errors.New("局域网中继暂时不可用，消息保留待重试")
}

func (n *Node) receiveRelayed(via string, packet *relayPacket) error {
	if packet == nil || len(packet.Key) != ed25519.PublicKeySize || packet.From != keyID(packet.Key) ||
		len(packet.Payload) > maxFrame/2 || packet.Expires < time.Now().Unix() || packet.Expires > time.Now().Add(2*time.Minute).Unix() ||
		!ed25519.Verify(packet.Key, relayBody(*packet), packet.Signature) {
		return errors.New("无效的群消息中继签名")
	}
	var message envelope
	if json.Unmarshal(packet.Payload, &message) != nil || !relayType(message.Type) || message.Relay != nil {
		return errors.New("无效的群消息中继类型")
	}
	n.mu.Lock()
	self := n.data.State.Self.ID
	allowed := relayGroupAllows(n.data.State, packet.From, packet.To, message)
	target := n.connections[packet.To]
	if packet.To == self && allowed {
		if n.relayRoutes[via] == nil {
			n.relayRoutes[via] = make(map[string]bool)
		}
		n.relayRoutes[via][packet.From] = true
	}
	n.mu.Unlock()
	if !allowed {
		return errors.New("群成员身份不允许中继")
	}
	if packet.To == self {
		return n.receive(packet.From, message)
	}
	if via != packet.From || target == nil {
		return nil
	}
	return target.send(envelope{Type: "relay", Relay: packet})
}

func (n *Node) exchangePeerRoutes() {
	n.mu.Lock()
	var peers []domain.Peer
	var connections []*peerConn
	for _, connection := range n.connections {
		peer := connection.peer
		peer.Avatar, peer.Signature, peer.Details = "", "", domain.ProfileDetails{}
		peers = append(peers, peer)
		if peer.RelaySupported {
			connections = append(connections, connection)
		}
	}
	n.mu.Unlock()
	for _, connection := range connections {
		_ = connection.send(envelope{Type: "peer_routes", Peers: peers})
	}
}

func (n *Node) receivePeerRoutes(via string, peers []domain.Peer) error {
	if len(peers) > 256 {
		return errors.New("局域网路由数量超限")
	}
	n.mu.Lock()
	routes := make(map[string]bool)
	for _, peer := range peers {
		if len(peer.PublicKey) == ed25519.PublicKeySize && peer.ID == keyID(peer.PublicKey) && peer.ID != n.data.State.Self.ID && peer.ID != via {
			routes[peer.ID] = true
		}
	}
	n.relayRoutes[via] = routes
	var pendingPeers []string
	for id := range routes {
		for _, delivery := range n.data.Outbox {
			if delivery.PeerID == id {
				pendingPeers = append(pendingPeers, id)
				break
			}
		}
	}
	n.mu.Unlock()
	for _, id := range pendingPeers {
		n.flushPeer(id, true)
	}
	return nil
}
