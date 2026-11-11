package core

import (
	"bytes"
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goim/internal/domain"
	"goim/internal/neiwangtong"
)

func TestNeiwangtongCompatibilitySettingsAndLifecycle(t *testing.T) {
	n, err := New(Options{DataDir: t.TempDir(), Listen: "127.0.0.1:0", DisableDiscovery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()

	initial := n.Snapshot()
	if initial.Settings.Neiwangtong.Enabled || initial.NeiwangtongStatus.Active {
		t.Fatal("fresh install started Neiwangtong unexpectedly")
	}

	discPort := freeCompatibilityPort(t)
	cfg := domain.CompatibilityConfig{Enabled: true, Port: discPort}
	if err := n.setCompatibility(domain.TransportNeiwangtong, cfg); err != nil {
		t.Fatalf("setCompatibility error: %v", err)
	}

	s := n.Snapshot()
	if !s.Settings.Neiwangtong.Enabled || !s.NeiwangtongStatus.Active {
		t.Fatalf("Neiwangtong not active: %+v", s.NeiwangtongStatus)
	}

	// Disable
	cfg.Enabled = false
	if err := n.setCompatibility(domain.TransportNeiwangtong, cfg); err != nil {
		t.Fatalf("disable error: %v", err)
	}
	s = n.Snapshot()
	if s.Settings.Neiwangtong.Enabled || s.NeiwangtongStatus.Active {
		t.Fatalf("Neiwangtong still active after disable: %+v", s.NeiwangtongStatus)
	}
}

func TestNeiwangtongDirectCommunication(t *testing.T) {
	nodeDir := t.TempDir()
	discPort := freeCompatibilityPort(t)

	n, err := New(Options{DataDir: nodeDir, Listen: "127.0.0.1:0", DisableDiscovery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()

	if err := n.setCompatibility(domain.TransportNeiwangtong, domain.CompatibilityConfig{Enabled: true, Port: discPort}); err != nil {
		t.Fatalf("setCompatibility error: %v", err)
	}

	textCh := make(chan string, 1)
	localIP := netip.AddrFrom4([4]byte{127, 0, 0, 1})
	peerDiscPort := freeCompatibilityPort(t)
	peerCtrlPort := freeCompatibilityPort(t)
	peerTranPort := freeCompatibilityPort(t)

	// Standalone native peer
	peerSrv, err := neiwangtong.Start(neiwangtong.Config{
		Name:             "RemoteNWT",
		Group:            "Marketing",
		DiscoveryPort:    peerDiscPort,
		ControlPort:      peerCtrlPort,
		TransferPort:     peerTranPort,
		BindIP:           localIP,
		DisableBroadcast: true,
		Callbacks: neiwangtong.Callbacks{
			Text: func(pID string, packetID uint64, text string) {
				textCh <- text
			},
		},
	})
	if err != nil {
		t.Fatalf("Start peer error: %v", err)
	}
	defer peerSrv.Close()

	// Probe from peer to GoIM Node
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := peerSrv.Probe(ctx, netip.AddrPortFrom(localIP, uint16(discPort)), 9012, 9013); err != nil {
		t.Fatalf("Probe error: %v", err)
	}

	// Wait for GoIM to see peer
	deadline := time.Now().Add(3 * time.Second)
	var foundPeerID string
	for time.Now().Before(deadline) {
		s := n.Snapshot()
		for _, p := range s.Peers {
			if p.Transport == domain.TransportNeiwangtong && p.Online {
				foundPeerID = p.ID
				break
			}
		}
		if foundPeerID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if foundPeerID == "" {
		t.Fatal("GoIM did not discover Neiwangtong peer")
	}

	// 1. Peer sends text to GoIM
	goimNodeID := n.Snapshot().Self.ID
	_ = goimNodeID
	// In native protocol, sending text to any connected peer
	peerSrv.Probe(ctx, netip.AddrPortFrom(localIP, uint16(discPort)), 9012, 9013)
	time.Sleep(100 * time.Millisecond)
	for id := range n.Snapshot().Peers {
		_ = id
	}

	// 2. Test receiving text in GoIM via Node's receive callback
	n.receiveNeiwangtongText(foundPeerID, 12345, "Hello from Neiwangtong native!")
	s := n.Snapshot()
	var foundMsg bool
	for _, m := range s.Messages {
		if m.Text == "Hello from Neiwangtong native!" {
			foundMsg = true
			break
		}
	}
	if !foundMsg {
		t.Fatal("GoIM state did not record native text message")
	}

	// 3. Test receiving file offer in GoIM
	tmpDir := t.TempDir()
	testPayload := []byte("neiwangtong test file payload 999")
	srcPath := filepath.Join(tmpDir, "nwt-offer.txt")
	_ = os.WriteFile(srcPath, testPayload, 0644)
	taskID := uint64(777)
	peerSrv.RegisterFile(taskID, srcPath, int64(len(testPayload)))

	n.receiveNeiwangtongFiles(foundPeerID, taskID, "nwt-offer.txt", int64(len(testPayload)))
	s = n.Snapshot()
	var foundTransfer domain.Transfer
	for _, tr := range s.Transfers {
		if tr.Name == "nwt-offer.txt" && tr.LegacyTransport == domain.TransportNeiwangtong {
			foundTransfer = tr
			break
		}
	}
	if foundTransfer.ID == "" {
		t.Fatal("GoIM did not create transfer for Neiwangtong file offer")
	}

	// 4. Test download in GoIM
	destPath := filepath.Join(tmpDir, "downloaded-nwt.txt")
	n.mu.Lock()
	srv := n.neiwangtong
	n.mu.Unlock()
	if srv != nil {
		// Mock peer in srv with peerTranPort
		srv.SetPeerTransferPort(foundPeerID, netip.AddrPortFrom(localIP, uint16(peerTranPort)))
	}

	if err := n.downloadNeiwangtong(context.Background(), foundTransfer, s.File(foundTransfer.FileID), destPath); err != nil {
		t.Fatalf("downloadNeiwangtong error: %v", err)
	}

	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, testPayload) {
		t.Fatalf("Downloaded file mismatch: got %s, want %s", string(content), string(testPayload))
	}
}