package core

import (
	"bytes"
	"context"
	"crypto/md5"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"goim/internal/domain"
	"goim/internal/neiwangtong"
)

func testGoIMCreatedNeiwangtongGroup(t *testing.T, node *Node, remote *neiwangtong.Service, selfID, peerID string,
	rosters <-chan neiwangtong.GroupRoster, messages <-chan neiwangtong.GroupMessage, photos <-chan string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	const name = `原生群 & "同事"`
	if err := node.execute(domain.Command{Kind: domain.CreateGroup, Name: name, IDs: []string{peerID}}); err != nil {
		t.Fatal(err)
	}
	var roster neiwangtong.GroupRoster
	select {
	case roster = <-rosters:
		if roster.OwnerID != selfID || roster.Name != name || len(roster.Members) != 2 ||
			!slices.ContainsFunc(roster.Members, func(m neiwangtong.GroupMember) bool { return m.ID == peerID }) {
			t.Fatalf("unexpected native roster: %+v", roster)
		}
	case <-ctx.Done():
		t.Fatal("group was saved without announcing membership on the selected native route")
	}
	room := node.Snapshot().Group(nativeGroupID(roster.ID))
	if room.Transport != transportNWT || room.OwnerID != node.Snapshot().Self.ID || !contains(room.Members, peerID) {
		t.Fatalf("native and local group identities did not match: %+v", room)
	}
	if err := node.execute(domain.Command{Kind: domain.SetGroupAvatar, GroupID: room.ID,
		AvatarSize: 2, AvatarBGRA: make([]byte, 16)}); err != nil {
		t.Fatal(err)
	}
	photo := node.Snapshot().Group(room.ID)
	select {
	case digest := <-photos:
		if digest != photo.AvatarMD5 || digest == "" {
			t.Fatal("member received the wrong group photo revision")
		}
	case <-ctx.Done():
		t.Fatal("missing group photo announcement")
	}
	// Only the digest persists across a restart. The server must resolve
	// the cached PNG without relying on a desktop snapshot hydrating it.
	if err := node.change(func(s *domain.State) { s.Groups[groupIndex(s, room.ID)].Avatar = "" }); err != nil {
		t.Fatal(err)
	}
	data, err := remote.FetchGroupAvatar(ctx, selfID, photo.AvatarMD5)
	if err != nil || !bytes.Equal(data, groupAvatarBytes(photo)) {
		t.Fatalf("member could not retrieve the current owner photo: %v", err)
	}
	if got := node.serveNativeGroupAvatar("other-peer", photo.AvatarMD5); len(got) != 0 {
		t.Fatal("non-member received the photo")
	}
	conversation := "group:" + room.ID
	if err := node.sendText(conversation, "GoIM → 原版"); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-messages:
		if message.GroupID != roster.ID || message.Message.Text() != "GoIM → 原版" {
			t.Fatalf("unexpected outbound group message: %+v", message)
		}
	case <-ctx.Done():
		t.Fatal("native participant did not receive the group message")
	}
	if err := remote.SendGroupText(selfID, roster.ID, 333, newID(), "原版 → GoIM"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "reply is stored in the original GoIM group", func() bool {
		return slices.ContainsFunc(node.Snapshot().ChatMessages(conversation), func(m domain.Message) bool {
			return m.SenderID == peerID && m.Text == "原版 → GoIM"
		})
	})
	if err := remote.SendGroupExit(selfID, roster.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "native voluntary exit removes the member", func() bool {
		return !contains(node.Snapshot().Group(room.ID).Members, peerID)
	})
	if got := node.serveNativeGroupAvatar(peerID, photo.AvatarMD5); len(got) != 0 {
		t.Fatal("former member retained access")
	}
}

