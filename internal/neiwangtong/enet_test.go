package neiwangtong

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func TestENetPeerCanReconnectOnTheSamePort(t *testing.T) {
	lost := make(chan netip.AddrPort, 1)
	received := make(chan string, 2)
	server, err := newENetTransport(loopback, 0, func(_ netip.AddrPort, _ uint32, payload []byte) {
		received <- string(payload)
	}, func(addr netip.AddrPort) {
		select {
		case lost <- addr:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go server.readLoop()
	target := server.localAddr.AddrPort()
	connect := func(port int, message string) *ENetTransport {
		t.Helper()
		client, err := newENetTransport(loopback, port, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		go client.readLoop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := client.Connect(ctx, target); err != nil {
			t.Fatal(err)
		}
		if err := client.SendAppCommand(target, CmdSendMsg, []byte(message)); err != nil {
			t.Fatal(err)
		}
		if got := waitEvent(t, received); got != message {
			t.Fatalf("message after connecting: got %q, want %q", got, message)
		}
		return client
	}
	client := connect(0, "first session")
	addr := client.localAddr.AddrPort()
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if got := waitEvent(t, lost); got != addr {
		t.Fatalf("disconnected endpoint: got %v, want %v", got, addr)
	}
	connect(int(addr.Port()), "replacement session")
}
