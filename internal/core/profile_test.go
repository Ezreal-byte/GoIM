package core

import (
	"testing"

	"goim/internal/domain"
)

func TestProfileDetailsPersistAndReachConnectedPeer(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	details := domain.ProfileDetails{Gender: "female", Company: "示例单位", Department: "研发", Phone: "1001", Mobile: "555-0100", Email: "test@example.com"}
	if err := a.execute(domain.Command{Kind: domain.SetProfile, Name: "甲", Presence: domain.PresenceOnline, ProfileDetails: &details}); err != nil {
		t.Fatal(err)
	}
	id := a.Snapshot().Self.ID
	eventually(t, "updated contact profile reached peer", func() bool { return b.Snapshot().Peer(id).Details == details })
	dir := a.dir
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := testNodeAt(t, "甲", dir)
	if got := reopened.Snapshot(); got.Settings.ProfileDetails != details || got.Self.Details != details {
		t.Fatal("profile details did not survive restart")
	}
}