func testReceivedNativeGroupPhoto(t *testing.T, n *Node, remote *neiwangtong.Service, self, owner, digest string, data []byte) {
	t.Helper()
	roster := neiwangtong.GroupRoster{ID: newID(), OwnerID: owner, Name: "群头像", PhotoMD5: digest,
		InfoVersion: 2, MembersVersion: 2, Members: []neiwangtong.GroupMember{{ID: owner, Name: "群主"}, {ID: self, Name: "GoIM"}}}
	if err := remote.SendGroupRoster(self, roster); err != nil {
		t.Fatal(err)
	}
	id := nativeGroupID(roster.ID)
	eventually(t, "original owner photo cached", func() bool {
		g := n.Snapshot().Group(id)
		return g.AvatarMD5 == digest && g.Avatar != "" && len(groupAvatarBytes(g)) > 0
	})
	if err := n.execute(domain.Command{Kind: domain.SetGroupAvatar, GroupID: id, RemoveAvatar: true}); err == nil {
		t.Fatal("member could edit original-owned group")
	}
	n.receiveNativeGroupPhoto("other-peer", roster.ID, "", 5)
	if n.Snapshot().Group(id).Avatar == "" {
		t.Fatal("non-owner cleared the photo")
	}
	roster.InfoVersion, roster.PhotoMD5 = 3, ""
	if err := remote.SendGroupPhoto(self, roster); err != nil {
		t.Fatal(err)
	}
	eventually(t, "owner clear restores default", func() bool {
		g := n.Snapshot().Group(id)
		return g.Avatar == "" && g.AvatarMD5 == "" && g.AvatarHash == ""
	})
	n.mu.Lock()
	service := n.neiwangtong
	n.mu.Unlock()
	n.receiveNativeGroupPhoto(owner, roster.ID, digest, 2)
	n.cacheNativeGroupPhoto(nativeAvatarFetch{service, owner, id, digest}, data)
	if g := n.Snapshot().Group(id); g.Avatar != "" || g.AvatarMD5 != "" {
		t.Fatal("late response resurrected the cleared photo")
	}
}

const nativeFolderTestBlock = 32 << 10
const nativeFolderTestFile = "资料.bin"

func interruptNeiwangtongGroupFolder(t *testing.T, n *Node, remote *neiwangtong.Service, self, peer, task string,
	firstBlock <-chan struct{}, release func()) (domain.Transfer, string, neiwangtong.Directory) {
	t.Helper()
	defer release()
	room := neiwangtong.GroupRoster{ID: newID(), Name: "群目录续传", OwnerID: peer, InfoVersion: 1, MembersVersion: 1,
		Members: []neiwangtong.GroupMember{{ID: peer, Name: "Peer"}, {ID: self, Name: "GoIM"}}}
	if err := remote.SendGroupRoster(self, room); err != nil {
		t.Fatal(err)
	}
	eventually(t, "native group folder room", func() bool {
		return n.Snapshot().Group(nativeGroupID(room.ID)).ID != ""
	})
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, nativeFolderTestFile), bytes.Repeat([]byte("group folder test"), 16000), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := neiwangtong.InspectDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	share := newID()
	if err := remote.SendGroupFolder(self, room.ID, 9876, newID(), share, task, source); err != nil {
		t.Fatal(err)
	}
	var transfer domain.Transfer
	eventually(t, "the group folder invitation", func() bool {
		for _, candidate := range n.Snapshot().Transfers {
			if candidate.RemoteShareID == share && candidate.Direction == "receive" {
				transfer = candidate
				return true
			}
		}
		return false
	})
	dest := filepath.Join(t.TempDir(), "received")
	if err := n.execute(domain.Command{Kind: domain.AcceptTransfer, TransferID: transfer.ID, Paths: []string{dest}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstBlock:
	case <-time.After(5 * time.Second):
		t.Fatal("the sender did not write its first block")
	}
	eventually(t, "the first group folder block on disk", func() bool {
		s := n.Snapshot()
		i := transferIndex(&s, transfer.ID)
		if i < 0 {
			return false
		}
		transfer = s.Transfers[i]
		info, err := os.Stat(filepath.Join(nativeGroupFolderStage(transfer), "files", nativeFolderTestFile))
		return err == nil && info.Size() == nativeFolderTestBlock
	})
	n.mu.Lock()
	stop := n.transfers[transfer.ID]
	n.mu.Unlock()
	if stop == nil {
		t.Fatal("the receiver stopped before the planned interruption")
	}
	stop()
	eventually(t, "the interrupted receiver stopped", func() bool {
		n.mu.Lock()
		defer n.mu.Unlock()
		return n.transfers[transfer.ID] == nil
	})
	stage := nativeGroupFolderStage(transfer)
	if info, err := os.Stat(filepath.Join(stage, "files", nativeFolderTestFile)); err != nil || info.Size() != nativeFolderTestBlock {
		t.Fatalf("interrupting the receiver discarded saved data: %v", err)
	}
	return transfer, room.ID, source
}

func verifyNeiwangtongGroupFolderRetry(t *testing.T, n *Node, previous domain.Transfer, source neiwangtong.Directory, progress <-chan int64) {
	t.Helper()
	select {
	case got := <-progress:
		if got != 2*nativeFolderTestBlock {
			t.Fatalf("expected the sender to start after %d saved bytes, got first progress %d", nativeFolderTestBlock, got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the sender did not get a resumed group folder request after reconnecting")
	}
	eventually(t, "completed group folder and removed staging", func() bool {
		s := n.Snapshot()
		_, err := os.Stat(previous.FolderStage)
		i := transferIndex(&s, previous.ID)
		return i >= 0 && s.Transfers[i].Status == "completed" && s.Transfers[i].FolderStage == "" && os.IsNotExist(err)
	})
	want, err := os.ReadFile(filepath.Join(source.Path, nativeFolderTestFile))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(previous.Path, nativeFolderTestFile))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("resumed group folder data did not match: %v", err)
	}
}

