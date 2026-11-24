package core

import (
	"context"
	"net"
	"testing"

	"goim/internal/domain"
	"goim/internal/remotedesktop"
)

func TestRemoteDesktopHistoryKeepsDraftAndSurvivesRestart(t *testing.T) {
	engine := func(ctx context.Context, _ net.Conn, _ remotedesktop.Start, _ <-chan domain.RemoteDesktopInput, events remotedesktop.Events) error {
		events.Ready()
		<-ctx.Done()
		return nil
	}
	viewer, host := remoteTestNode(t, "viewer", engine), remoteTestNode(t, "host", engine)
	connectNodes(t, viewer, host)
	hostID := host.Snapshot().Self.ID
	conversationID := direct(t, viewer, host)
	if err := viewer.execute(domain.Command{Kind: domain.SaveDraft, ConversationID: conversationID, Text: "unfinished"}); err != nil {
		t.Fatal(err)
	}

	if err := viewer.startRemoteDesktop(hostID); err != nil {
		t.Fatal(err)
	}
	cancelled := waitRemote(t, host, domain.RemoteDesktopPending)
	if err := viewer.stopRemoteDesktop(cancelled.ID, "ended"); err != nil {
		t.Fatal(err)
	}
	waitRemote(t, host, domain.RemoteDesktopEnded)

	if err := viewer.startRemoteDesktop(hostID); err != nil {
		t.Fatal(err)
	}
	active := waitRemote(t, host, domain.RemoteDesktopPending)
	if err := host.acceptRemoteDesktop(active.ID, true, false); err != nil {
		t.Fatal(err)
	}
	waitRemote(t, viewer, domain.RemoteDesktopActive)
	waitRemote(t, host, domain.RemoteDesktopActive)
	if err := viewer.stopRemoteDesktop(active.ID, "ended"); err != nil {
		t.Fatal(err)
	}
	waitRemote(t, host, domain.RemoteDesktopEnded)

	for _, node := range []*Node{viewer, host} {
		snapshot := node.Snapshot()
		if len(snapshot.Messages) != 2 {
			t.Fatalf("expected one record per request, got %+v", snapshot.Messages)
		}
		for i, event := range []struct {
			id, reason string
			connected  bool
		}{{cancelled.ID, "cancelled", false}, {active.ID, "ended", true}} {
			message := snapshot.Messages[i]
			record := message.RemoteDesktop
			if message.ID != "remote-desktop:"+event.id || message.ConversationID != conversationID ||
				message.Kind != "system" || record == nil || record.Incoming != (node == host) ||
				record.Reason != event.reason || record.Connected != event.connected {
				t.Fatalf("record %d lost remote desktop facts: %+v, %+v", i, message, record)
			}
		}
	}
	if got := viewer.Snapshot().Conversation(conversationID).Draft; got != "unfinished" {
		t.Fatalf("remote desktop history changed the draft to %q", got)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := testNodeAt(t, "host", host.DataDir())
	messages := restarted.Snapshot().Messages
	if len(messages) != 2 || messages[1].RemoteDesktop == nil || !messages[1].RemoteDesktop.Connected {
		t.Fatalf("restart lost remote desktop history: %+v", messages)
	}
}
