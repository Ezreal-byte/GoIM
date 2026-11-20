package core

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"goim/internal/domain"
	ipmsgproto "goim/internal/ipmsg"
)

func TestLegacyTextAndFileCallbacksCreateDirectConversation(t *testing.T) {
	node, err := New(Options{
		DataDir: t.TempDir(), Listen: "127.0.0.1:0", Name: "local",
		DisableDiscovery: true, DisableCompatibility: true, Version: "test",
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
	if err := node.receiveLegacyMessage(peer.ID, 101, "hello", nil); err != nil {
		t.Fatal(err)
	}
	if err := node.receiveLegacyMessage(peer.ID, 102, "report.txt", []ipmsgproto.Attachment{{
		ID: 9, Name: "report.txt", Size: 128, Kind: 1, ModTime: time.Now().AddDate(-1, 0, 0).Unix(),
	}}); err != nil {
		t.Fatal(err)
	}
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

func TestLegacyMessageAndAttachmentsCommitTogetherOnRetry(t *testing.T) {
	dir := t.TempDir()
	n := testNodeAt(t, "local", dir)
	peer := ipmsgproto.Peer{ID: "ipmsg-test", Name: "Sender", Address: "192.0.2.10:2425", Online: true}
	n.receiveLegacyPeer(peer)
	files := []ipmsgproto.Attachment{{ID: 1, Name: "report.txt", Size: 12}}
	_ = n.db.Close()
	if err := n.receiveLegacyMessage(peer.ID, 42, "report caption", files); err == nil {
		t.Fatal("failed message persistence was reported as successful")
	}
	if s := n.Snapshot(); len(s.Messages) != 0 || len(s.Files) != 0 || len(s.Transfers) != 0 {
		t.Fatal("part of the failed packet entered persistent state")
	}
	_ = n.Close()
	n = testNodeAt(t, "local", dir)
	n.receiveLegacyPeer(peer)
	for range 2 {
		if err := n.receiveLegacyMessage(peer.ID, 42, "report caption", files); err != nil {
			t.Fatal(err)
		}
	}
	if s := n.Snapshot(); len(s.Messages) != 2 || len(s.Files) != 1 || len(s.Transfers) != 1 {
		t.Fatalf("retry did not commit one text and one attachment: %+v / %+v", s.Messages, s.Transfers)
	}
}

func TestUnboundLegacyHistorySurvivesWithoutReplayingSends(t *testing.T) {
	dir := t.TempDir()
	n := testNodeAt(t, "local", dir)
	const id = "ipmsg-0123456789abcdef"
	var conversation string
	if err := n.durable(func(p *persisted) []pending {
		peer := domain.Peer{ID: id, Name: "Previous login", Transport: legacyIPMSG}
		p.State.Peers = append(p.State.Peers, peer)
		conversation = ensureDirect(&p.State, peer)
		m := domain.Message{ID: newID(), ConversationID: conversation, SenderID: p.State.Self.ID, Text: "old outgoing", Kind: "text", Status: "pending"}
		addMessageState(&p.State, m)
		return []pending{{PeerID: id, Envelope: envelope{Type: "message", Message: m}}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = testNodeAt(t, "local", dir)
	messages := n.Snapshot().ChatMessages(conversation)
	if len(messages) != 1 || messages[0].Status != "failed" || messages[0].Text != "old outgoing" {
		t.Fatalf("unbound history = %+v", messages)
	}
	n.mu.Lock()
	queued := len(n.data.Outbox)
	n.mu.Unlock()
	if queued != 0 || n.sendText(conversation, "new message") == nil {
		t.Fatal("an unbound conversation still sends without selecting a logon")
	}
}

const repeatedZeroHash = "0000000000000000000000000000000000000000000000000000000000000000"

func TestLegacyMessageKeepsItsIdentityWhenClosedBeforeTheReceipt(t *testing.T) {
	remote, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	dir := t.TempDir()
	port := freeCompatibilityPort(t)
	starts := 0
	start := func() *Node {
		t.Helper()
		name := ""
		if starts == 0 {
			name = "receipt fixture"
		}
		starts++
		n := testNodeAt(t, name, dir)
		if err := n.applyIPMSG(domain.CompatibilityConfig{Enabled: true, Port: port}); err != nil {
			t.Fatal(err)
		}
		if _, err := remote.WriteToUDP([]byte("1:81:fixture:receipt-host:16777219:原版测试\x00"),
			net.UDPAddrFromAddrPort(netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(port)))); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the original IPMSG contact is online", func() bool {
			for _, peer := range n.Snapshot().Peers {
				if peer.Transport == legacyIPMSG && peer.Online {
					return true
				}
			}
			return false
		})
		return n
	}
	read := func() (string, string, *net.UDPAddr) {
		t.Helper()
		_ = remote.SetReadDeadline(time.Now().Add(3 * time.Second))
		var data [8192]byte
		for {
			count, from, err := remote.ReadFromUDP(data[:])
			if err != nil {
				t.Fatal(err)
			}
			fields := strings.SplitN(string(data[:count]), ":", 6)
			if len(fields) != 6 {
				t.Fatal("invalid IPMSG packet")
			}
			command, err := strconv.ParseUint(fields[4], 10, 32)
			if err != nil {
				t.Fatal(err)
			}
			if command&255 == 32 {
				return fields[1], fields[2], from
			}
		}
	}
	node := start()
	peer := node.Snapshot().Peers[0]
	conversation := directID(node.Snapshot().Self.ID, peer.ID)
	if err := node.change(func(s *domain.State) { ensureDirect(s, peer) }); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- node.sendText(conversation, "receipt restart fixture") }()
	first, user, _ := read()
	state := node.Snapshot()
	if messages := state.ChatMessages(conversation); len(messages) != 1 || messages[0].Status == "sent" {
		t.Fatal("the message was reported sent before the original acknowledged it")
	}
	if err := node.change(func(s *domain.State) { s.Settings.Nickname = "renamed fixture" }); err != nil {
		t.Fatal(err)
	}
	node.legacy.UpdateProfile("renamed fixture", false, false)
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	next := start()
	replayed, renamedUser, from := read()
	if replayed != first || renamedUser != user {
		t.Fatal("restart assigned a different wire packet to the same queued message")
	}
	if _, err := remote.WriteToUDP([]byte(fmt.Sprintf("1:92:fixture:receipt-host:33:%s\x00", first)), from); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the exact message completes after its real receipt", func() bool {
		messages := next.Snapshot().ChatMessages(conversation)
		return len(messages) == 1 && messages[0].Status == "sent"
	})
	next.mu.Lock()
	defer next.mu.Unlock()
	if len(next.data.Outbox) != 0 {
		t.Fatal("a confirmed IPMSG message remained in the outbox")
	}
}

