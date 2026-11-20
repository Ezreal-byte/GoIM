package core

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"goim/internal/domain"
)

// A saved link from a previous version. We retain confirmed links so already
// linked chats and transfers keep their IDs. Discovery never creates links.
type peerBridge struct {
	CanonicalID   string
	IPMSGID       string
	NeiwangtongID string
	Confirmed     bool
}

type compatibilityEndpoint struct {
	transport string
	id        string
}

func nativeIDForGoIM(id string) [16]byte {
	hash := sha256.Sum256([]byte(id))
	return [16]byte(hash[:16])
}

func keyIDFromNative(id [16]byte) string { return hex.EncodeToString(id[:]) }

func goIMPeerForNativeID(s *domain.State, nativeID string) string {
	if len(nativeID) != 32 {
		return ""
	}
	for _, peer := range s.Peers {
		if peer.Transport != domain.TransportGoIM || len(peer.PublicKey) != ed25519.PublicKeySize || keyID(peer.PublicKey) != peer.ID {
			continue
		}
		id := nativeIDForGoIM(peer.ID)
		if hex.EncodeToString(id[:]) == nativeID {
			return peer.ID
		}
	}
	return ""
}

func removeUnverifiedBridges(data *persisted) {
	for id, bridge := range data.PeerBridges {
		if bridge.Confirmed {
			for _, contactID := range []string{bridge.IPMSGID, bridge.NeiwangtongID} {
				if i := peerIndex(&data.State, contactID); i >= 0 && data.State.Peers[i].Transport != domain.TransportGoIM {
					preferNativeContact(data, &data.State.Peers[i], bridge)
				}
			}
			continue
		}
		delete(data.PeerBridges, id)
		// Previous builds could promote the IPMSG contact by rewriting its
		// transport. Restore that contact's wire identity; history stays put.
		if i := peerIndex(&data.State, bridge.IPMSGID); i >= 0 {
			peer := &data.State.Peers[i]
			if strings.HasPrefix(peer.ID, "ipmsg-") && peer.Transport == transportNWT {
				peer.Transport, peer.Address = legacyIPMSG, ""
				peer.ClientType = domain.ClientNeiwangtong
			}
		}
	}
}

func preferNativeContact(data *persisted, peer *domain.Peer, bridge peerBridge) {
	if peer.Transport != transportNWT {
		peer.Address = ""
	}
	peer.Transport, peer.ClientType = transportNWT, domain.ClientNeiwangtong
	peer.CompatibilityAliasOf = ""
	if peer.ID != bridge.CanonicalID {
		peer.CompatibilityAliasOf = bridge.CanonicalID
		migrateContactPreferences(data, peer.ID, bridge.CanonicalID)
	}
}

func bridgeFor(data *persisted, id string) (peerBridge, bool) {
	for _, bridge := range data.PeerBridges {
		if bridge.Confirmed && (id == bridge.IPMSGID || id == bridge.NeiwangtongID) {
			return bridge, true
		}
	}
	return peerBridge{}, false
}

func wireCompatibilityID(data *persisted, id, transport string) string {
	if bridge, ok := bridgeFor(data, id); ok {
		if transport == transportNWT {
			return bridge.NeiwangtongID
		}
		return bridge.IPMSGID
	}
	if i := peerIndex(&data.State, id); i >= 0 && data.State.Peers[i].Transport == transport &&
		data.State.Peers[i].CompatibilityAliasOf == "" {
		return id
	}
	return ""
}

func transferWireID(data *persisted, transfer domain.Transfer) string {
	if transfer.RemotePeerID != "" {
		return transfer.RemotePeerID
	}
	return wireCompatibilityID(data, transfer.PeerID, transfer.LegacyTransport)
}

// Call under n.mu, or inside a durable mutation. An incoming event is tied to
// the sending adapter even when another adapter is preferred for new sends.
func (n *Node) compatibilitySenderLocked(transport, id string) domain.Peer {
	route, ok := n.compatPeers[compatibilityEndpoint{transport, id}]
	if !ok || !route.Online {
		return domain.Peer{}
	}
	canonical := id
	if bridge, ok := bridgeFor(&n.data, id); ok {
		canonical = bridge.CanonicalID
	} else if route.IsLegacyNeiwangtong() {
		return domain.Peer{}
	}
	peer := n.data.State.Peer(canonical)
	if peer.Transport == domain.TransportGoIM || peer.IsLegacyNeiwangtong() {
		return domain.Peer{}
	}
	return peer
}

