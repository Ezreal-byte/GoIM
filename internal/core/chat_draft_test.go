package core

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"goim/internal/domain"
)

func TestMixedChatDraftDeliversInOrderAndKeepsUnsentSuffix(t *testing.T) {
	sender, receiver := testNode(t, "sender"), testNode(t, "receiver")
	connectNodes(t, sender, receiver)
	id := direct(t, sender, receiver)
	path := filepath.Join(t.TempDir(), "mixed.png")
	imageFile, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(png.Encode(imageFile, image.NewRGBA(image.Rect(0, 0, 3, 3))), imageFile.Close()); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.png")
	parts := []domain.ChatDraftPart{{Text: "前"}, {Path: path}, {Text: "后"}, {Path: missing}, {Text: "待发"}}
	if err := sender.execute(domain.Command{Kind: domain.SaveDraft, ConversationID: id, Text: domain.ChatDraftText(parts), DraftParts: parts}); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	sender.Submit(domain.Command{Kind: domain.SendChatDraft, ConversationID: id, DraftParts: parts, OnResult: func(err error) {
		result <- err
	}})
	eventually(t, "ordered submission completes", func() bool { return len(result) == 1 })
	var failure *domain.ChatDraftSendError
	if err := <-result; !errors.As(err, &failure) || failure.Accepted != 3 {
		t.Fatalf("unexpected batch result: %v", err)
	}
	if got := sender.Snapshot().Conversation(id).DraftParts; !slices.Equal(got, parts[3:]) {
		t.Fatalf("persisted draft includes accepted messages: %+v", got)
	}

	eventually(t, "recipient has all three messages", func() bool { return len(receiver.Snapshot().ChatMessages(id)) == 3 })
	for _, node := range []*Node{sender, receiver} {
		messages := node.Snapshot().ChatMessages(id)
		if len(messages) != 3 || messages[0].Text != "前" || messages[1].Kind != "image" || messages[2].Text != "后" {
			t.Fatalf("messages are out of composer order: %+v", messages)
		}
	}
}

func TestMixedChatDraftPreservesNewerDraftAndStopsWhenLocked(t *testing.T) {
	sender, receiver := testNode(t, "sender"), testNode(t, "receiver")
	connectNodes(t, sender, receiver)
	id := direct(t, sender, receiver)
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(path, []byte("attachment"), 0o600); err != nil {
		t.Fatal(err)
	}
	step := chatDraftStep{{Path: path}}
	newer := []domain.ChatDraftPart{{Text: "另一窗口正在编辑"}, {Path: path}}
	if err := sender.execute(domain.Command{Kind: domain.SaveDraft, ConversationID: id, Text: domain.ChatDraftText(newer), DraftParts: newer}); err != nil {
		t.Fatal(err)
	}
	if err := sender.sendChatDraft(id, step); err == nil {
		t.Fatal("stale draft passed the batch submission check")
	}
	// The other window may edit after a batch starts, while this file is read.
	if err := sender.sendFileWithLink(id, path, false, "", browserShare{}, step); err != nil {
		t.Fatal(err)
	}
	if err := sender.change(func(s *domain.State) { s.Locked = true }); err != nil {
		t.Fatal(err)
	}
	if err := sender.sendFileWithLink(id, path, false, "", browserShare{}, chatDraftStep(newer[1:])); err == nil {
		t.Fatal("the pending attachment was accepted after the application locked")
	}
	snapshot := sender.Snapshot()
	if got := snapshot.Conversation(id).DraftParts; !slices.Equal(got, newer) {
		t.Fatalf("pending send changed the newer draft: %+v", got)
	}
	if got := snapshot.ChatMessages(id); len(got) != 1 {
		t.Fatalf("message committed after lock: %+v", got)
	}
}