// A reference peer first loses the invite, then acknowledges after a restart.
// Its original TCP identifiers must remain valid after another sender restart.
func TestLegacyFileOfferSurvivesRestartBeforeAndAfterReceipt(t *testing.T) {
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	dir := t.TempDir()
	port := freeCompatibilityPort(t)
	endpoint := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	start := func() *Node {
		n := testNodeAt(t, "", dir)
		if err := n.applyIPMSG(domain.CompatibilityConfig{Enabled: true, Port: port}); err != nil {
			t.Fatal(err)
		}
		if _, err := udp.WriteToUDP([]byte("1:81:file-fixture:file-host:16777219:Receiver\x00"), endpoint); err != nil {
			t.Fatal(err)
		}
		eventually(t, "IPMSG file recipient online", func() bool {
			peers := n.Snapshot().Peers
			return len(peers) == 1 && peers[0].Online
		})
		return n
	}
	invite := func() []byte {
		if err := udp.SetReadDeadline(time.Now().Add(4 * time.Second)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 8192)
		for {
			count, _, err := udp.ReadFromUDP(buf)
			if err != nil {
				t.Fatal(err)
			}
			fields := strings.SplitN(string(buf[:count]), ":", 6)
			if len(fields) != 6 {
				continue
			}
			cmd, _ := strconv.ParseUint(fields[4], 10, 32)
			if cmd&0xff == 32 && cmd&0x200000 != 0 {
				return bytes.Clone(buf[:count])
			}
		}
	}
	n := start()
	peer := n.Snapshot().Peers[0]
	if err := n.execute(domain.Command{Kind: domain.StartChat, PeerID: peer.ID}); err != nil {
		t.Fatal(err)
	}
	content := []byte("file content held across two sender restarts")
	path := filepath.Join(dir, "资料.txt")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	sent := make(chan error, 1)
	go func() { sent <- n.sendFile(directID(n.Snapshot().Self.ID, peer.ID), path, false, "") }()
	first := invite()
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	n = start()
	second := invite()
	if !bytes.Equal(first, second) {
		t.Fatalf("restarted invitation changed:\n%q\n%q", first, second)
	}
	packet := strings.SplitN(string(second), ":", 3)[1]
	if _, err := udp.WriteToUDP([]byte("1:84:file-fixture:file-host:33:"+packet+"\x00"), endpoint); err != nil {
		t.Fatal(err)
	}
	eventually(t, "matching invitation was acknowledged", func() bool {
		n.mu.Lock()
		defer n.mu.Unlock()
		for _, p := range n.data.Outbox {
			if p.Envelope.Type == "file_offer" {
				return false
			}
		}
		return true
	})
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = start()
	task := n.Snapshot().Transfers[0]
	number, id, err := legacyOfferIDs(task)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := verifiedLegacyOffer(t.Context(), nil, peer.ID, task, n.Snapshot().File(task.FileID))
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "same acknowledged offer registered on restart", func() bool { return n.legacy.VerifiedFile(peer.ID, expected) })
	conn, err := net.DialTimeout("tcp4", endpoint.String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(conn, "1:87:file-fixture:file-host:96:%x:%x:0:\x00", number, id); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(conn)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("restored download=%q, err=%v", got, err)
	}
	eventually(t, "original file completed", func() bool { return n.Snapshot().Transfers[0].Status == "completed" })
}

