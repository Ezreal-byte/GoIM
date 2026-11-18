package core

import (
	"fmt"
	"net"
	"net/netip"
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