func TestNativeGroupSelfExitDoesNotRestoreAccessFromStaleRoster(t *testing.T) {
	n := testNode(t, "GoIM group exit")
	self := n.Snapshot().Self.ID
	const owner = "10000000000000000000000000000001"
	const member = "20000000000000000000000000000002"
	const outsider = "30000000000000000000000000000003"
	const room = "40000000000000000000000000000004"
	for _, peer := range []string{owner, member, outsider} {
		n.receiveNeiwangtongPeer(neiwangtong.Peer{ID: peer, Name: peer, Online: true})
	}
	roster := neiwangtong.GroupRoster{ID: room, Name: "退群测试", OwnerID: owner, InfoVersion: 1, MembersVersion: 1,
		Members: []neiwangtong.GroupMember{{ID: owner}, {ID: member}, {ID: keyIDFromNative(nativeIDForGoIM(self))}}}
	id := nativeGroupID(room)
	n.receiveNeiwangtongGroupRoster(owner, roster)
	n.receiveNeiwangtongGroupDeparture(outsider, room, neiwangtong.CmdGroupExit)
	if group := n.Snapshot().Group(id); group.Left || !contains(group.Members, member) {
		t.Fatalf("an outsider changed the membership: %+v", group)
	}
	n.receiveNeiwangtongGroupDeparture(member, room, neiwangtong.CmdGroupDismiss)
	if n.Snapshot().Group(id).Dissolved {
		t.Fatal("a regular member dissolved someone else's group")
	}
	if err := n.manageGroup(domain.Command{Kind: domain.LeaveGroup, GroupID: id}); err != nil {
		t.Fatal(err)
	}
	roster.InfoVersion++ // the owner renamed it before learning of the departure
	n.receiveNeiwangtongGroupRoster(owner, roster)
	if group := n.Snapshot().Group(id); !group.Left || contains(group.Members, self) {
		t.Fatalf("a stale membership reactivated the session: %+v", group)
	}
}

