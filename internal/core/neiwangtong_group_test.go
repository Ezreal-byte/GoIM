package core

import (
	"testing"

	"goim/internal/neiwangtong"
)

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
	roster.MembersVersion++
	roster.Members = roster.Members[:1]
	n.receiveNeiwangtongGroupRoster(owner, roster)
	if err := n.receiveNeiwangtongGroupText(owner, msg); err == nil || !n.Snapshot().Group(id).Left {
		t.Fatal("the removed local client kept receiving group messages")
	}
}
