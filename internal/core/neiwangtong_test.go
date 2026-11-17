package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goim/internal/domain"
	"goim/internal/neiwangtong"
)

func TestNeiwangtongCompatibilitySettingsAndLifecycle(t *testing.T) {
	n := testNode(t, "local")
	initial := n.Snapshot()
	if !initial.Settings.Neiwangtong.Enabled || initial.NeiwangtongStatus.Active {
		t.Fatal("default or test isolation is wrong")
	}
	cfg := domain.CompatibilityConfig{Enabled: true, Port: freeCompatibilityPort(t)}
	if err := n.setCompatibility(transportNWT, cfg); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	service := n.neiwangtong
	n.mu.Unlock()
	blocker, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	if err := n.setCompatibility(transportNWT, domain.CompatibilityConfig{Enabled: true, Port: blocker.LocalAddr().(*net.UDPAddr).Port}); err == nil {
		t.Fatal("saved an unavailable discovery port")
	}
	if s := n.Snapshot(); s.Settings.Neiwangtong != cfg || !s.NeiwangtongStatus.Active || s.NeiwangtongStatus.Problem == "" {
		t.Fatalf("working config was lost: %+v", s.NeiwangtongStatus)
	}
	cfg.Port = freeCompatibilityPort(t)
	if err := n.setCompatibility(transportNWT, cfg); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	same := n.neiwangtong == service
	n.mu.Unlock()
	if !same || n.Snapshot().Settings.Neiwangtong != cfg {
		t.Fatal("changing discovery restarted the control and TCP listeners")
	}
	cfg.Enabled = false
	if err := n.setCompatibility(transportNWT, cfg); err != nil {
		t.Fatal(err)
	}
	if s := n.Snapshot(); s.Settings.Neiwangtong.Enabled || s.NeiwangtongStatus.Active {
		t.Fatal("failed to stop only the native adapter")
	}
}

func TestNeiwangtongRecallScopesSenderAndSuppressesReplay(t *testing.T) {
	n := testNode(t, "recall fixture")
	const first = "11111111111111111111111111111111"
	const second = "22222222222222222222222222222222"
	const sharedID = "123456789abcdef0123456789abcdef0"
	for _, id := range []string{first, second} {
		n.receiveNeiwangtongPeer(neiwangtong.Peer{ID: id, Name: id, Online: true})
	}

	receive := func(peer string, m neiwangtong.DirectMessage) {
		t.Helper()
		if err := n.receiveNeiwangtongMessage(peer, m); err != nil {
			t.Fatal(err)
		}
	}
	message := neiwangtong.DirectMessage{ID: sharedID, Parts: []neiwangtong.MessagePart{{Text: "same ID on separate original clients"}}}
	receive(first, message)
	receive(second, message)
	receive(first, neiwangtong.DirectMessage{RecallID: sharedID})
	receive(first, message)
	s := n.Snapshot()
	chat1, chat2 := s.ChatMessages(directID(s.Self.ID, first)), s.ChatMessages(directID(s.Self.ID, second))
	if len(chat1) != 1 || !chat1[0].Recalled || chat1[0].Text != "" ||
		len(chat2) != 1 || chat2[0].Recalled || chat2[0].Text != message.Text() {
		t.Fatalf("recall or replay affected the wrong messages: %+v; %+v", chat1, chat2)
	}
}

func TestNeiwangtongOutgoingFileIDsAreStableAndDistinct(t *testing.T) {
	transfer := domain.Transfer{ID: "0123456789abcdef0123456789abcdef", LegacyTransport: transportNWT}
	taskID, fileID := neiwangtongOutgoingIDs(transfer)
	if taskID == 0 || fileID == 0 || taskID == fileID || taskID > 0xffffffff || fileID > 0xffffffff {
		t.Fatalf("invalid native transfer IDs: task=%d file=%d", taskID, fileID)
	}
	transfer.RemotePacket, transfer.RemoteFile = fmt.Sprint(taskID), fmt.Sprint(fileID)
	nextTask, nextFile := neiwangtongOutgoingIDs(transfer)
	if nextTask != taskID || nextFile != fileID {
		t.Fatalf("retry changed IDs: before=%d/%d after=%d/%d", taskID, fileID, nextTask, nextFile)
	}
	legacy := domain.Transfer{ID: transfer.ID, RemotePacket: "70000", RemoteFile: "70000", LegacyTransport: transportNWT}
	legacyTask, legacyFile := neiwangtongOutgoingIDs(legacy)
	if legacyTask != 70000 || legacyFile == legacyTask {
		t.Fatalf("legacy same-ID transfer was not migrated: task=%d file=%d", legacyTask, legacyFile)
	}
}

