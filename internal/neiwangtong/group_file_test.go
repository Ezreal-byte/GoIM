package neiwangtong

import (
	"bytes"
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestRestoredGroupFileKeepsTheRecipientAndMembershipScope(t *testing.T) {
	var permitted atomic.Bool
	permitted.Store(true)
	a := Config{Name: "Sender", BindIP: loopback, DisableBroadcast: true, ID: [16]byte{71},
		EnableFolders: true,
		DiscoveryPort: freePort(t, "udp"), ControlPort: freePort(t, "udp"), TransferPort: freePort(t, "tcp"),
		CanServeGroupFile: func(_, _ string) bool { return permitted.Load() }}
	b := Config{Name: "Receiver", BindIP: loopback, DisableBroadcast: true, ID: [16]byte{72},
		EnableFolders: true,
		DiscoveryPort: freePort(t, "udp"), ControlPort: freePort(t, "udp"), TransferPort: freePort(t, "tcp")}
	a.RemoteControlPort, b.RemoteControlPort = b.ControlPort, a.ControlPort
	sender, err := Start(a)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	receiver, err := Start(b)
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := receiver.ProbePeer(ctx, netip.AddrPortFrom(loopback, uint16(a.DiscoveryPort)), a.ControlPort, a.TransferPort); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "chosen.bin")
	data := bytes.Repeat([]byte("explicit group file"), 2300)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	ref := GroupFileRef{ID: "1234567890abcdef1234567890abcdef", Name: "chosen.bin", Size: int64(len(data))}
	const group = "abcdef1234567890abcdef1234567890"
	if err := sender.RememberGroupFile(receiver.selfID, group, ref, 8801, 8802, source); err != nil {
		t.Fatal(err)
	}
	if !sender.HasGroupFileOffer(receiver.selfID, ref.ID) {
		t.Fatal("a restored invitation was not retained")
	}
	unknown := "00000000000000000000000000000001"
	if err := receiver.DownloadGroupFile(ctx, sender.selfID, unknown, ref.Size, filepath.Join(dir, "unknown.bin"), nil); err == nil {
		t.Fatal("an unoffered file identifier was accepted")
	}
	dest := filepath.Join(dir, "received.bin")
	if err := receiver.DownloadGroupFile(ctx, sender.selfID, ref.ID, ref.Size, dest, nil); err != nil {
		t.Fatal(err)
	}
	if actual, err := os.ReadFile(dest); err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("restored group file did not match its source: %v", err)
	}
	if err := sender.RememberGroupFile(receiver.selfID, group, ref, 8801, 8802, source); err != nil {
		t.Fatal(err)
	}
	permitted.Store(false)
	if err := receiver.DownloadGroupFile(ctx, sender.selfID, ref.ID, ref.Size, filepath.Join(dir, "removed.bin"), nil); err == nil {
		t.Fatal("a removed group member could download a restored invitation")
	}

	// The directory path is also restored without re-announcing a message.
	// It remains restricted to this recipient and the chosen directory.
	permitted.Store(true)
	folder := filepath.Join(dir, "chosen-folder")
	if err := os.MkdirAll(filepath.Join(folder, "empty-child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "sample.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	tree, err := InspectDirectory(folder)
	if err != nil {
		t.Fatal(err)
	}
	const folderID = "00000000000000000000000000000003"
	if err := sender.RememberGroupFolder(receiver.selfID, group, folderID, "00000000000000000000000000000004", tree); err != nil {
		t.Fatal(err)
	}
	folderDest := filepath.Join(dir, "received-folder")
	if err := os.Mkdir(folderDest, 0700); err != nil {
		t.Fatal(err)
	}
	total, err := receiver.ReceiveGroupDirectory(ctx, sender.selfID, folderID, tree.Name, folderDest, nil)
	if err != nil || total != int64(len(data)) {
		t.Fatalf("restored group directory failed: total=%d, %v", total, err)
	}
	if actual, err := os.ReadFile(filepath.Join(folderDest, "sample.bin")); err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("restored group directory content changed: %v", err)
	}
	permitted.Store(false)
	deniedDest := t.TempDir()
	if _, err := receiver.ReceiveGroupDirectory(ctx, sender.selfID, folderID, tree.Name, deniedDest, nil); err == nil {
		t.Fatal("a removed member could retrieve the group directory")
	}
	if entries, err := os.ReadDir(deniedDest); err != nil || len(entries) != 0 {
		t.Fatalf("a denied directory request wrote files: %v", err)
	}
}
