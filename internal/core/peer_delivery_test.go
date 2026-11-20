package core

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"goim/internal/domain"
	ipmsgproto "goim/internal/ipmsg"
)

func TestUnchangedCompatibilityAnnouncementKeepsStateRevision(t *testing.T) {
	n := testNode(t, "local")
	peer := ipmsgproto.Peer{ID: "ipmsg-user", Name: "colleague", Address: "127.0.0.1:2425", Online: true}
	n.receiveLegacyPeer(peer)
	state := n.Snapshot()
	for range 3 {
		n.receiveLegacyPeer(peer)
	}
	if next := n.Snapshot(); next.Revision != state.Revision || next.Peer(peer.ID).LastSeen != state.Peer(peer.ID).LastSeen {
		t.Fatal("unchanged announcements rewrote the contact")
	}
	peer.Away = true
	n.receiveLegacyPeer(peer)
	if n.Snapshot().Peer(peer.ID).Presence != domain.PresenceAway {
		t.Fatal("a presence change was suppressed")
	}
}

func TestUnacknowledgedIPMSGDoesNotDelayOtherConversation(t *testing.T) {
	sender, receiver := testNode(t, "sender"), testNode(t, "recipient")
	connectNodes(t, sender, receiver)
	native := direct(t, sender, receiver)
	port := freeCompatibilityPort(t)
	if err := sender.applyIPMSG(domain.CompatibilityConfig{Enabled: true, Port: port}); err != nil {
		t.Fatal(err)
	}
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	if _, err := socket.WriteToUDP([]byte("1:81:fixture:slow-peer:16777219:slow\x00"), addr); err != nil {
		t.Fatal(err)
	}
	var legacy domain.Peer
	eventually(t, "legacy peer", func() bool {
		for _, p := range sender.Snapshot().Peers {
			if p.Transport == legacyIPMSG && p.Online {
				legacy = p
				return true
			}
		}
		return false
	})
	if err := sender.execute(domain.Command{Kind: domain.StartChat, PeerID: legacy.ID}); err != nil {
		t.Fatal(err)
	}
	sender.Submit(domain.Command{Kind: domain.SendText, ConversationID: directID(sender.Snapshot().Self.ID, legacy.ID), Text: "slow delivery"})
	if err := socket.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var packet [8192]byte
	for {
		n, _, err := socket.ReadFromUDP(packet[:])
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.SplitN(string(packet[:n]), ":", 6)
		if len(fields) == 6 {
			command, _ := strconv.ParseUint(fields[4], 10, 32)
			if command&255 == 32 {
				break // leave the legacy send awaiting its receipt
			}
		}
	}
	sender.Submit(domain.Command{Kind: domain.SendText, ConversationID: native, Text: "independent delivery"})
	deadline := time.After(2 * time.Second)
	for {
		for _, m := range receiver.Snapshot().Messages {
			if m.Text == "independent delivery" {
				return
			}
		}
		select {
		case <-receiver.Updates():
		case <-deadline:
			t.Fatal("native conversation blocked behind IPMSG receipt")
		}
	}
}
