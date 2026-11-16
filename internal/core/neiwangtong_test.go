package core

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	profile := neiwangtong.Peer{Name: "Peer", Sign: "签名", Company: "测试组"}
	remoteCfg := neiwangtong.Config{
		ID: remoteID, Profile: profile, AvatarPNG: nativeTestAvatar(t, 30), BindIP: remoteIP, DisableBroadcast: true,
		DiscoveryPort: freeCompatibilityPort(t), ControlPort: 9012, RemoteControlPort: 9012, TransferPort: 9013,
		Callbacks: neiwangtong.Callbacks{Text: func(_ string, _ uint64, text string) { outgoing <- text }},
	}
	remote, err := neiwangtong.Start(remoteCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
	if err := remote.SendFileOffer(selfID, 777, filepath.Base(path), int64(len(content)), time.Now().Unix(), path); err != nil {
		t.Fatal(err)
	}
	eventually(t, "file offer", func() bool { return len(n.Snapshot().Transfers) == 1 })
	task := n.Snapshot().Transfers[0]
	if task.LegacyTransport != transportNWT || task.Status != "offered" || task.RemotePeerID != nativeID {
		t.Fatalf("offer: %+v", task)
	}
	eventually(t, "file card in chat", func() bool {
		for _, message := range n.Snapshot().ChatMessages(conv) {
			if message.Kind == "file" && message.FileID == task.FileID {
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
	profile.Name = "Updated"
	remote.UpdateProfile(profile, nativeTestAvatar(t, 90))
	eventually(t, "changed avatar and profile", func() bool {
		p := n.Snapshot().Peer(nativeID)
		return p.Avatar != "" && p.Avatar != first && p.Name == "Updated"
	})
	remote.UpdateProfile(profile, nil)
	eventually(t, "removed avatar", func() bool { return n.Snapshot().Peer(nativeID).Avatar == "" })
}
