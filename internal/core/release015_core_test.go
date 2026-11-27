package core

import (
	"crypto/tls"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestVoiceSignalsCannotControlAnotherPeersCall(t *testing.T) {
	n := testNode(t, "Receiver")
	caller := domain.Peer{ID: newID(), Name: "Caller", Online: true, Transport: domain.TransportGoIM, VoiceSupported: true}
	other := domain.Peer{ID: newID(), Name: "Other", Online: true, Transport: domain.TransportGoIM, VoiceSupported: true}
	if err := n.change(func(s *domain.State) { s.Peers = append(s.Peers, caller, other) }); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"accept", "reject", "busy", "end"} {
		t.Run(action, func(t *testing.T) {
			n.mu.Lock()
			n.closeVoiceLocked()
			callID := n.newVoiceSessionLocked(caller, domain.VoiceCallOutgoing, newID(), false).call.ID
			n.mu.Unlock()
			if err := n.receiveVoiceSignal(other.ID, domain.VoiceSignal{CallID: callID, Action: action}); err != nil {
				t.Fatal(err)
			}
			call := n.Snapshot().VoiceCall
			if call.ID != callID || call.PeerID != caller.ID || call.Status != domain.VoiceCallRinging {
				t.Fatalf("unrelated peer changed the call: %+v", call)
			}
			if err := n.receiveVoiceSignal(caller.ID, domain.VoiceSignal{CallID: callID, Action: action}); err != nil {
				t.Fatal(err)
			}
			if n.Snapshot().VoiceCall.Status == domain.VoiceCallRinging {
				t.Fatal("the actual participant's signal was ignored")
			}
		})
	}
}

func TestCloseInterruptsPeerWaitingForHello(t *testing.T) {
	n, client := testNode(t, "Server"), testNode(t, "Client")
	raw, err := net.Dial("tcp", n.Address())
	if err != nil {
		t.Fatal(err)
	}
	conn := tls.Client(raw, client.tls.Clone())
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := conn.Handshake(); err != nil {
		t.Fatal(err)
	}
	if _, err := readFrame(conn); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- n.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for a peer's hello deadline")
	}
}

