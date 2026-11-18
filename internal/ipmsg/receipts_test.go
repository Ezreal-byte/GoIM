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
	return service, remote, peerID("old-host", "127.0.0.1")
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
