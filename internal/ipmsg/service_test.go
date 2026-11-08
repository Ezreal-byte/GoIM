package ipmsg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestServiceReceivesLegacyText(t *testing.T) {
	port := freeIPMSGPort(t)
	var gotPeer Peer
	var gotNumber uint64
	var gotText string
	service, err := Start(Config{
		Host: "receiver-host", Name: "receiver", Port: port,
		Callbacks: Callbacks{
			Peer: func(peer Peer) { gotPeer = peer },
			Text: func(peerID string, packetNumber uint64, text string) {
				if gotPeer.ID != peerID {
					t.Errorf("text peer %q does not match callback peer %q", peerID, gotPeer.ID)
				}
				gotNumber, gotText = packetNumber, text
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	from := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2425}
	service.handleDatagram(packetString(77, "sender", "sender-host", commandSend|optionUTF8, "hello"), from)
	if gotNumber != 77 || gotText != "hello" {
		t.Fatalf("text callback = %d/%q", gotNumber, gotText)
	}
	if gotPeer.Name != "sender" || !gotPeer.Online {
		t.Fatalf("peer callback = %+v", gotPeer)
	}
}

func TestServiceOffersAndServesResumableFile(t *testing.T) {
	port := freeIPMSGPort(t)
	remote, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	service, err := Start(Config{Host: "file-host", Name: "file-sender", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if !service.FilesAvailable() {
		t.Fatal("TCP file listener is unavailable")
	}
	remoteID := "legacy-peer"
	service.mu.Lock()
	service.peers[remoteID] = &peerState{
		peer: Peer{ID: remoteID, Name: "receiver", Online: true},
		addr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: remote.LocalAddr().(*net.UDPAddr).Port},
		seen: time.Now(),
	}
	service.mu.Unlock()
	content := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	path := filepath.Join(t.TempDir(), "sample.bin")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	packetNumber, fileID, err := service.OfferFile(remoteID, path, "transfer", "file caption")
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 64<<10)
	count, _, err := remote.ReadFromUDP(payload)
	if err != nil {
		t.Fatal(err)
	}
	offer, err := parsePacket(payload[:count])
	if err != nil {
		t.Fatal(err)
	}
	if offer.Number != packetNumber || offer.baseCommand() != commandSend || !offer.has(optionFile) {
		t.Fatalf("unexpected file offer: %+v", offer)
	}
	text, encodedFiles := decodeMessage(offer.Extra, offer.has(optionUTF8))
	files := decodeAttachments(encodedFiles)
	if text != "file caption" || len(files) != 1 || files[0].ID != fileID {
		t.Fatalf("offer content = %q %+v", text, files)
	}
	service.mu.Lock()
	service.peers[remoteID].addr.Port = port
	service.mu.Unlock()
	var resumed bytes.Buffer
	_, err = service.DownloadFile(context.Background(), remoteID, packetNumber, fileID, int64(len(content)), 7, &resumed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(resumed.Bytes(), content[7:]) {
		t.Fatalf("resumed bytes = %q", resumed.Bytes())
	}
	var whole bytes.Buffer
	hash, err := service.DownloadFile(context.Background(), remoteID, packetNumber, fileID, int64(len(content)), 0, &whole, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256(content)
	if got := fmt.Sprintf("%x", wantHash); hash != got || !bytes.Equal(whole.Bytes(), content) {
		t.Fatalf("download hash/content mismatch: %s %q", hash, whole.Bytes())
	}
}

func freeIPMSGPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	listener.Close()
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return port
}
