package domain

import "testing"

func TestHistoricalDirectRoutes(t *testing.T) {
	const old = "ipmsg-0123456789abcdee"
	cases := []struct {
		peer, canonical Peer
		readOnly        bool
	}{
		{Peer{ID: old, Transport: TransportIPMSG}, Peer{}, true},
		{Peer{ID: old, Transport: TransportIPMSG, ClientType: ClientNeiwangtong}, Peer{}, true},
		{Peer{ID: "nwt", Transport: TransportNeiwangtong, CompatibilityAliasOf: "goim"}, Peer{ID: "goim", Transport: TransportGoIM}, true},
		{Peer{ID: "nwt", Transport: TransportNeiwangtong, CompatibilityAliasOf: old}, Peer{ID: old, Transport: TransportNeiwangtong}, false},
		{Peer{ID: old, Transport: TransportNeiwangtong}, Peer{}, false},
		{Peer{ID: "ipmsg-0123456789abcdef0123456789abcdef", Transport: TransportIPMSG}, Peer{}, false},
	}
	for _, c := range cases {
		state := State{Peers: []Peer{c.peer, c.canonical}}
		if (state.DirectReadOnlyReason(c.peer) != "") != c.readOnly {
			t.Errorf("unexpected send eligibility for %+v canonical %+v", c.peer, c.canonical)
		}
	}
}
