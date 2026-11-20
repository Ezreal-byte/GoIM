package core

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"goim/internal/domain"
)

func relayTestGroup(t *testing.T) (*Node, *Node, *Node, string) {
	t.Helper()
	hub, first, second := testNode(t, "Windows"), testNode(t, "统信"), testNode(t, "麒麟")
	connectNodes(t, first, hub)
	connectNodes(t, second, hub)
	root := hub.Snapshot().Self
	peers := []domain.Peer{root, first.Snapshot().Self, second.Snapshot().Self}
	nonce := newID()
	group := domain.Group{ID: groupID(root.PublicKey, nonce), Nonce: nonce, Name: "隔离网段测试",
		RootKey: root.PublicKey, OwnerKey: root.PublicKey, OwnerID: root.ID, Epoch: 1,
		Members: []string{root.ID, peers[1].ID, peers[2].ID}}
	signGroup(&group, hub.key)
	for _, node := range []*Node{hub, first, second} {
		if err := node.change(func(state *domain.State) {
			for _, peer := range peers {
				if peer.ID != state.Self.ID && peerIndex(state, peer.ID) < 0 {
					peer.Address = "127.0.0.1:1"
					state.Peers = append(state.Peers, peer)
				}
			}
			installGroupState(state, group)
		}); err != nil {
			t.Fatal(err)
		}
	}
	hub.exchangePeerRoutes()
	eventually(t, "single-hop routes", func() bool {
		first.mu.Lock()
		defer first.mu.Unlock()
		return first.relayRoutes[root.ID][peers[2].ID]
	})
	return hub, first, second, group.ID
}