func TestLegacyQueuedOfferRejectsChangedSameSizeContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := fileHash(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = verifiedLegacyOffer(t.Context(), nil, "", domain.Transfer{RemotePacket: "123", RemoteFile: "0"}, domain.File{SourcePath: path, Size: 5, Hash: digest})
	if !errors.Is(err, errLegacySourceChanged) {
		t.Fatalf("changed file accepted: %v", err)
	}
}

func TestRejectedLegacyReceiveRetriesOriginalAttachment(t *testing.T) {
	original, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: freeCompatibilityPort(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	addr := original.Addr().(*net.TCPAddr)
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: addr.IP, Port: addr.Port})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	port := freeCompatibilityPort(t)
	n := testNode(t, "receiver")
	if err := n.applyIPMSG(domain.CompatibilityConfig{Enabled: true, Port: port}); err != nil {
		t.Fatal(err)
	}
	target := &net.UDPAddr{IP: addr.IP, Port: port}
	if _, err := udp.WriteToUDP([]byte("1:23:user:host:16777219:Sender\x00"), target); err != nil {
		t.Fatal(err)
	}
	if _, err := udp.WriteToUDP([]byte("1:42:user:host:2097440:item.txt\x009:item.txt:5:1:1:\a\x00"), target); err != nil {
		t.Fatal(err)
	}
	eventually(t, "incoming file", func() bool { return len(n.Snapshot().Transfers) == 1 })
	task := n.Snapshot().Transfers[0]
	if err := n.execute(domain.Command{Kind: domain.RejectTransfer, TransferID: task.ID}); err != nil {
		t.Fatal(err)
	}
	if err := n.execute(domain.Command{Kind: domain.RetryTransfer, TransferID: task.ID}); err != nil {
		t.Fatal(err)
	}
	_ = original.SetDeadline(time.Now().Add(4 * time.Second))
	conn, err := original.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	request, err := bufio.NewReader(conn).ReadString(0)
	if err != nil || !strings.HasSuffix(request, ":96:2a:9:0:\x00") {
		t.Fatalf("wrong retry request %q: %v", request, err)
	}
	if _, err := io.WriteString(conn, "hello"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "original receive completed", func() bool { tasks := n.Snapshot().Transfers; return len(tasks) == 1 && tasks[0].Status == "completed" })
	got, err := os.ReadFile(n.Snapshot().Transfers[0].Path)
	if err != nil || string(got) != "hello" {
		t.Fatalf("saved %q: %v", got, err)
	}
}
