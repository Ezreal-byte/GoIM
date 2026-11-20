package core

import (
	"encoding/json"
	"strings"
	"testing"

	"goim/internal/domain"
	ipmsgproto "goim/internal/ipmsg"
	"goim/internal/neiwangtong"
)

func TestContactVisibilityFollowsLocalIdentityAndReconnect(t *testing.T) {
	a, b := testNode(t, "Alice"), testNode(t, "Bob")
	connectNodes(t, a, b)
	id := b.Snapshot().Self.ID
	set := func(kind domain.CommandKind, hidden bool) error {
		return a.execute(domain.Command{Kind: kind, PeerID: id, Bool: hidden})
	}
	if err := set(domain.DeleteContact, false); err == nil {
		t.Fatal("deleted an online contact")
	}
	conv := direct(t, a, b)
	if err := a.sendText(conv, "first"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "delivered before disconnect", func() bool {
		return len(b.Snapshot().Messages) == 1 && a.Snapshot().Messages[0].Status == "delivered"
	})
	if err := set(domain.SetContactHidden, true); err != nil {
		t.Fatal(err)
	}
	peer := a.Snapshot().Peer(id)
	wire, err := json.Marshal(peer)
	if err != nil || !peer.ContactHidden || strings.Contains(string(wire), "ContactHidden") {
		t.Fatalf("local visibility or serialization failed: %v, %s", err, wire)
	}
	dir := a.dir
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a = testNodeAt(t, "Alice", dir)
	if !a.Snapshot().Peer(id).ContactHidden {
		t.Fatal("lost hidden contact on restart")
	}
	connectNodes(t, a, b)
	if !a.Snapshot().Peer(id).ContactHidden {
		t.Fatal("reconnect unhid a contact")
	}
	if err := set(domain.SetContactHidden, false); err != nil || a.Snapshot().Peer(id).ContactHidden {
		t.Fatalf("could not restore contact: %v", err)
	}
	bDir := b.dir
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "offline contact", func() bool { return !a.Snapshot().Peer(id).Online })
	if err := set(domain.DeleteContact, false); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a = testNodeAt(t, "Alice", dir)
	state := a.Snapshot()
	if peer := state.Peer(id); !peer.ContactDeleted || peer.ListedContact() || state.Conversation(conv).ID == "" || len(state.Messages) != 1 {
		t.Fatal("offline deletion lost history or returned the contact after restart")
	}
	b = testNodeAt(t, "Bob", bDir)
	connectNodes(t, a, b)
	if peer := a.Snapshot().Peer(id); peer.ContactDeleted || !peer.ListedContact() {
		t.Fatal("contact did not return after reconnect")
	}
}

func TestCompatibilityReconnectRestoresDeletedContact(t *testing.T) {
	n := testNode(t, "local")
	for _, kind := range []domain.CommandKind{domain.SetContactHidden, domain.DeleteContact} {
		for _, id := range []string{"", "unknown", n.Snapshot().Self.ID} {
			if err := n.execute(domain.Command{Kind: kind, PeerID: id, Bool: true}); err == nil {
				t.Fatalf("accepted %v for %q", kind, id)
			}
		}
	}
	for _, transport := range []string{domain.TransportIPMSG, domain.TransportNeiwangtong} {
		t.Run(transport, func(t *testing.T) {
			id := "ipmsg-bob"
			observe := func(online bool) {
				n.receiveLegacyPeer(ipmsgproto.Peer{ID: id, Name: "Bob", Address: "192.0.2.2:2425", Online: online})
			}
			if transport == domain.TransportNeiwangtong {
				id = "00112233445566778899aabbccddeeff"
				observe = func(online bool) {
					n.receiveNeiwangtongPeer(neiwangtong.Peer{ID: id, Name: "Bob", Address: "192.0.2.3:9012", Online: online})
				}
			}
			observe(true)
			observe(false)
			if err := n.execute(domain.Command{Kind: domain.DeleteContact, PeerID: id}); err != nil {
				t.Fatal(err)
			}
			observe(false)
			if !n.Snapshot().Peer(id).ContactDeleted {
				t.Fatal("duplicate offline notice revived a contact")
			}
			observe(true)
			if peer := n.Snapshot().Peer(id); !peer.Online || !peer.ListedContact() || peer.ContactDeleted {
				t.Fatalf("reconnected contact: %+v", peer)
			}
		})
	}
}