func TestPendingPeerConnectionsAreBounded(t *testing.T) {
	n, peer := testNode(t, "Server"), testNode(t, "Peer")
	connectNodes(t, n, peer)
	n.mu.Lock()
	registered := len(n.incoming)
	n.mu.Unlock()
	if registered != 0 {
		t.Fatal("a registered messaging peer still consumes pending admission")
	}
	for range maxPendingPeerConnections {
		conn, err := net.Dial("tcp", n.Address())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
	}
	eventually(t, "pending connection budget filled", func() bool {
		n.mu.Lock()
		defer n.mu.Unlock()
		return len(n.incoming) == maxPendingPeerConnections
	})
	extra, err := net.Dial("tcp", n.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	_ = extra.SetReadDeadline(time.Now().Add(time.Second))
	var data [1]byte
	if _, err = extra.Read(data[:]); err != io.EOF {
		t.Fatalf("excess connection was not immediately closed: %v", err)
	}
	if !n.Snapshot().Peer(peer.Snapshot().Self.ID).Online {
		t.Fatal("pending connection limit disconnected an established peer")
	}
}

func TestPendingPrintBytesIncludesFilesWaitingForQueueCompletion(t *testing.T) {
	const size = int64(1234)
	for _, status := range []string{domain.PrintStatusReceiving, domain.PrintStatusSubmitting, domain.PrintStatusQueued, domain.PrintStatusCompleted, domain.PrintStatusFailed} {
		t.Run(status, func(t *testing.T) {
			job := domain.PrintJob{ID: newID(), Status: status}
			s := domain.State{
				PrintJobs: []domain.PrintJob{job},
				Transfers: []domain.Transfer{
					{ID: newID(), PrintJobID: job.ID, Direction: "receive", Status: "completed", Size: size},
					{ID: newID(), PrintJobID: job.ID, Direction: "send", Status: "offered", Size: size},
				},
			}
			want := size
			if job.Terminal() {
				want = 0
			}
			if got := pendingPrintBytes(&s); got != want {
				t.Fatalf("pending print bytes = %d, want %d", got, want)
			}
		})
	}
}

func TestTextRecallCannotRevokeAnotherSendersAttachment(t *testing.T) {
	for _, persistedText := range []bool{false, true} {
		t.Run(map[bool]string{false: "incoming", true: "already-stored"}[persistedText], func(t *testing.T) {
			n := testNode(t, "Receiver")
			selfID := n.Snapshot().Self.ID
			peer, third, groupID := newID(), newID(), newID()
			conversation := "group:" + groupID
			file := domain.File{ID: newID(), SenderID: selfID, ConversationID: conversation, Name: "keep.txt", SourcePath: "local-source"}
			message := domain.Message{ID: newID(), SenderID: peer, ConversationID: conversation, Kind: "text", Text: "text", FileID: file.ID, PrintJobID: newID()}
			if err := n.change(func(s *domain.State) {
				s.Groups = append(s.Groups, domain.Group{ID: groupID, Members: []string{selfID, peer, third}})
				s.Files = append(s.Files, file)
				s.Transfers = append(s.Transfers, domain.Transfer{ID: newID(), FileID: file.ID, ConversationID: conversation, Direction: "send", PeerID: third, Status: "offered"})
				if persistedText {
					s.Messages = append(s.Messages, message)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if !persistedText {
				if err := n.receiveMessage(peer, envelope{Message: message}); err != nil {
					t.Fatal(err)
				}
				stored := n.Snapshot().Messages[0]
				if stored.FileID != "" || stored.PrintJobID != "" {
					t.Fatal("text retained untrusted attachment references")
				}
			}
			if err := n.receiveMessageRecall(peer, envelope{ID: newID(), Message: message}); err != nil {
				t.Fatal(err)
			}
			s := n.Snapshot()
			if s.File(file.ID).ID == "" || s.Transfers[0].Revoked || s.Transfers[0].Status != "offered" {
				t.Fatal("a text recall revoked another sender's file")
			}
			if !s.Messages[0].Recalled {
				t.Fatal("the text itself was not recalled")
			}
		})
	}
}

func TestRecallKeepsSharedFileOffersInOtherConversations(t *testing.T) {
	file := domain.File{ID: newID(), SenderID: "self", ConversationID: "first", Shared: true}
	other := file
	other.ConversationID = "second"
	p := persisted{
		MessageRecalls: make(map[string]bool),
		State:          domain.State{Self: domain.Peer{ID: "self"}, Files: []domain.File{file}},
		Outbox: map[string]pending{
			"first":  {Envelope: envelope{Type: "file_offer", File: file}},
			"second": {Envelope: envelope{Type: "file_offer", File: other}},
		},
	}
	applyMessageRecall(&p, domain.Message{ID: file.ID, FileID: file.ID, SenderID: "self", ConversationID: "first", Kind: "file"})
	if _, exists := p.Outbox["first"]; exists {
		t.Fatal("the recalled conversation's offer survived")
	}
	if _, exists := p.Outbox["second"]; !exists {
		t.Fatal("recall removed a shared file offer in another conversation")
	}
}

func TestPrintCleanupFollowsSuccessfulStatusCommit(t *testing.T) {
	for _, saveFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "save-failed"}[saveFails], func(t *testing.T) {
			n := testNode(t, "Owner")
			job := domain.PrintJob{ID: newID(), TransferID: newID(), PrinterOwnerID: n.Snapshot().Self.ID, FileName: "print.pdf", Status: domain.PrintStatusReceiving}
			destination := printReceivePath(n.dir, job)
			partial := filepath.Join(n.dir, "partials", job.TransferID+".part")
			for _, path := range []string{destination, partial} {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("print content"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := n.change(func(s *domain.State) {
				s.PrintJobs = append(s.PrintJobs, job)
				s.Transfers = append(s.Transfers, domain.Transfer{ID: job.TransferID, PrintJobID: job.ID, Direction: "receive", Status: "failed", Path: destination})
			}); err != nil {
				t.Fatal(err)
			}
			if saveFails {
				if err := n.db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			n.finishPrintJob(job.ID, domain.PrintStatusFailed, "transfer failed")
			for _, path := range []string{destination, partial} {
				_, err := os.Stat(path)
				if saveFails && err != nil {
					t.Fatalf("uncommitted failure removed recoverable content: %v", err)
				}
				if !saveFails && !os.IsNotExist(err) {
					t.Fatalf("completed print cleanup left content: %v", err)
				}
			}
		})
	}
}
