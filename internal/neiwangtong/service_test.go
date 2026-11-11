package neiwangtong

import (
	"bytes"
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceLocalLoopback(t *testing.T) {
	textCh := make(chan string, 1)
	fileCh := make(chan uint64, 1)

	localIP := netip.AddrFrom4([4]byte{127, 0, 0, 1})

	// Server 1
	s1, err := Start(Config{
		Name:             "Node1",
		Group:            "Dev",
		DiscoveryPort:    19011,
		ControlPort:      19012,
		TransferPort:     19013,
		BindIP:           localIP,
		DisableBroadcast: true,
		Callbacks: Callbacks{
			Text: func(peerID string, packetID uint64, text string) {
				textCh <- text
			},
			Files: func(peerID string, taskID uint64, name string, size int64) {
				fileCh <- taskID
			},
		},
	})
	if err != nil {
		t.Fatalf("Start s1 error: %v", err)
	}
	defer s1.Close()

	// Server 2
	s2, err := Start(Config{
		Name:             "Node2",
		Group:            "Dev",
		DiscoveryPort:    29011,
		ControlPort:      29012,
		TransferPort:     29013,
		BindIP:           localIP,
		DisableBroadcast: true,
	})
	if err != nil {
		t.Fatalf("Start s2 error: %v", err)
	}
	defer s2.Close()

	// Directed probe s2 -> s1
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s2.Probe(ctx, netip.AddrPortFrom(localIP, 19011), 19012, 19013); err != nil {
		t.Fatalf("Probe error: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	s1PeerID := s1.markerHex
	s2PeerID := s2.markerHex

	s1.peersMu.RLock()
	for id, p := range s1.peers {
		t.Logf("s1 sees peer %s: udp9012=%v, tcp9013=%v", id, p.udp9012, p.tcp9013)
	}
	s1.peersMu.RUnlock()

	s2.peersMu.RLock()
	for id, p := range s2.peers {
		t.Logf("s2 sees peer %s: udp9012=%v, tcp9013=%v", id, p.udp9012, p.tcp9013)
	}
	s2.peersMu.RUnlock()

	// 1. Send text from s2 to s1
	if err := s2.SendText(s1PeerID, "loopback hello!"); err != nil {
		t.Fatalf("SendText error: %v", err)
	}

	select {
	case msg := <-textCh:
		if msg != "loopback hello!" {
			t.Errorf("Unexpected text: %s", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for text message")
	}

	// 2. Send file offer from s2 to s1
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "test-send.txt")
	testData := []byte("neiwangtong file loopback data content 12345")
	_ = os.WriteFile(srcPath, testData, 0644)
	taskID := uint64(888)
	if err := s2.SendFileOffer(s1PeerID, taskID, "test-send.txt", int64(len(testData)), time.Now().Unix(), srcPath); err != nil {
		t.Fatalf("SendFileOffer error: %v", err)
	}

	var recvdTaskID uint64
	select {
	case recvdTaskID = <-fileCh:
		if recvdTaskID != taskID {
			t.Errorf("Unexpected taskID: %d", recvdTaskID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for file offer")
	}

	// 3. Accept file on s1
	destPath := filepath.Join(tmpDir, "test-recvd.txt")
	if err := s1.AcceptFile(context.Background(), s2PeerID, recvdTaskID, int64(len(testData)), destPath); err != nil {
		t.Fatalf("AcceptFile error: %v", err)
	}

	readBack, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBack, testData) {
		t.Fatalf("Downloaded file mismatch: got %s, want %s", string(readBack), string(testData))
	}
}