func (n *Node) observeCompatibilityPeer(value domain.Peer) {
	// The adapter tracks liveness on every packet. An unchanged announcement
	// does not change the contact or require another database transaction.
	n.mu.Lock()
	previous, known := n.compatPeers[compatibilityEndpoint{value.Transport, value.ID}]
	n.mu.Unlock()
	if known && previous.Online == value.Online && previous.Presence == value.Presence &&
		previous.Name == value.Name && previous.Address == value.Address && previous.ClientType == value.ClientType &&
		previous.Signature == value.Signature && previous.Details == value.Details && previous.Version == value.Version {
		return
	}
	if value.IsLegacyNeiwangtong() {
		n.mu.Lock()
		saved := n.data.State.Peer(value.ID)
		if n.closing || saved.ID == "" || saved.Transport == domain.TransportGoIM {
			n.mu.Unlock()
			return
		}
		if saved.Transport == transportNWT || saved.IsLegacyNeiwangtong() && !saved.Online {
			n.rememberCompatibilityRouteLocked(value)
			n.mu.Unlock()
			return // only old transfers need this wire now; the contact did not change
		}
		n.mu.Unlock()
	}
	var linked []string
	_ = n.durable(func(data *persisted) []pending {
		s := &data.State
		if value.Transport == transportNWT && goIMPeerForNativeID(s, value.ID) != "" {
			return nil
		}
		i := peerIndex(s, value.ID)
		if i >= 0 && s.Peers[i].Transport == domain.TransportGoIM {
			return nil
		}
		// The version marker identifies the Neiwangtong 2425 announcement,
		// but does not prove it belongs to any particular native identity.
		// Keep it only for history and transfers already bound to that wire ID.
		if i < 0 && value.IsLegacyNeiwangtong() {
			return nil
		}
		n.rememberCompatibilityRouteLocked(value)
		if i < 0 {
			s.Peers = append(s.Peers, value)
		}
		linked = n.refreshCompatibilityContactLocked(data, value.ID)
		return nil
	})
	for _, id := range linked {
		n.async(func() { n.replay(id) })
	}
}

func (n *Node) rememberCompatibilityRouteLocked(peer domain.Peer) {
	if n.compatPeers == nil {
		n.compatPeers = make(map[compatibilityEndpoint]domain.Peer)
	}
	n.compatPeers[compatibilityEndpoint{peer.Transport, peer.ID}] = peer
}

// Rebuild only the display profile and effective route. Chat IDs, messages and
// already issued transfer requests always retain their original identities.
func (n *Node) refreshCompatibilityContactLocked(data *persisted, id string) []string {
	s := &data.State
	ids := []string{id}
	bridge, linked := bridgeFor(data, id)
	if linked {
		ids = []string{bridge.IPMSGID, bridge.NeiwangtongID}
	}
	var replay []string
	for _, contactID := range ids {
		i := peerIndex(s, contactID)
		if i < 0 || s.Peers[i].Transport == domain.TransportGoIM {
			continue
		}
		p := &s.Peers[i]
		if p.CompatibilityAliasOf != "" && !linked {
			continue // suppressed GoIM compatibility advertisement
		}
		if linked {
			preferNativeContact(data, p, bridge)
		}
		p.Online = false
		wireID := wireCompatibilityID(data, contactID, p.Transport)
		route := n.compatPeers[compatibilityEndpoint{p.Transport, wireID}]
		if p.IsLegacyNeiwangtong() || route.IsLegacyNeiwangtong() {
			p.ClientType = domain.ClientNeiwangtong
			continue
		}
		if !route.Online {
			continue
		}
		p.Online, p.Address, p.Presence, p.LastSeen = true, route.Address, route.Presence, time.Now()
		data.restoreDeletedContact(p.ID)
		p.Name, p.ClientType = route.Name, route.ClientType
		if p.Transport == transportNWT {
			p.Signature, p.Details, p.Version = route.Signature, route.Details, route.Version
		}
		replay = append(replay, contactID)
	}
	return replay
}

func (n *Node) clearCompatibilityRoutesLocked(transport string) {
	for key := range n.compatPeers {
		if key.transport == transport {
			delete(n.compatPeers, key)
		}
	}
	for _, peer := range n.data.State.Peers {
		if peer.Transport != domain.TransportGoIM {
			n.refreshCompatibilityContactLocked(&n.data, peer.ID)
		}
	}
}
