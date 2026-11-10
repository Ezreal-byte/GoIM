package core

import (
	"os"
	"path/filepath"
	"testing"

	"goim/internal/domain"
)

func TestRecallReachesOfflinePeerAndRejectsReplay(t *testing.T) {
	a, b := testNode(t, "发送者"), testNode(t, "接收者")
	connectNodes(t, a, b)
	peer := b.Snapshot().Self.ID
	conv := direct(t, a, b)
	if err := a.sendText(conv, "离线撤回原文"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "original delivered", func() bool { return len(b.Snapshot().ChatMessages(conv)) == 1 })
	m := a.Snapshot().ChatMessages(conv)[0]
	dir := b.dir
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.execute(domain.Command{Kind: domain.RecallMessage, ConversationID: conv, MessageID: m.ID}); err != nil {
		t.Fatal(err)
	}
	if got := a.Snapshot().ChatMessages(conv)[0]; !got.Recalled || got.Text != "" {
		t.Fatalf("sender did not recall: %+v", got)
	}

	b = testNodeAt(t, "接收者", dir)
	connectNodes(t, a, b)
	eventually(t, "recipient recall", func() bool {
		messages := b.Snapshot().ChatMessages(conv)
		return len(messages) == 1 && messages[0].Recalled && messages[0].Text == ""
	})
	a.reliable(peer, envelope{Type: "message", Message: m})
	if err := a.sendText(conv, "后续消息"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "message after replay", func() bool { return len(b.Snapshot().ChatMessages(conv)) == 2 })
	got := b.Snapshot().ChatMessages(conv)
	if !got[0].Recalled || got[0].Text != "" {
		t.Fatal("a late original restored a recalled message")
	}

	if err := b.execute(domain.Command{Kind: domain.DeleteMessage, ConversationID: conv, MessageID: got[1].ID}); err != nil {
		t.Fatal(err)
	}
	if got := b.Snapshot().ChatMessages(conv); len(got) != 1 {
		t.Fatalf("local deletion did not remove the message: %v", got)
	}
	if got := a.Snapshot().ChatMessages(conv); len(got) != 2 {
		t.Fatal("local deletion changed the other participant's history")
	}
}

func TestRecallFileCancelsOfferWithoutDeletingOriginal(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	conv := direct(t, a, b)
	source := filepath.Join(t.TempDir(), "example.txt")
	if err := os.WriteFile(source, []byte("original file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.sendFile(conv, source, false, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "file offer", func() bool { return len(b.Snapshot().Transfers) == 1 })
	m := a.Snapshot().ChatMessages(conv)[0]
	if err := a.execute(domain.Command{Kind: domain.RecallMessage, ConversationID: conv, MessageID: m.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "remote file recall", func() bool {
		s := b.Snapshot()
		return s.File(m.FileID).ID == "" && s.Transfers[0].Revoked && s.Transfers[0].Status == "cancelled" && s.ChatMessages(conv)[0].Recalled
	})
	if data, err := os.ReadFile(source); err != nil || string(data) != "original file" {
		t.Fatal("recall changed the source file")
	}
}

func TestRecallBeforeOriginalDoesNotCreateVisibleHistory(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	conv := direct(t, a, b)
	m := domain.Message{ID: newID(), SenderID: a.Snapshot().Self.ID, ConversationID: conv, Kind: "text", Text: "迟到的原文"}
	if err := b.receiveMessageRecall(m.SenderID, envelope{ID: newID(), Message: m}); err != nil {
		t.Fatal(err)
	}
	if err := b.receiveMessage(m.SenderID, envelope{Message: m}); err != nil {
		t.Fatal(err)
	}
	if got := b.Snapshot().ChatMessages(conv); len(got) != 0 {
		t.Fatalf("recall marker did not suppress the delayed message: %v", got)
	}
}

func TestGroupRecallUsesOriginalRecipients(t *testing.T) {
	a, b, c := testNode(t, "群主"), testNode(t, "成员一"), testNode(t, "成员二")
	id := createTestGroup(t, a, b, c)
	conv := "group:" + id
	if err := a.sendText(conv, "群聊撤回测试"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "group message delivery", func() bool {
		return len(b.Snapshot().ChatMessages(conv)) == 1 && len(c.Snapshot().ChatMessages(conv)) == 1
	})
	m := a.Snapshot().ChatMessages(conv)[0]
	if err := b.recallMessage(conv, m.ID); err == nil {
		t.Fatal("another member recalled the sender's message")
	}
	if err := a.recallMessage(conv, m.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "recall across members", func() bool {
		return b.Snapshot().ChatMessages(conv)[0].Recalled && c.Snapshot().ChatMessages(conv)[0].Recalled
	})
}