func TestGroupRelayDeliversBidirectionallyWithoutDirectConnection(t *testing.T) {
	_, first, second, groupID := relayTestGroup(t)
	for index := range 20 {
		if err := first.sendText("group:"+groupID, fmt.Sprintf("统信-%d", index)); err != nil {
			t.Fatal(err)
		}
		if err := second.sendText("group:"+groupID, fmt.Sprintf("麒麟-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	for _, node := range []*Node{first, second} {
		eventually(t, "all forty messages with durable acknowledgements", func() bool {
			state := node.Snapshot()
			if len(state.Messages) != 40 {
				return false
			}
			for _, message := range state.Messages {
				if message.Status != "delivered" {
					return false
				}
			}
			return true
		})
	}
	first.mu.Lock()
	connected := first.connections[second.Snapshot().Self.ID] != nil
	first.mu.Unlock()
	if connected {
		t.Fatal("test unexpectedly formed a direct connection")
	}
	first.replay(second.Snapshot().Self.ID)
	if len(second.Snapshot().Messages) != 40 {
		t.Fatal("replay duplicated a message")
	}
}

func TestRelayRejectsTamperingExpiryAndNestedForwarding(t *testing.T) {
	hub, first, second, groupID := relayTestGroup(t)
	message := envelope{ID: newID(), Type: "message", Message: domain.Message{
		ID: newID(), Kind: "text", Text: "签名校验", SenderID: first.Snapshot().Self.ID,
		ConversationID: "group:" + groupID, CreatedAt: time.Now()}}
	payload, _ := json.Marshal(message)
	packet := relayPacket{From: first.Snapshot().Self.ID, To: second.Snapshot().Self.ID,
		Key: first.key.Public().(ed25519.PublicKey), Payload: payload, Expires: time.Now().Add(time.Minute).Unix()}
	packet.Signature = ed25519.Sign(first.key, relayBody(packet))
	packet.Payload = append([]byte(nil), payload...)
	packet.Payload[0] = '!'
	if err := second.receiveRelayed(hub.Snapshot().Self.ID, &packet); err == nil {
		t.Fatal("accepted tampered packet")
	}
	packet.Payload, packet.Expires = payload, time.Now().Add(-time.Second).Unix()
	packet.Signature = ed25519.Sign(first.key, relayBody(packet))
	if err := second.receiveRelayed(hub.Snapshot().Self.ID, &packet); err == nil {
		t.Fatal("accepted expired packet")
	}
	message.Type = "relay"
	packet.Payload, _ = json.Marshal(message)
	packet.Expires = time.Now().Add(time.Minute).Unix()
	packet.Signature = ed25519.Sign(first.key, relayBody(packet))
	if err := second.receiveRelayed(hub.Snapshot().Self.ID, &packet); err == nil {
		t.Fatal("accepted recursive relay")
	}
}

func TestGroupRelayRetriesAfterCommonPeerReconnect(t *testing.T) {
	hub, first, second, groupID := relayTestGroup(t)
	directory := hub.dir
	if err := hub.Close(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "common peer disconnected", func() bool {
		first.mu.Lock()
		firstCount := len(first.connections)
		first.mu.Unlock()
		second.mu.Lock()
		secondCount := len(second.connections)
		second.mu.Unlock()
		return firstCount == 0 && secondCount == 0
	})
	if err := first.sendText("group:"+groupID, "共同节点恢复后补发"); err != nil {
		t.Fatal(err)
	}
	if first.Snapshot().Messages[0].Status == "delivered" {
		t.Fatal("undelivered relay message was acknowledged early")
	}
	hub = testNodeAt(t, "Windows", directory)
	connectNodes(t, first, hub)
	connectNodes(t, second, hub)
	eventually(t, "queued relay acknowledged by receiver", func() bool {
		sent, received := first.Snapshot().Messages, second.Snapshot().Messages
		return len(sent) == 1 && sent[0].Status == "delivered" && len(received) == 1 && received[0].Text == "共同节点恢复后补发"
	})
}

func TestVoiceHistoryIsPersistedOnceOnBothSides(t *testing.T) {
	caller, receiver := testNode(t, "呼叫方"), testNode(t, "接听方")
	connectNodes(t, caller, receiver)
	if err := caller.startVoiceCall(receiver.Snapshot().Self.ID); err != nil {
		t.Fatal(err)
	}
	call := waitVoiceCall(t, receiver, func(call domain.VoiceCall) bool { return call.Status == domain.VoiceCallRinging })
	if err := receiver.acceptVoiceCall(call.ID); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, caller, func(call domain.VoiceCall) bool { return call.Status == domain.VoiceCallActive })
	if err := caller.endVoiceCall(call.ID); err != nil {
		t.Fatal(err)
	}
	for _, node := range []*Node{caller, receiver} {
		eventually(t, "persisted call history", func() bool {
			state := node.Snapshot()
			return len(state.Messages) == 1 && state.Messages[0].ID == "voice:"+call.ID && state.Messages[0].Kind == "system"
		})
		node.finishVoice(call.ID, "ended", true)
		if len(node.Snapshot().Messages) != 1 {
			t.Fatal("duplicate call history")
		}
	}
	directory := receiver.dir
	if err := receiver.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := testNodeAt(t, "接听方", directory)
	if len(restarted.Snapshot().Messages) != 1 {
		t.Fatal("restart lost call history")
	}
}

func TestDiscoveryReconnectsMovedIdentityWithoutDuplicatingContact(t *testing.T) {
	host := "127.0.0.2"
	probe, probeErr := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if probeErr != nil {
		host = "127.0.0.1"
		t.Log("secondary loopback unavailable; verifying identity across a changed port instead")
	} else {
		_ = probe.Close()
	}
	first, second := testNode(t, "Windows"), testNode(t, "移动设备")
	connectNodes(t, first, second)
	conversationID := direct(t, first, second)
	identity, directory := second.Snapshot().Self.ID, second.dir
	if err := first.sendText(conversationID, "换网前"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "original message", func() bool { return len(second.Snapshot().Messages) == 1 })
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := New(Options{DataDir: directory, Listen: net.JoinHostPort(host, "0"), Name: "移动设备", DisableDiscovery: true, DisableCompatibility: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	packet := announcement{Version: protocolVersion, Peer: second.Snapshot().Self}
	packet.Peer.Address = second.Address()
	body, _ := json.Marshal(packet)
	packet.Signature = ed25519.Sign(second.key, body)
	body, _ = json.Marshal(packet)
	first.handleDiscoveryMessage(body, net.ParseIP(host))
	eventually(t, "moved address authenticated", func() bool {
		peer := first.Snapshot().Peer(identity)
		return peer.Online && peer.Address == second.Address()
	})
	if len(first.Snapshot().Peers) != 1 || first.Snapshot().Conversation(conversationID).PeerID != identity {
		t.Fatal("IP change duplicated identity or conversation")
	}
	if err := first.sendText(conversationID, "换网后"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "same history after network change", func() bool { return len(second.Snapshot().Messages) == 2 })
}

func TestDiscoveryKeepsHealthyMultiInterfaceConnection(t *testing.T) {
	first, second := testNode(t, "Windows"), testNode(t, "双网卡设备")
	connectNodes(t, first, second)
	eventually(t, "initial connection dial finished", func() bool {
		first.mu.Lock()
		defer first.mu.Unlock()
		return len(first.dialing) == 0
	})
	identity := second.Snapshot().Self.ID
	first.mu.Lock()
	connection := first.connections[identity]
	first.mu.Unlock()
	packet := announcement{Version: protocolVersion, Peer: second.Snapshot().Self}
	packet.Peer.Address = second.Address()
	body, _ := json.Marshal(packet)
	packet.Signature = ed25519.Sign(second.key, body)
	body, _ = json.Marshal(packet)
	first.handleDiscoveryMessage(body, net.ParseIP("127.0.0.2"))
	first.mu.Lock()
	dials := len(first.dialing)
	unchanged := first.connections[identity] == connection
	first.mu.Unlock()
	if dials != 0 || !unchanged {
		t.Fatal("another interface displaced a healthy authenticated connection")
	}
}