func TestNeiwangtongStatusValuesMatchNativeMenu(t *testing.T) {
	n := testNode(t, "status mapping")
	cases := []struct {
		status uint32
		want   domain.Presence
	}{
		{neiwangtong.StatusOnline, domain.PresenceOnline},
		{neiwangtong.StatusBusy, domain.PresenceBusy},
		{neiwangtong.StatusRightBack, domain.PresenceAway},
		{neiwangtong.StatusAway, domain.PresenceAway},
	}
	for i, tc := range cases {
		id := fmt.Sprintf("status-%d", i)
		n.receiveNeiwangtongPeer(neiwangtong.Peer{
			ID: id, Name: id, Address: "192.0.2.10:9012", Online: true, Status: tc.status,
		})
		if got := n.Snapshot().Peer(id).Presence; got != tc.want {
			t.Fatalf("native status %d mapped to %q, want %q", tc.status, got, tc.want)
		}
	}
}

func nativeTestAvatar(t *testing.T, blue uint8) []byte {
	t.Helper()
	p := image.NewRGBA(image.Rect(0, 0, 24, 24))
	p.SetRGBA(12, 12, color.RGBA{B: blue, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, p); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestNeiwangtongDirectCommunication(t *testing.T) {
	n := testNode(t, "GoIM core")
	port := freeCompatibilityPort(t)
	if err := n.setCompatibility(transportNWT, domain.CompatibilityConfig{Enabled: true, Port: port}); err != nil {
		t.Fatal(err)
	}
	remoteID := [16]byte{10}
	localIP := netip.MustParseAddr("127.0.0.1")
	remoteIP := netip.MustParseAddr("127.0.0.2")
	controlProbe, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(remoteIP, 9012)))
	if err != nil {
		t.Skipf("platform does not provide a second loopback IPv4 address: %v", err)
	}
	_ = controlProbe.Close()
	transferProbe, err := net.Listen("tcp4", net.TCPAddrFromAddrPort(netip.AddrPortFrom(remoteIP, 9013)).String())
	if err != nil {
		t.Skipf("secondary loopback file port is unavailable: %v", err)
	}
	_ = transferProbe.Close()
	outgoing := make(chan string, 2)
	offers := make(chan neiwangtong.XMLSendFile, 2)
	folders := make(chan neiwangtong.FolderOffer, 2)
	groupChat := make(chan neiwangtong.GroupMessage, 2)
	groupRosters := make(chan neiwangtong.GroupRoster, 2)
	withdrawals := make(chan uint64, 2)
	profile := neiwangtong.Peer{Name: "Peer", Sign: "签名", Company: "测试组"}
	remoteCfg := neiwangtong.Config{
		ID: remoteID, Profile: profile, AvatarPNG: nativeTestAvatar(t, 30), BindIP: remoteIP, DisableBroadcast: true, EnableFolders: true,
		DiscoveryPort: freeCompatibilityPort(t), ControlPort: 9012, RemoteControlPort: 9012, TransferPort: 9013,
		Callbacks: neiwangtong.Callbacks{
			Text:  func(_ string, _ uint64, text string) { outgoing <- text },
			Files: func(_ string, offer neiwangtong.XMLSendFile) { offers <- offer },
			GroupText: func(_ string, message neiwangtong.GroupMessage) error {
				groupChat <- message
				return nil
			},
			GroupRoster: func(_ string, roster neiwangtong.GroupRoster) { groupRosters <- roster },
			Folders: func(_ string, offer neiwangtong.FolderOffer) {
				folders <- offer
			},
			FileCancelled: func(_ string, taskID uint64) {
				withdrawals <- taskID
			},
		},
	}
	remote, err := neiwangtong.Start(remoteCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := remote.Probe(ctx, netip.AddrPortFrom(localIP, uint16(port)), 9012, 9013); err != nil {
		t.Fatal(err)
	}
	// Remote uses a separate loopback control port, so explicitly complete
	// the reverse query just as two real clients query one another.
	n.mu.Lock()
	local := n.neiwangtong
	n.mu.Unlock()
	if err := local.Probe(ctx, netip.AddrPortFrom(remoteIP, uint16(remoteCfg.DiscoveryPort)), remoteCfg.ControlPort, remoteCfg.TransferPort); err != nil {
		t.Fatal(err)
	}
	nativeID := keyIDFromNative(remoteID)
	eventually(t, "native profile", func() bool {
		p := n.Snapshot().Peer(nativeID)
		return p.Online && p.Transport == transportNWT && p.Signature == "签名"
	})
	eventually(t, "first avatar", func() bool { return n.Snapshot().Peer(nativeID).Avatar != "" })
	first := n.Snapshot().Peer(nativeID).Avatar
	selfID := keyIDFromNative(nativeIDForGoIM(n.Snapshot().Self.ID))
	testGoIMCreatedNeiwangtongGroup(t, n, remote, selfID, nativeID, groupRosters, groupChat)
	if err := remote.SendText(selfID, "原生文本 & 😀"); err != nil {
		t.Fatal(err)
	}
	conv := directID(n.Snapshot().Self.ID, nativeID)
	eventually(t, "native message", func() bool {
		msgs := n.Snapshot().ChatMessages(conv)
		return len(msgs) == 1 && msgs[0].Text == "原生文本 & 😀"
	})
	if err := n.sendText(conv, "回复 & 中文"); err != nil {
		t.Fatal(err)
	}
	select {
	case reply := <-outgoing:
		if reply != "回复 & 中文" {
			t.Fatalf("reply: %q", reply)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	path := filepath.Join(t.TempDir(), "原生文件.txt")
	content := []byte("independent loopback file")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	// The original 3.4 client can advertise the full Windows path, even
	// when the receiver is running on a different OS.
	if err := remote.SendFileOffer(selfID, 777, `C:\goim-qa\原生文件.txt`, int64(len(content)), time.Now().Unix(), path); err != nil {
		t.Fatal(err)
	}
	eventually(t, "file offer", func() bool { return len(n.Snapshot().Transfers) == 1 })
	task := n.Snapshot().Transfers[0]
	if task.LegacyTransport != transportNWT || task.Status != "offered" || task.RemotePeerID != nativeID || task.Name != filepath.Base(path) {
		t.Fatalf("offer: %+v", task)
	}
	eventually(t, "file card in chat", func() bool {
		for _, message := range n.Snapshot().ChatMessages(conv) {
			if message.Kind == "file" && message.FileID == task.FileID && message.Text == filepath.Base(path) {
				return true
			}
		}
		return false
	})
	dest := filepath.Join(t.TempDir(), "received.txt")
	if err := n.execute(domain.Command{Kind: domain.AcceptTransfer, TransferID: task.ID, Paths: []string{dest}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(6 * time.Second)
	for n.Snapshot().Transfers[0].Status != "completed" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if completed := n.Snapshot().Transfers[0]; completed.Status != "completed" {
		t.Fatalf("complete native file: %+v", completed)
	}
	got, err := os.ReadFile(dest)
	if err != nil || sha256.Sum256(got) != sha256.Sum256(content) {
		t.Fatalf("file verification: %v", err)
	}
	outgoingFile := filepath.Join(t.TempDir(), "GoIM 发出的文件.txt")
	outgoingBytes := []byte("GoIM 至内网通的文件内容")
	if err := os.WriteFile(outgoingFile, outgoingBytes, 0600); err != nil {
		t.Fatal(err)
	}
	nextOffer := func() neiwangtong.XMLSendFile {
		t.Helper()
		if err := n.execute(domain.Command{Kind: domain.SendFiles, ConversationID: conv, Paths: []string{outgoingFile}}); err != nil {
			t.Fatal(err)
		}
		select {
		case offer := <-offers:
			if offer.Name != filepath.Base(outgoingFile) || offer.Size != int64(len(outgoingBytes)) ||
				offer.TaskID == 0 || offer.FileID == 0 || offer.TaskID == offer.FileID {
				t.Fatalf("GoIM file offer: %+v", offer)
			}
			return offer
		case <-ctx.Done():
			t.Fatalf("GoIM file offer: %v", ctx.Err())
			return neiwangtong.XMLSendFile{}
		}
	}
	awaitSentTransfer := func(offer neiwangtong.XMLSendFile, status string) {
		t.Helper()
		eventually(t, "GoIM outgoing native transfer "+status, func() bool {
			for _, transfer := range n.Snapshot().Transfers {
				if transfer.Direction == "send" && transfer.LegacyTransport == transportNWT &&
					transfer.RemotePacket == fmt.Sprint(offer.TaskID) && transfer.RemoteFile == fmt.Sprint(offer.FileID) &&
					transfer.Status == status {
					return status != "completed" || transfer.Done == transfer.Size
				}
			}
			return false
		})
	}
	offer := nextOffer()
	awaitSentTransfer(offer, "offered")
	peerDest := filepath.Join(t.TempDir(), offer.Name)
	if err := remote.AcceptFile(ctx, selfID, offer.TaskID, offer.FileID, offer.Size, peerDest, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(peerDest); err != nil || sha256.Sum256(got) != sha256.Sum256(outgoingBytes) {
		t.Fatalf("peer file SHA-256 mismatch: %v", err)
	}
	awaitSentTransfer(offer, "completed")

	interrupted := nextOffer()
	n.receiveNeiwangtongProgress(nativeID, interrupted.TaskID, 1, interrupted.Size, false, errors.New("test connection dropped"))
	awaitSentTransfer(interrupted, "failed")
	var retryID string
	for _, transfer := range n.Snapshot().Transfers {
		if transfer.Direction == "send" && transfer.RemotePacket == fmt.Sprint(interrupted.TaskID) {
			retryID = transfer.ID
			break
		}
	}
	if err := n.execute(domain.Command{Kind: domain.RetryTransfer, TransferID: retryID}); err != nil {
		t.Fatal(err)
	}
	select {
	case retry := <-offers:
		if retry.TaskID != interrupted.TaskID || retry.FileID != interrupted.FileID {
			t.Fatalf("retry issued a new file identity: before=%+v after=%+v", interrupted, retry)
		}
	case <-ctx.Done():
		t.Fatal("the same file offer was not retried")
	}

	rejected := nextOffer()
	if rejected.TaskID == offer.TaskID || rejected.FileID == offer.FileID {
		t.Fatalf("new offer reused selectors: %+v", rejected)
	}
	if err := remote.StopReceiving(selfID, rejected.TaskID); err != nil {
		t.Fatal(err)
	}
	awaitSentTransfer(rejected, "rejected")

	// Task IDs are chosen by the sender. The same ID can be in flight in
	// both directions without a withdrawal touching the other transfer.
	waiting := nextOffer()
	if err := remote.SendFileOffer(selfID, waiting.TaskID, "对方取消.txt", int64(len(content)), time.Now().Unix(), path); err != nil {
		t.Fatal(err)
	}
	var incomingID, outgoingID string
	eventually(t, "two independent file offers", func() bool {
		for _, transfer := range n.Snapshot().Transfers {
			if transfer.RemotePacket == fmt.Sprint(waiting.TaskID) && transfer.Status == "offered" {
				if transfer.Direction == "receive" {
					incomingID = transfer.ID
				} else {
					outgoingID = transfer.ID
				}
			}
		}
		return incomingID != "" && outgoingID != ""
	})
	if err := remote.CancelSend(selfID, waiting.TaskID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "cancelled incoming native offer", func() bool {
		for _, transfer := range n.Snapshot().Transfers {
			if transfer.ID == incomingID {
				return transfer.Status == "cancelled"
			}
		}
		return false
	})
	awaitSentTransfer(waiting, "offered")
	if err := n.execute(domain.Command{Kind: domain.CancelTransfer, TransferID: outgoingID}); err != nil {
		t.Fatal(err)
	}
	awaitSentTransfer(waiting, "cancelled")
	select {
	case taskID := <-withdrawals:
		if taskID != waiting.TaskID {
			t.Fatalf("wrong native offer cancelled: %d", taskID)
		}
	case <-ctx.Done():
		t.Fatalf("peer did not receive withdrawal: %v", ctx.Err())
	}

	profile.Name = "Updated"
	remote.UpdateProfile(profile, nativeTestAvatar(t, 90))
	eventually(t, "changed avatar and profile", func() bool {
		p := n.Snapshot().Peer(nativeID)
		return p.Avatar != "" && p.Avatar != first && p.Name == "Updated"
	})
	remote.UpdateProfile(profile, nil)
	eventually(t, "removed avatar", func() bool { return n.Snapshot().Peer(nativeID).Avatar == "" })
	testNeiwangtongDirectories(t, n, remote, selfID, nativeID, conv, folders)
	testNeiwangtongGroupImages(t, n, remote, selfID, nativeID, groupChat)

	if err := remote.SendQuit(selfID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "native peer went offline", func() bool { return !n.Snapshot().Peer(nativeID).Online })
	const offlineBody = "重启后补投的消息"
	if err := n.sendText(conv, offlineBody); err != nil {
		t.Fatal(err)
	}
	if err := n.sendFile(conv, outgoingFile, false, ""); err != nil {
		t.Fatal(err)
	}
	before := n.Snapshot().ChatMessages(conv)
	if len(before) < 2 || before[len(before)-2].Text != offlineBody || before[len(before)-2].Status != "pending" {
		t.Fatal("text was not kept pending for the offline peer")
	}
	if err := remote.Close(); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := testNodeAt(t, "GoIM core", n.dir)
	if err := reopened.setCompatibility(transportNWT, domain.CompatibilityConfig{Enabled: true, Port: port}); err != nil {
		t.Fatal(err)
	}
	again, err := neiwangtong.Start(remoteCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	reconnect, stop := context.WithTimeout(context.Background(), 12*time.Second)
	defer stop()
	if err := again.Probe(reconnect, netip.AddrPortFrom(localIP, uint16(port)), 9012, 9013); err != nil {
		t.Fatal(err)
	}
	reopened.mu.Lock()
	restarted := reopened.neiwangtong
	reopened.mu.Unlock()
	if err := restarted.Probe(reconnect, netip.AddrPortFrom(remoteIP, uint16(remoteCfg.DiscoveryPort)), 9012, 9013); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-outgoing:
		if got != offlineBody {
			t.Fatalf("queued text differs: %q", got)
		}
	case <-reconnect.Done():
		t.Fatal("queued text was not replayed", reconnect.Err())
	}
	select {
	case offer := <-offers:
		file := filepath.Join(t.TempDir(), offer.Name)
		if err := again.AcceptFile(reconnect, selfID, offer.TaskID, offer.FileID, offer.Size, file, nil); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(file); err != nil || sha256.Sum256(got) != sha256.Sum256(outgoingBytes) {
			t.Fatalf("queued file SHA-256 mismatch: %v", err)
		}
	case <-reconnect.Done():
		t.Fatal("queued file offer was not replayed", reconnect.Err())
	}
	eventually(t, "queued text receipt persisted", func() bool {
		for _, m := range reopened.Snapshot().ChatMessages(conv) {
			if m.Text == offlineBody {
				return m.Status == "sent"
			}
		}
		return false
	})
}

func testNeiwangtongDirectories(t *testing.T, n *Node, remote *neiwangtong.Service, selfID, peerID, conv string, offers <-chan neiwangtong.FolderOffer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	base := t.TempDir()
	source := filepath.Join(base, "目录")
	if err := os.MkdirAll(filepath.Join(source, "资料"), 0700); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join("空目录", "仍空")
	if err := os.MkdirAll(filepath.Join(source, empty), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"顶层.bin": bytes.Repeat([]byte("root-data"), 4100), "资料/0-较大.bin": bytes.Repeat([]byte("resend"), 38000),
		"资料/中文.txt": []byte("目录内容"), "资料/空文件.txt": nil}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	verify := func(target string) {
		t.Helper()
		if entries, err := os.ReadDir(filepath.Join(target, empty)); err != nil || len(entries) != 0 {
			t.Fatalf("empty directory was not preserved: %v", err)
		}
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("directory item %s did not match: %v", name, err)
			}
		}
	}
	dir, err := neiwangtong.InspectDirectory(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.SendDirectory(selfID, newID(), dir); err != nil {
		t.Fatal(err)
	}
	var inbound string
	eventually(t, "native folder offer", func() bool {
		for _, transfer := range n.Snapshot().Transfers {
			if transfer.PeerID == peerID && transfer.Name == dir.Name && transfer.Direction == "receive" && transfer.Status == "offered" {
				inbound = transfer.ID
				return true
			}
		}
		return false
	})
	dest := filepath.Join(base, "received")
	if err := n.execute(domain.Command{Kind: domain.AcceptTransfer, TransferID: inbound, Paths: []string{dest}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "completed folder in GoIM", func() bool {
		for _, transfer := range n.Snapshot().Transfers {
			if transfer.ID == inbound {
				return transfer.Status == "completed" && transfer.Path == dest
			}
		}
		return false
	})
	verify(dest)
	if err := n.execute(domain.Command{Kind: domain.SendFiles, ConversationID: conv, Paths: []string{source}}); err != nil {
		t.Fatal(err)
	}
	var offer neiwangtong.FolderOffer
	select {
	case offer = <-offers:
	case <-ctx.Done():
		t.Fatal("test peer did not receive the GoIM directory offer", ctx.Err())
	}
	partial := filepath.Join(base, "interrupted")
	if err := os.Mkdir(partial, 0700); err != nil {
		t.Fatal(err)
	}
	interrupted, stop := context.WithCancel(ctx)
	defer stop()
	var firstBytes int64
	if err := remote.ReceiveDirectory(interrupted, selfID, offer, partial, func(done int64) {
		firstBytes = done
		stop()
	}); err == nil || firstBytes == 0 || firstBytes >= dir.Size {
		t.Fatalf("expected a directory interruption after the first data block, got %d bytes and %v", firstBytes, err)
	}
	var transferID string
	eventually(t, "failed GoIM directory transfer", func() bool {
		for _, transfer := range n.Snapshot().Transfers {
			if transfer.Direction == "send" && transfer.RemotePacket == offer.TaskID && transfer.Status == "failed" {
				transferID = transfer.ID
				return true
			}
		}
		return false
	})
	if err := n.execute(domain.Command{Kind: domain.RetryTransfer, TransferID: transferID}); err != nil {
		t.Fatal(err)
	}
	oldTask := offer.TaskID
	select {
	case offer = <-offers:
		if offer.TaskID == oldTask {
			t.Fatal("retry reused the interrupted directory invitation")
		}
	case <-ctx.Done():
		t.Fatal("test peer did not receive a fresh directory invitation", ctx.Err())
	}
	peerDest := filepath.Join(base, "received-by-peer")
	if err := os.Mkdir(peerDest, 0700); err != nil {
		t.Fatal(err)
	}
	if err := remote.ReceiveDirectory(ctx, selfID, offer, peerDest, nil); err != nil {
		t.Fatal(err)
	}
	verify(peerDest)
	eventually(t, "completed folder to native peer", func() bool {
		for _, transfer := range n.Snapshot().Transfers {
			if transfer.ID == transferID {
				return transfer.RemotePacket == offer.TaskID && transfer.Status == "completed" && transfer.Done == dir.Size
			}
		}
		return false
	})
}
