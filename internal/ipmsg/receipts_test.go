package ipmsg

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"
)

func receiptFixture(t *testing.T) (*Service, *net.UDPConn, string) {
	t.Helper()
	remote, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remote.Close() })
	service, err := Start(Config{Host: "goim-host", Name: "中文昵称", Port: freeIPMSGPort(t),
		BindIP: netip.MustParseAddr("127.0.0.1"), DisableBroadcast: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	service.handleDatagram(packetString(11, "old-user", "old-host", commandAnswer, "原版用户"), remote.LocalAddr().(*net.UDPAddr))
	return service, remote, peerID("old-host", "old-user")
}

func readReceiptFixtureMessage(t *testing.T, remote *net.UDPConn) ([]byte, packet, *net.UDPAddr) {
	t.Helper()
	_ = remote.SetReadDeadline(time.Now().Add(2 * time.Second))
	var data [8192]byte
	for {
		n, from, err := remote.ReadFromUDP(data[:])
		if err != nil {
			t.Fatal(err)
		}
		p, err := parsePacket(data[:n])
		if err != nil {
			t.Fatal(err)
		}
		if p.baseCommand() == commandSend {
			return bytes.Clone(data[:n]), p, from
		}
	}
}

func TestConfirmedTextRetriesIdenticalBytesAndMatchesPeerReceipt(t *testing.T) {
	service, remote, id := receiptFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- service.SendTextConfirmed(ctx, id, 7654321, "中文互通") }()
	first, message, from := readReceiptFixtureMessage(t, remote)
	if message.Number != 7654321 || !message.has(optionSendCheck) || message.has(optionUTF8) {
		t.Fatalf("classic confirmation packet changed: %+v", message)
	}
	ack := func(sock *net.UDPConn, host string, number uint64) {
		t.Helper()
		_, err := sock.WriteToUDP(packetString(91, "old-user", host, commandReceived, strconv.FormatUint(number, 10)), from)
		if err != nil {
			t.Fatal(err)
		}
	}
	ack(remote, "old-host", message.Number+1)
	ack(remote, "unrelated-host", message.Number)
	rogue, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
	if err != nil {
		t.Fatal(err)
	}
	defer rogue.Close()
	ack(rogue, "old-host", message.Number)
	select {
	case err := <-result:
		t.Fatalf("a mismatched receipt completed the send: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	second, _, _ := readReceiptFixtureMessage(t, remote)
	if !bytes.Equal(first, second) {
		t.Fatal("retransmission changed the packet identity or encoded text")
	}
	ack(remote, "old-host", message.Number)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("valid receipt did not complete the send")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.receipts) != 0 || len(service.peers) != 1 {
		t.Fatal("receipt handling leaked a waiter or created an unrelated contact")
	}
}

func TestConfirmedTextStopsOnCancellationAndServiceClose(t *testing.T) {
	for _, closeService := range []bool{false, true} {
		t.Run(strconv.FormatBool(closeService), func(t *testing.T) {
			service, remote, id := receiptFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- service.SendTextConfirmed(ctx, id, 112233, "cancel fixture") }()
			readReceiptFixtureMessage(t, remote)
			if closeService {
				service.Close()
			} else {
				cancel()
			}
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("send returned %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("an unacknowledged send prevented shutdown")
			}
		})
	}
}

func TestConfirmedTextNeverReportsSuccessWithoutAReceipt(t *testing.T) {
	service, _, id := receiptFixture(t)
	if err := service.SendTextConfirmed(t.Context(), id, 445566, "missing receipt"); !errors.Is(err, ErrReceiptTimeout) {
		t.Fatalf("unacknowledged send returned %v", err)
	}
}

func TestNicknameChangesDoNotChangeTheWireLogon(t *testing.T) {
	service, _, _ := receiptFixture(t)
	before, err := parsePacket(service.presencePacket(service.profile.Load(), commandAnswer))
	if err != nil {
		t.Fatal(err)
	}
	const name = "新昵称:研发"
	service.UpdateProfile(name, false, false)
	after, err := parsePacket(service.presencePacket(service.profile.Load(), commandAnswer))
	if err != nil {
		t.Fatal(err)
	}
	nickname, _ := decodeMessage(after.Extra, after.has(optionUTF8))
	if before.Sender != after.Sender || nickname != name {
		t.Fatal("changing the display nickname changed the logon identity or the nickname's punctuation")
	}
}

