package core

import (
	"encoding/json"
	"strings"
	"testing"

	"goim/internal/domain"
)

func TestContactRemarkPersistsLocallyAcrossProfileRefreshAndRestart(t *testing.T) {
	a, b := testNode(t, "Alice"), testNode(t, "Bob")
	connectNodes(t, a, b)
	id := b.Snapshot().Self.ID
	if err := a.setContactRemark(id, "  财务小王  "); err != nil {
		t.Fatal(err)
	}
	peer := a.Snapshot().Peer(id)
	if peer.Name != "Bob" || peer.DisplayName() != "财务小王" {
		t.Fatalf("peer=%+v", peer)
	}
	wire, err := json.Marshal(envelope{Peer: peer, Peers: []domain.Peer{peer}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "财务") || strings.Contains(string(wire), "Remark") {
		t.Fatal("local remark leaked into wire profile")
	}
	if b.Snapshot().Peer(a.Snapshot().Self.ID).Remark != "" {
		t.Fatal("remark reached the remote peer")
	}
	if err := a.change(func(s *domain.State) {
		i := peerIndex(s, id)
		s.Peers[i] = domain.Peer{ID: id, Name: "Robert"}
	}); err != nil {
		t.Fatal(err)
	}
	if a.Snapshot().Peer(id).DisplayName() != "财务小王" {
		t.Fatal("profile refresh erased remark")
	}
	dir := a.dir
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a = testNodeAt(t, "Alice", dir)
	if a.Snapshot().Peer(id).DisplayName() != "财务小王" {
		t.Fatal("restart lost remark")
	}
	if err := a.setContactRemark(id, ""); err != nil {
		t.Fatal(err)
	}
	if a.Snapshot().Peer(id).DisplayName() != "Robert" {
		t.Fatal("clearing remark did not restore username")
	}
}

func TestContactRemarkRejectsInvalidValues(t *testing.T) {
	n := testNode(t, "local")
	if err := n.change(func(s *domain.State) { s.Peers = append(s.Peers, domain.Peer{ID: "peer", Name: "name"}) }); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{strings.Repeat("字", 41), "line\nbreak", "a\x00b", "a\tb", "\xff"} {
		if err := n.setContactRemark("peer", value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if err := n.setContactRemark("missing", "name"); err == nil {
		t.Fatal("accepted missing peer")
	}
}

func TestContactRemarkFollowsAuthenticatedIdentity(t *testing.T) {
	for _, canonicalRemark := range []string{"", "原生备注"} {
		t.Run("canonical="+canonicalRemark, func(t *testing.T) {
			a, b := testNode(t, "Alice"), testNode(t, "Bob")
			peer := b.Snapshot().Self
			alias := keyIDFromNative(nativeIDForGoIM(peer.ID))
			if err := a.change(func(s *domain.State) {
				s.Peers = append(s.Peers, domain.Peer{ID: alias, Name: "Bob", Transport: transportNWT}, peer)
			}); err != nil {
				t.Fatal(err)
			}
			if err := a.setContactRemark(alias, "兼容备注"); err != nil {
				t.Fatal(err)
			}
			if canonicalRemark != "" {
				if err := a.setContactRemark(peer.ID, canonicalRemark); err != nil {
					t.Fatal(err)
				}
			}
			connectNodes(t, a, b)
			want := canonicalRemark
			if want == "" {
				want = "兼容备注"
			}
			for _, id := range []string{alias, peer.ID} {
				if got := a.Snapshot().Peer(id).Remark; got != want {
					t.Fatalf("remark for %s = %q, want %q", id, got, want)
				}
			}
			// Old conversations may still address the compatibility identity.
			if err := a.setContactRemark(alias, ""); err != nil {
				t.Fatal(err)
			}
			dir := a.dir
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			a = testNodeAt(t, "Alice", dir)
			for _, id := range []string{alias, peer.ID} {
				if got := a.Snapshot().Peer(id).Remark; got != "" {
					t.Fatalf("cleared remark reappeared for %s: %q", id, got)
				}
			}
		})
	}
}
