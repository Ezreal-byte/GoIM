package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goim/internal/domain"
)

func TestNeiwangtongLargeFileRoutes(t *testing.T) {
	const limit = int64(1 << 32)
	native := domain.Peer{ID: "native", Transport: transportNWT, Version: "3.4.3055"}
	direct := domain.Conversation{ID: "direct", Kind: "direct", PeerID: native.ID}
	group := domain.Conversation{ID: "group:native", Kind: "group", GroupID: "native"}
	goimGroup := domain.Conversation{ID: "group:goim", Kind: "group", GroupID: "goim"}
	p := persisted{State: domain.State{Peers: []domain.Peer{native}, Conversations: []domain.Conversation{direct, group, goimGroup}},
		NativeGroups: map[string]nativeGroup{"native": {WireID: "wire-group"}}}
	for _, tc := range []struct {
		name, version string
		conv          domain.Conversation
		size          int64
		wantError     bool
	}{
		{"last byte below boundary", "", direct, limit - 1, false},
		{"at boundary", "3.4.3055", direct, limit, false},
		{"verified Windows sample", "3.4.3055", direct, limit + 65553, false},
		{"unknown build", "", direct, limit, true},
		{"different build", "3.4.3056", direct, limit, true},
		{"native group", "3.4.3055", group, limit, true},
		{"smaller native group file", "", group, limit - 1, false},
		{"GoIM group", "", goimGroup, limit, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p.State.Peers[0].Version = tc.version
			if err := p.neiwangtongAttachmentSizeError(tc.conv, tc.size); (err != nil) != tc.wantError {
				t.Fatalf("admission: %v", err)
			}
			if tc.conv == goimGroup {
				return
			}
			task := domain.Transfer{PeerID: native.ID, ConversationID: tc.conv.ID}
			if err := neiwangtongTransferSizeError(&p.State, task, domain.File{Size: tc.size}); (err != nil) != tc.wantError {
				t.Fatalf("pending/recovery: %v", err)
			}
		})
	}
	p.State.Peers[0] = native
	task := domain.Transfer{PeerID: native.ID, ConversationID: direct.ID}
	for _, f := range []domain.File{{Size: limit, InlineImage: true}, {Size: limit, Shared: true}, {Size: limit, ShareFolder: true}} {
		if err := neiwangtongTransferSizeError(&p.State, task, f); err == nil {
			t.Fatalf("non-file attachment admitted: %+v", f)
		}
	}
	task.FromShare = true
	if err := neiwangtongTransferSizeError(&p.State, task, domain.File{Size: limit}); err == nil {
		t.Fatal("legacy shared transfer admitted")
	}
	task.FromShare = false
	if err := neiwangtongTransferSizeError(&p.State, task, domain.File{Size: limit, Folder: true}); err != nil {
		t.Fatalf("directory total incorrectly limited in place of individual entries: %v", err)
	}
}

func TestNeiwangtongLargeFileRestoresOfflineVersion(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(source, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	const peerID = "11111111111111111111111111111111"
	n := testNodeAt(t, "sender", dir)
	task := domain.Transfer{ID: "transfer", FileID: "file", PeerID: peerID, Direction: "send", Status: "offered",
		LegacyTransport: transportNWT, RemotePeerID: peerID, RemotePacket: "101", RemoteFile: "3511"}
	if err := n.change(func(s *domain.State) {
		peer := domain.Peer{ID: peerID, Name: "offline stock peer", Transport: transportNWT, Version: "3.4.3055"}
		s.Peers = append(s.Peers, peer)
		task.ConversationID = ensureDirect(s, peer)
		s.Transfers = append(s.Transfers, task)
		s.Files = append(s.Files, domain.File{ID: task.FileID, SourcePath: source, Size: 1<<32 + 65553})
	}); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = testNodeAt(t, "sender", dir)
	if err := n.setCompatibility(transportNWT, domain.CompatibilityConfig{Enabled: true, Port: freeCompatibilityPort(t)}); err != nil {
		t.Fatal(err)
	}
	state := n.snapshot(false)
	if p := state.Peer(peerID); p.Online || p.Version != "3.4.3055" {
		t.Fatalf("expected the last offline native profile, got %+v", p)
	}
	// A short real source with a persisted large size reaches validation and
	// proves the route was admitted without reading a 4 GiB file in unit CI.
	value := pending{PeerID: peerID, QueuedOffline: true, Envelope: envelope{Type: "file_offer", TransferID: task.ID}}
	if err := n.neiwangtongPending(peerID, value); err == nil || !strings.Contains(err.Error(), "源文件已变化") {
		t.Fatalf("offline replay did not validate the original source: %v", err)
	}
	n.restoreNeiwangtongFiles(peerID)
	state = n.snapshot(false)
	got := state.Transfers[transferIndex(&state, task.ID)]
	if !strings.Contains(got.Error, "源文件已变化") || got.RemotePacket != task.RemotePacket || got.RemoteFile != task.RemoteFile {
		t.Fatalf("restored original invite: %+v", got)
	}
	groupTask := task
	groupTask.ID, groupTask.ConversationID = "group-transfer", "group:room"
	if err := n.change(func(s *domain.State) {
		s.Conversations = append(s.Conversations, domain.Conversation{ID: groupTask.ConversationID, Kind: "group", GroupID: "room"})
		s.Transfers = append(s.Transfers, groupTask)
	}); err != nil {
		t.Fatal(err)
	}
	n.restoreNeiwangtongFiles(peerID)
	state = n.snapshot(false)
	if got := state.Transfers[transferIndex(&state, groupTask.ID)]; got.Status != "failed" || !strings.Contains(got.Error, "单聊") {
		t.Fatalf("old unverified group file restored: %+v", got)
	}
	if err := n.change(func(s *domain.State) { s.Peers[peerIndex(s, peerID)].Version = "3.4.3056" }); err != nil {
		t.Fatal(err)
	}
	if err := n.neiwangtongPending(peerID, value); err != nil {
		t.Fatal(err)
	}
	state = n.snapshot(false)
	if got := state.Transfers[transferIndex(&state, task.ID)]; got.Status != "failed" || !strings.Contains(got.Error, "4 GiB") {
		t.Fatalf("replay allowed an unverified replacement version: %+v", got)
	}
}