func TestDifferentUsersCanShareTheComputerName(t *testing.T) {
	text := make(chan string, 1)
	service, err := Start(Config{Host: "same-host", User: "GoIM-fixed-identity", Name: "GoIM",
		Port: freeIPMSGPort(t), BindIP: netip.MustParseAddr("127.0.0.1"), DisableBroadcast: true,
		Callbacks: Callbacks{Message: func(_ string, _ uint64, body string, _ []Attachment) error { text <- body; return nil }}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	from := net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:2425"))
	service.handleDatagram(packetString(12, "colleague", "same-host", commandSend|optionUTF8, "同名电脑"), from)
	select {
	case got := <-text:
		if got != "同名电脑" {
			t.Fatal("message from the other logon changed")
		}
	default:
		t.Fatal("a different logon was discarded because its computer name matched")
	}
	service.handleDatagram(packetString(13, "GoIM-fixed-identity", "same-host", commandSend|optionUTF8, "self"), from)
	select {
	case <-text:
		t.Fatal("a reflected packet from our own logon was delivered")
	default:
	}
}

func TestReceiptWaitsForMessagePersistence(t *testing.T) {
	service, remote, _ := receiptFixture(t)
	stored := false
	service.cfg.Callbacks.Message = func(_ string, number uint64, text string, files []Attachment) error {
		if number != 42 || text != "attachment caption" || len(files) != 1 || files[0].Name != "report.txt" {
			t.Fatalf("message contents were split or lost: %d %q %+v", number, text, files)
		}
		if !stored {
			return errors.New("fixture store unavailable")
		}
		return nil
	}
	data := packetString(42, "old-user", "old-host", commandSend|optionSendCheck|optionUTF8|optionFile,
		"attachment caption\x00"+encodeAttachments([]Attachment{{ID: 1, Name: "report.txt", Size: 12, Kind: fileRegular}}))
	from := remote.LocalAddr().(*net.UDPAddr)
	readReceipt := func() (packet, error) {
		var response [2048]byte
		for {
			size, _, err := remote.ReadFromUDP(response[:])
			if err != nil {
				return packet{}, err
			}
			if p, err := parsePacket(response[:size]); err == nil && p.baseCommand() == commandReceived {
				return p, nil
			}
		}
	}
	service.handleDatagram(data, from)
	_ = remote.SetReadDeadline(time.Now().Add(75 * time.Millisecond))
	if _, err := readReceipt(); err == nil {
		t.Fatal("a failed write produced a delivery receipt")
	}
	stored = true
	for range 2 {
		service.handleDatagram(data, from)
		_ = remote.SetReadDeadline(time.Now().Add(time.Second))
		packet, err := readReceipt()
		if err != nil || packet.Extra != "42" {
			t.Fatalf("persisted message receipt = %+v, %v", packet, err)
		}
	}
}

func TestLoginSwitchRetiresThePreviousRoute(t *testing.T) {
	service, remote, old := receiptFixture(t)
	from := remote.LocalAddr().(*net.UDPAddr)
	service.handleDatagram(packetString(12, "new-user", "old-host", commandAnswer, "Other user"), from)
	next := peerID("old-host", "new-user")
	if old == next || service.peer(old) != nil || service.peer(next) == nil {
		t.Fatal("the new login reused the previous login's route")
	}
	if err := service.SendTextConfirmed(t.Context(), old, 42, "old pending message"); !errors.Is(err, ErrPeerOffline) {
		t.Fatalf("old login could still deliver on the shared endpoint: %v", err)
	}
}

func TestManualContactReceivesAwayAndAvailableUpdates(t *testing.T) {
	service, remote, _ := receiptFixture(t)
	for _, away := range []bool{true, false} {
		// The manual endpoint uses its own port. It must still receive a
		// directed update when the local user also enables LAN discovery.
		service.SetBroadcastEnabled(!away)
		service.UpdateProfile("fixture", false, away)
		_ = remote.SetReadDeadline(time.Now().Add(2 * time.Second))
		var data [4096]byte
		for {
			count, _, err := remote.ReadFromUDP(data[:])
			if err != nil {
				t.Fatal(err)
			}
			p, err := parsePacket(data[:count])
			if err != nil {
				t.Fatal(err)
			}
			if p.baseCommand() != commandAbsence {
				continue
			}
			if p.has(optionAbsence) != away || !p.has(optionFile) {
				t.Fatalf("presence command %#x, away=%t", p.Command, away)
			}
			break
		}
	}
}
