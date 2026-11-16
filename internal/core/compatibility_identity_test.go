package core

import (
	"testing"

	"goim/internal/domain"
	ipmsgproto "goim/internal/ipmsg"
	"goim/internal/neiwangtong"
)

func TestCompatibilityDoesNotMergeByIPAndName(t *testing.T) {
	node := testNode(t, "local")
	const legacyID, nativeID = "ipmsg-neiwangtong", "00112233445566778899aabbccddeeff"
	node.receiveLegacyPeer(ipmsgproto.Peer{ID: legacyID, Name: "相同昵称", Address: "192.0.2.10:2425", Online: true, Neiwangtong: true})
	node.receiveLegacyText(legacyID, 1, "旧会话")
	node.receiveNeiwangtongPeer(neiwangtong.Peer{ID: nativeID, Name: "相同昵称", Address: "192.0.2.10:9012", Online: true})
	s := node.Snapshot()
	if len(s.Peers) != 2 || s.Peer(legacyID).Transport != legacyIPMSG || s.Peer(nativeID).Transport != transportNWT {
		t.Fatalf("independent identities were rewritten: %+v", s.Peers)
	}
	if len(s.ChatMessages(directID(s.Self.ID, legacyID))) != 1 {
		t.Fatal("old conversation changed")
	}
}

func TestCompatibilityLinkKeepsHistoryAndSwitchesOnlyVerifiedRoutes(t *testing.T) {
	dir := t.TempDir()
	node := testNodeAt(t, "local", dir)
	const legacyID, nativeID = "ipmsg-link", "00112233445566778899aabbccddeeff"
	legacy := ipmsgproto.Peer{ID: legacyID, Name: "Fallback", Address: "192.0.2.10:2425", Online: true, Neiwangtong: true}
	native := neiwangtong.Peer{ID: nativeID, Name: "Native", Address: "192.0.2.10:9012", Online: true}
	node.receiveLegacyPeer(legacy)
	node.receiveLegacyText(legacyID, 1, "legacy history")
	node.receiveNeiwangtongPeer(native)
	node.receiveNeiwangtongText(nativeID, 2, "native history")
	if err := node.linkCompatibility(legacyID, nativeID); err != nil {
		t.Fatal(err)
	}
	s := node.Snapshot()
	if s.Peer(legacyID).Transport != transportNWT || s.Peer(nativeID).CompatibilityAliasOf != legacyID {
		t.Fatalf("preferred route is wrong: %+v", s.Peers)
	}
	primary, archive := directID(s.Self.ID, legacyID), directID(s.Self.ID, nativeID)
	node.receiveLegacyText(legacyID, 3, "original used 2425")
	node.receiveNeiwangtongText(nativeID, 4, "native message")
	node.receiveLegacyFiles(legacyID, 5, "", []ipmsgproto.Attachment{{ID: 3, Name: "previous-route.txt", Size: 8}})
	s = node.Snapshot()
	if len(s.ChatMessages(primary)) != 4 || len(s.ChatMessages(archive)) != 1 || s.Transfers[0].RemotePeerID != legacyID {
		t.Fatalf("bridged ingress did not preserve history or transfer identity: %+v", s.Messages)
	}
	native.Online = false
	node.receiveNeiwangtongPeer(native)
	if p := node.Snapshot().Peer(legacyID); p.Transport != legacyIPMSG || !p.Online || p.ClientType != domain.ClientNeiwangtong {
		t.Fatalf("lost native path did not fall back: %+v", p)
	}
	native.Online = true
	node.receiveNeiwangtongPeer(native)
	s = node.Snapshot()
	if s.Peer(legacyID).Transport != transportNWT || s.Transfers[0].LegacyTransport != legacyIPMSG {
		t.Fatal("native recovery did not keep the transfer on its original protocol")
	}
	node.mu.Lock()
	tr, wire, ready := node.pendingRouteLocked(pending{PeerID: legacyID, WireTransport: legacyIPMSG, WirePeerID: legacyID})
	node.mu.Unlock()
	if tr != legacyIPMSG || wire != legacyID || !ready {
		t.Fatal("an already dispatched message changed transports")
	}
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}
	node = testNodeAt(t, "local", dir)
	node.receiveLegacyPeer(legacy)
	if p := node.Snapshot().Peer(legacyID); p.Transport != legacyIPMSG || !p.Online {
		t.Fatal("restart picked an unverified native route")
	}
	node.receiveNeiwangtongPeer(native)
	s = node.Snapshot()
	if s.Peer(legacyID).Transport != transportNWT || len(s.ChatMessages(archive)) != 1 {
		t.Fatal("confirmed link or archived conversation was lost")
	}
}

func TestUnconfirmedBridgeIsNeverUsedAfterUpgrade(t *testing.T) {
	dir := t.TempDir()
	n := testNodeAt(t, "local", dir)
	const legacyID, nativeID = "ipmsg-old", "00112233445566778899aabbccddeeff"
	if err := n.durable(func(p *persisted) []pending {
		p.PeerBridges[legacyID] = peerBridge{CanonicalID: legacyID, IPMSGID: legacyID, NeiwangtongID: nativeID}
		p.State.Peers = append(p.State.Peers, domain.Peer{ID: legacyID, Transport: transportNWT, ClientType: domain.ClientNeiwangtong})
		ensureDirect(&p.State, p.State.Peer(legacyID))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = testNodeAt(t, "local", dir)
	n.receiveNeiwangtongPeer(neiwangtong.Peer{ID: nativeID, Name: "Independent", Address: "192.0.2.10:9012", Online: true})
	s := n.Snapshot()
	if s.Peer(legacyID).Transport != legacyIPMSG || s.Peer(legacyID).Online ||
		s.Conversation(directID(s.Self.ID, legacyID)).ID == "" {
		t.Fatalf("unverified mapping or its chat was handled incorrectly: %+v", s.Peers)
	}
}

func TestAutoRoutePrefersAuthenticatedGoIM(t *testing.T) {
	a, b := testNode(t, "a"), testNode(t, "b")
	if err := a.connectVia(domain.TransportAuto, b.Address()); err != nil {
		t.Fatal(err)
	}
	id := b.Snapshot().Self.ID
	eventually(t, "verified TLS connection", func() bool { return a.Snapshot().Peer(id).Online })
	alias := keyIDFromNative(nativeIDForGoIM(id))
	a.receiveNeiwangtongPeer(neiwangtong.Peer{ID: alias, Name: "b", Online: true, Address: "127.0.0.1:9012"})
	if s := a.Snapshot(); len(s.Peers) != 1 || s.Peer(id).Transport != domain.TransportGoIM {
		t.Fatalf("native appearance displaced the authenticated contact: %+v", s.Peers)
	}
}
