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
	if err := node.change(func(s *domain.State) {
		peer := domain.Peer{ID: legacyID, Name: "相同昵称", ClientType: domain.ClientNeiwangtong, Transport: legacyIPMSG}
		s.Peers = append(s.Peers, peer)
		addMessageState(s, domain.Message{ID: "old", ConversationID: ensureDirect(s, peer), SenderID: legacyID, Kind: "text", Text: "旧会话"})
	}); err != nil {
		t.Fatal(err)
	}
	node.receiveLegacyPeer(ipmsgproto.Peer{ID: legacyID, Name: "相同昵称", Address: "192.0.2.10:2425", Online: true, Neiwangtong: true})
	node.receiveNeiwangtongPeer(neiwangtong.Peer{ID: nativeID, Name: "相同昵称", Address: "192.0.2.10:9012", Online: true})
	s := node.Snapshot()
	if old := s.Peer(legacyID); len(s.Peers) != 2 || old.ListedContact() || old.Online ||
		old.CompatibilityAliasOf != "" || s.Peer(nativeID).Transport != transportNWT {
		t.Fatalf("independent identities were rewritten: %+v", s.Peers)
	}
	archive := directID(s.Self.ID, legacyID)
	node.receiveLegacyText(legacyID, 2, "不要进入旧会话")
	if len(node.Snapshot().ChatMessages(archive)) != 1 {
		t.Fatal("old conversation changed")
	}
	if err := node.sendText(archive, "不能发往未经确认的身份"); err == nil {
		t.Fatal("an unlinked legacy chat accepted a new send")
	}
}

func TestNeiwangtongAndIPMSGAdvertisementsUseTheirOwnRoutes(t *testing.T) {
	node := testNode(t, "local")
	const alias, nativeID, ipmsgID = "ipmsg-nwt", "00112233445566778899aabbccddeeff", "ipmsg-feiq"
	before := node.Snapshot().Revision
	node.receiveLegacyPeer(ipmsgproto.Peer{ID: alias, Name: "原版", Address: "192.0.2.10:2425", Online: true, Neiwangtong: true})
	if s := node.Snapshot(); len(s.Peers) != 0 || before != s.Revision {
		t.Fatal("Neiwangtong's compatibility announcement appeared in the contact state")
	}
	node.receiveNeiwangtongPeer(neiwangtong.Peer{ID: nativeID, Name: "原版", Address: "192.0.2.10:9012", Online: true})
	node.receiveLegacyPeer(ipmsgproto.Peer{ID: ipmsgID, Name: "飞秋", Address: "192.0.2.10:2425", Online: true})
	if s := node.Snapshot(); len(s.Peers) != 2 {
		t.Fatalf("separate native and IPMSG contacts must remain visible: %+v", s.Peers)
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	for id, expected := range map[string]string{nativeID: transportNWT, ipmsgID: legacyIPMSG} {
		transport, wireID, ready := node.pendingRouteLocked(pending{PeerID: id})
		if transport != expected || wireID != id || !ready {
			t.Fatalf("route for %s was %q/%q (ready=%t)", id, transport, wireID, ready)
		}
	}
}

func TestSavedCompatibilityLinkKeepsHistoryAndUsesNativeForNewSends(t *testing.T) {
	dir := t.TempDir()
	node := testNodeAt(t, "local", dir)
	const legacyID, nativeID = "ipmsg-link", "00112233445566778899aabbccddeeff"
	legacy := ipmsgproto.Peer{ID: legacyID, Name: "Fallback", Address: "192.0.2.10:2425", Online: true, Neiwangtong: true}
	native := neiwangtong.Peer{ID: nativeID, Name: "Native", Address: "192.0.2.10:9012", Online: true}
	if err := node.durable(func(p *persisted) []pending {
		p.PeerBridges[legacyID] = peerBridge{CanonicalID: legacyID, IPMSGID: legacyID, NeiwangtongID: nativeID, Confirmed: true}
		for _, peer := range []domain.Peer{
			{ID: legacyID, Name: legacy.Name, ClientType: domain.ClientNeiwangtong, Transport: legacyIPMSG},
			{ID: nativeID, Name: native.Name, ClientType: domain.ClientNeiwangtong, Transport: transportNWT},
		} {
			p.State.Peers = append(p.State.Peers, peer)
			addMessageState(&p.State, domain.Message{ID: "old-" + peer.ID, ConversationID: ensureDirect(&p.State, peer), SenderID: peer.ID, Kind: "text", Text: "old history"})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	node.receiveLegacyPeer(legacy)
	node.receiveNeiwangtongPeer(native)
	s := node.Snapshot()
	if s.Peer(legacyID).Transport != transportNWT || s.Peer(nativeID).CompatibilityAliasOf != legacyID {
		t.Fatalf("preferred route is wrong: %+v", s.Peers)
	}
	primary, archive := directID(s.Self.ID, legacyID), directID(s.Self.ID, nativeID)
	node.receiveLegacyText(legacyID, 3, "original used 2425")
	if err := node.receiveNeiwangtongMessage(nativeID, neiwangtong.DirectMessage{
		PacketID: 4, Parts: []neiwangtong.MessagePart{{Text: "native message"}},
	}); err != nil {
		t.Fatal(err)
	}
	node.receiveLegacyFiles(legacyID, 5, "", []ipmsgproto.Attachment{{ID: 3, Name: "previous-route.txt", Size: 8}})
	s = node.Snapshot()
	if len(s.ChatMessages(primary)) != 4 || len(s.ChatMessages(archive)) != 1 || s.Transfers[0].RemotePeerID != legacyID {
		t.Fatalf("bridged ingress did not preserve history or transfer identity: %+v", s.Messages)
	}
	native.Online = false
	node.receiveNeiwangtongPeer(native)
	if p := node.Snapshot().Peer(legacyID); p.Transport != transportNWT || p.Online || !p.ListedContact() {
		t.Fatalf("native disconnect fell back to IPMSG: %+v", p)
	}
	node.mu.Lock()
	tr, wire, ready := node.pendingRouteLocked(pending{PeerID: legacyID})
	node.mu.Unlock()
	if tr != transportNWT || wire != nativeID || ready {
		t.Fatal("a new send could downgrade to the legacy route")
	}
	native.Online = true
	node.receiveNeiwangtongPeer(native)
	s = node.Snapshot()
	if s.Peer(legacyID).Transport != transportNWT || s.Transfers[0].LegacyTransport != legacyIPMSG {
		t.Fatal("native recovery did not keep the transfer on its original protocol")
	}
	node.mu.Lock()
	tr, wire, ready = node.pendingRouteLocked(pending{PeerID: legacyID, WireTransport: legacyIPMSG, WirePeerID: legacyID})
	node.mu.Unlock()
	if tr != legacyIPMSG || wire != legacyID || !ready {
		t.Fatal("an already dispatched message changed transports")
	}
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}
	node = testNodeAt(t, "local", dir)
	node.receiveLegacyPeer(legacy)
	if p := node.Snapshot().Peer(legacyID); p.Transport != transportNWT || p.Online {
		t.Fatal("restart downgraded before the native peer was verified")
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
