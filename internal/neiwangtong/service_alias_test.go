package neiwangtong

import (
	"net/netip"
	"testing"
)

func TestUnboundControlCannotUseAnotherPeersProfile(t *testing.T) {
	id := "native-peer"
	seen := false
	s := &Service{
		peers: map[string]*nativePeer{id: {peer: Peer{ID: id, Online: true}}},
	}
	s.cfg.Callbacks.Text = func(string, uint64, string) { seen = true }
	payload, err := FormatSendMsg(1, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	s.onENetCommand(netip.AddrPortFrom(loopback, 19012), CmdSendMsg, payload)
	if seen {
		t.Fatal("an unbound control endpoint borrowed the only known contact")
	}
}
