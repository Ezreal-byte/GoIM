package core

import (
	"testing"
	"time"

	ipmsgproto "goim/internal/ipmsg"
)

func TestLegacyTextAndFileCallbacksCreateDirectConversation(t *testing.T) {
	node, err := New(Options{
		DataDir: t.TempDir(), Listen: "127.0.0.1:0", Name: "local",
		DisableDiscovery: true, DisableIPMSG: true, Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = node.Close() })
	peer := ipmsgproto.Peer{
		ID: "ipmsg-test", Name: "飞秋用户", Host: "legacy-host",
		Address: "192.0.2.10:2425", Online: true,
	}
	node.receiveLegacyPeer(peer)
	initial := node.Snapshot()
	if len(initial.Conversations) != 0 {
		t.Fatal("discovery created a visible conversation")
	}
	node.receiveLegacyText(peer.ID, 101, "hello")
	node.receiveLegacyFiles(peer.ID, 102, "report", []ipmsgproto.Attachment{{
		ID: 9, Name: "report.txt", Size: 128, Kind: 1, ModTime: time.Now().AddDate(-1, 0, 0).Unix(),
	}})
	state := node.Snapshot()
	gotPeer := state.Peer(peer.ID)
	if gotPeer.Name != "飞秋用户" || gotPeer.Transport != legacyIPMSG || !gotPeer.Online {
		t.Fatalf("legacy peer = %+v", gotPeer)
	}
	conversationID := directID(state.Self.ID, peer.ID)
	if conversation := state.Conversation(conversationID); conversation.ID == "" || conversation.Title != "飞秋用户" {
		t.Fatalf("legacy conversation = %+v", conversation)
	}
	var textMessages, fileMessages int
	messages := state.ChatMessages(conversationID)
	for _, message := range messages {
		switch message.Kind {
		case "text":
			textMessages++
		case "file":
			fileMessages++
		}
	}
	if textMessages != 1 || fileMessages != 1 {
		t.Fatalf("messages = text:%d file:%d", textMessages, fileMessages)
	}
	if messages[1].CreatedAt.Before(messages[0].CreatedAt) {
		t.Fatal("incoming file card uses the source file's timestamp and would sort before the preceding text")
	}
	if len(state.Transfers) != 1 {
		t.Fatalf("transfers = %+v", state.Transfers)
	}
	transfer := state.Transfers[0]
	if transfer.LegacyTransport != legacyIPMSG || transfer.RemotePacket != "66" || transfer.RemoteFile != "9" {
		t.Fatalf("legacy transfer metadata = %+v", transfer)
	}
	if file := state.File(transfer.FileID); file.Name != "report.txt" || file.SenderID != peer.ID || file.Hash != repeatedZeroHash {
		t.Fatalf("legacy file = %+v", file)
	}
}

const repeatedZeroHash = "0000000000000000000000000000000000000000000000000000000000000000"