func TestNativeGroupRosterAndMessageAuthorization(t *testing.T) {
	n := testNode(t, "GoIM group fixture")
	self := n.Snapshot().Self.ID
	const owner = "10000000000000000000000000000001"
	const outsider = "20000000000000000000000000000002"
	const room = "30000000000000000000000000000003"
	for _, peer := range []string{owner, outsider} {
		n.receiveNeiwangtongPeer(neiwangtong.Peer{ID: peer, Name: peer, Online: true, Address: "192.0.2.10:9012"})
	}
	roster := neiwangtong.GroupRoster{ID: room, Name: "测试群", OwnerID: owner, InfoVersion: 1, MembersVersion: 1,
		Members: []neiwangtong.GroupMember{{ID: owner, Name: "群主"}, {ID: keyIDFromNative(nativeIDForGoIM(self)), Name: "GoIM"}}}
	id := nativeGroupID(room)
	n.receiveNeiwangtongGroupRoster(outsider, roster)
	if n.Snapshot().Group(id).ID != "" {
		t.Fatal("a non-owner roster created a group")
	}
	n.receiveNeiwangtongGroupRoster(owner, roster)
	group := n.Snapshot().Group(id)
	if group.OwnerID != owner || group.Name != roster.Name || !contains(group.Members, self) {
		t.Fatalf("native group was not attached to the owner and local user: %+v", group)
	}
	msg := neiwangtong.GroupMessage{GroupID: room, Message: neiwangtong.DirectMessage{
		PacketID: 301, ID: "11111111111111111111111111111111", Parts: []neiwangtong.MessagePart{{Text: "真实群会话 & 中文"}},
	}}
	if err := n.receiveNeiwangtongGroupText(outsider, msg); err == nil {
		t.Fatal("an unrelated peer was allowed to post to the native group")
	}
	if err := n.receiveNeiwangtongGroupText(owner, msg); err != nil {
		t.Fatal(err)
	}
	msg.Message.PacketID++ // a replay can have another native packet number
	if err := n.receiveNeiwangtongGroupText(owner, msg); err != nil {
		t.Fatal(err)
	}
	if messages := n.Snapshot().ChatMessages("group:" + id); len(messages) != 1 || messages[0].Text != msg.Message.Text() {
		t.Fatalf("native group text missing or duplicated: %+v", messages)
	}
	if err := n.durable(func(p *persisted) []pending {
		conversation := "group:" + id
		p.Outbox["old-text"] = pending{PeerID: owner, Envelope: envelope{ID: "old-text", Type: "message", Message: domain.Message{ConversationID: conversation}}}
		p.Outbox["old-file"] = pending{PeerID: owner, Envelope: envelope{ID: "old-file", Type: "file_offer", File: domain.File{ConversationID: conversation}}}
		p.State.Transfers = append(p.State.Transfers, domain.Transfer{ID: "roster-revocation-task", ConversationID: conversation,
			Direction: "send", PeerID: owner, Status: "waiting"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	roster.MembersVersion++
	roster.Members = roster.Members[:1]
	n.receiveNeiwangtongGroupRoster(owner, roster)
	n.mu.Lock()
	_, oldText := n.data.Outbox["old-text"]
	_, oldFile := n.data.Outbox["old-file"]
	n.mu.Unlock()
	if oldText || oldFile || n.Snapshot().Transfers[0].Status != "cancelled" {
		t.Fatal("roster-only removal retained old group deliveries")
	}
	if err := n.receiveNeiwangtongGroupText(owner, msg); err == nil || !n.Snapshot().Group(id).Left {
		t.Fatal("the removed local client kept receiving group messages")
	}
	roster.MembersVersion++
	roster.Members = append(roster.Members, neiwangtong.GroupMember{ID: keyIDFromNative(nativeIDForGoIM(self))})
	n.receiveNeiwangtongGroupRoster(owner, roster)
	n.receiveNeiwangtongGroupDeparture(owner, room, neiwangtong.CmdGroupKick)
	roster.InfoVersion++
	n.receiveNeiwangtongGroupRoster(owner, roster)
	if err := n.receiveNeiwangtongGroupText(owner, msg); err == nil || !n.Snapshot().Group(id).Left {
		t.Fatal("a rename restored membership after the owner removed GoIM")
	}
	roster.MembersVersion++
	n.receiveNeiwangtongGroupRoster(owner, roster)
	n.receiveNeiwangtongGroupDeparture(owner, room, neiwangtong.CmdGroupDismiss)
	roster.InfoVersion++
	n.receiveNeiwangtongGroupRoster(owner, roster)
	if err := n.receiveNeiwangtongGroupText(owner, msg); err == nil || !n.Snapshot().Group(id).Dissolved {
		t.Fatal("an old roster reactivated the dissolved group")
	}
}

func testNeiwangtongGroupImages(t *testing.T, node *Node, remote *neiwangtong.Service, selfID, peerID string, messages <-chan neiwangtong.GroupMessage) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	room := newID()
	conversation := "group:" + nativeGroupID(room)
	node.receiveNeiwangtongGroupRoster(peerID, neiwangtong.GroupRoster{
		ID: room, Name: "图片测试群", OwnerID: peerID, InfoVersion: 1, MembersVersion: 1,
		Members: []neiwangtong.GroupMember{{ID: peerID, Name: "原版"}, {ID: selfID, Name: "GoIM"}},
	})

	input := nativeTestAvatar(t, 142)
	source := filepath.Join(t.TempDir(), "群图片.png")
	if err := os.WriteFile(source, input, 0600); err != nil {
		t.Fatal(err)
	}
	if err := remote.SendGroupImage(selfID, room, 301, newID(), 205, 3205, int64(len(input)), md5.Sum(input), source); err != nil {
		t.Fatal(err)
	}
	var downloaded string
	eventually(t, "the group image is available in the original group", func() bool {
		state := node.Snapshot()
		found := state.ChatMessages(conversation)
		if len(found) != 1 || found[0].Kind != "image" || found[0].SenderID != peerID {
			return false
		}
		for _, transfer := range state.Transfers {
			if transfer.ConversationID == conversation && transfer.FileID == found[0].FileID && transfer.Status == "completed" {
				downloaded = transfer.Path
				return true
			}
		}
		return false
	})
	if data, err := os.ReadFile(downloaded); err != nil || !bytes.Equal(data, input) {
		t.Fatalf("received group image differs from the fixture: %v", err)
	}

	if err := node.execute(domain.Command{Kind: domain.SendFiles, ConversationID: conversation, Paths: []string{source}}); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-messages:
		parts := message.Message.Parts
		if message.GroupID != room || len(parts) != 1 || parts[0].Image == nil {
			t.Fatalf("expected an inline image in the original group: %+v", message)
		}
		data, err := remote.FetchChatImage(ctx, selfID, *parts[0].Image)
		if err != nil || !bytes.Equal(data, input) {
			t.Fatalf("sending an image to the original group changed its bytes: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("test peer did not receive the group image")
	}
}
