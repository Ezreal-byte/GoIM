package core

import (
	"fmt"
	"strings"
	"testing"

	"goim/internal/domain"
)

func TestMemberProjectLinkRequiresCatalogFromCurrentConnection(t *testing.T) {
	dir := t.TempDir()
	peer := domain.Peer{ID: strings.Repeat("1", 32), Name: "publisher", Address: "127.0.0.1:25001", Online: true}
	key := strings.Repeat("a", 32)
	id := "member:" + peer.ID + ":" + key
	catalog := func(revision uint64, port uint16) envelope {
		return envelope{Type: "catalog", CatalogRevision: revision, MemberCatalog: &memberCatalog{
			Port: port, Projects: []memberProject{{Key: key, Name: "shared folder"}},
		}}
	}

	// Store a real member catalog, then reopen the recipient's database.
	n := testNodeAt(t, "member", dir)
	if err := n.change(func(s *domain.State) { s.Peers = []domain.Peer{peer} }); err != nil {
		t.Fatal(err)
	}
	if err := n.receive(peer.ID, catalog(5, 18080)); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = testNodeAt(t, "member", dir)
	wantURL := func(port uint16) {
		t.Helper()
		got := n.Snapshot().File(id)
		want := ""
		if port != 0 {
			want = fmt.Sprintf("http://127.0.0.1:%d/m/%s", port, key)
		}
		if got.ID != id || got.MemberURL != want {
			t.Fatalf("cached project %q has URL %q, want %q", got.ID, got.MemberURL, want)
		}
	}
	wantURL(0)
	current := &peerConn{peer: peer}
	replaced := &peerConn{peer: peer}
	n.mu.Lock()
	n.connections[peer.ID] = current
	n.data.State.Peers[0].Online = true
	n.mu.Unlock()
	defer func() { n.mu.Lock(); delete(n.connections, peer.ID); n.mu.Unlock() }()
	receive := func(source *peerConn, revision uint64, port uint16) {
		t.Helper()
		if err := n.receiveFrom(peer.ID, catalog(revision, port), source); err != nil {
			t.Fatal(err)
		}
	}
	wantURL(0)
	receive(replaced, 9, 18081)
	wantURL(0)
	receive(current, 4, 18082)
	wantURL(0)
	receive(current, 6, 18083)
	wantURL(18083)
	receive(replaced, 10, 18081)
	wantURL(18083)
	n.mu.Lock()
	next := &peerConn{peer: peer}
	n.connections[peer.ID] = next
	n.mu.Unlock()
	wantURL(0)
	receive(current, 11, 18084)
	wantURL(0)
	receive(next, 7, 18085)
	wantURL(18085)
}
