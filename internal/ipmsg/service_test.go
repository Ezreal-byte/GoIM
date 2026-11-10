package ipmsg

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
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

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestProbeDiscoversClassicDialectsFromTheServicePort(t *testing.T) {
	for _, fixture := range []struct{ header, name string }{
		{"1@shiyeline", "内网通"},
		{"1_lbt6_0#128#fixture#0#0#0#4001#9", "飞秋"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			remote, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
			if err != nil {
				t.Fatal(err)
			}
			defer remote.Close()
			peers := make(chan Peer, 4)
			port := freeIPMSGPort(t)
			service, err := Start(Config{Host: "goim-host", Name: "小明", Port: port, Callbacks: Callbacks{Peer: func(peer Peer) {
				peers <- peer
			}}})
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			probe := make(chan error, 1)
			go func() { probe <- service.Probe(ctx, remote.LocalAddr().(*net.UDPAddr).AddrPort()) }()
			read := func() (packet, *net.UDPAddr) {
				t.Helper()
				_ = remote.SetReadDeadline(time.Now().Add(2 * time.Second))
				var buf [4096]byte
				count, from, err := remote.ReadFromUDP(buf[:])
				if err != nil {
					t.Fatal(err)
				}
				p, err := parsePacket(buf[:count])
				if err != nil {
					t.Fatal(err)
				}
				return p, from
			}
			entry, from := read()
			if entry.Command != commandEntry || from.Port != port {
				t.Fatalf("legacy discovery command=%d source port=%d", entry.Command, from.Port)
			}
			nickname, err := simplifiedchinese.GBK.NewEncoder().String(fixture.name)
			if err != nil {
				t.Fatal(err)
			}
			response := fmt.Sprintf("%s:71:SYSTEM:fixture-host:3:%s\x00\x00", fixture.header, nickname)
			if _, err := remote.WriteToUDP([]byte(response), from); err != nil {
				t.Fatal(err)
			}
			if err := <-probe; err != nil {
				t.Fatal(err)
			}
			peer := <-peers
			if peer.Name != fixture.name || !peer.Online {
				t.Fatalf("discovered peer = %+v", peer)
			}
			capabilities, _ := read()
			if capabilities.baseCommand() != commandAnswer || !capabilities.has(optionFile) {
				t.Fatalf("file receiver capability is missing: %+v", capabilities)
			}
			if err := service.SendText(peer.ID, "你好，旧版设备"); err != nil {
				t.Fatal(err)
			}
			message, _ := read()
			decoded, _ := decodeMessage(message.Extra, message.has(optionUTF8))
			if message.Command != commandSend|optionSendCheck || decoded != "你好，旧版设备" {
				t.Fatalf("classic text command=%d, text=%q", message.Command, decoded)
			}
		})
	}
}

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
	service.handleDatagram(packetString(76, "another GoIM", "other-host", commandEntry, "another GoIM\x00"+goIMDiscoveryMarker), from)
	if gotPeer.ID != "" {
		t.Fatal("GoIM discovery must not duplicate a native GoIM contact")
	}
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
	t.Run("Neiwangtong request without NUL", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		// Neiwangtong 3.4.3055 leaves the socket open after the final colon.
		for _, fragment := range []string{"1@shiyeline:42:SYSTEM:fixture-host:96:", fmt.Sprintf("%x:%x:", packetNumber, fileID), "7:"} {
			if _, err := io.WriteString(conn, fragment); err != nil {
				t.Fatal(err)
			}
		}
		got, err := io.ReadAll(conn)
		if err != nil || !bytes.Equal(got, content[7:]) {
			t.Fatalf("resumed bytes from Neiwangtong request = %q, %v", got, err)
		}
	})
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

func TestDownloadFromClassicFileSender(t *testing.T) {
	remote, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	var offerPeer string
	var offerNumber uint64
	var files []Attachment
	var texts int
	service, err := Start(Config{
		Host: "goim-host", Name: "GoIM", Port: freeIPMSGPort(t), DisableBroadcast: true,
		Callbacks: Callbacks{
			Files: func(peer string, number uint64, _ string, attachments []Attachment) {
				offerPeer, offerNumber, files = peer, number, attachments
			},
			Text: func(string, uint64, string) { texts++ },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	addr := remote.Addr().(*net.TCPAddr)
	from := &net.UDPAddr{IP: addr.IP, Port: addr.Port}
	content := "remaining file bytes"
	service.handleDatagram(fmt.Appendf(nil, "1@shiyeline:171:SYSTEM:fixture-host:2097440:sample.txt\x009:sample.txt:%x:1:1:\a\x00", 7+len(content)), from)
	if len(files) != 1 || files[0].Name != "sample.txt" || texts != 0 {
		t.Fatalf("received attachment list = %+v, text messages = %d", files, texts)
	}
	remoteDone := make(chan error, 1)
	go func() {
		remoteDone <- func() error {
			if err := remote.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				return err
			}
			conn, err := remote.AcceptTCP()
			if err != nil {
				return err
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				return err
			}
			// Read the actual wire packet. A classic sender expects a colon
			// after the hexadecimal offset, before the NUL terminator.
			raw, err := bufio.NewReader(conn).ReadString(0)
			if err != nil {
				return err
			}
			if !strings.HasSuffix(raw, ":96:ab:9:7:\x00") {
				return fmt.Errorf("unexpected download request %q", raw)
			}
			_, err = io.WriteString(conn, content)
			return err
		}()
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var dst bytes.Buffer
	_, downloadErr := service.DownloadFile(ctx, offerPeer, offerNumber, files[0].ID, files[0].Size, 7, &dst, nil)
	if err := <-remoteDone; err != nil {
		t.Fatal(err)
	}
	if downloadErr != nil || dst.String() != content {
		t.Fatalf("download = %q, %v", dst.String(), downloadErr)
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
