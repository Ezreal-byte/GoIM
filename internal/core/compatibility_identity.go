package core

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"goim/internal/domain"
)

// The two wire identities are independent. Confirmed is deliberately absent in
// old IP-based records: those records must never authorize routing a message.
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
			continue
		}
		delete(data.PeerBridges, id)
		// Previous builds could promote the IPMSG contact by rewriting its
		// transport. Restore that contact's wire identity; history stays put.
		if i := peerIndex(&data.State, bridge.IPMSGID); i >= 0 {
			peer := &data.State.Peers[i]
			if strings.HasPrefix(peer.ID, "ipmsg-") && peer.Transport == transportNWT {
				peer.Transport, peer.Address = legacyIPMSG, ""
			}
		}
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
	}
	peer := n.data.State.Peer(canonical)
	if peer.Transport == domain.TransportGoIM {
		return domain.Peer{}
	}
	return peer
}

func (n *Node) observeCompatibilityPeer(value domain.Peer) {
	var linked []string
	_ = n.durable(func(data *persisted) []pending {
		s := &data.State
		if value.Transport == transportNWT && goIMPeerForNativeID(s, value.ID) != "" {
			return nil
		}
		if i := peerIndex(s, value.ID); i >= 0 && s.Peers[i].Transport == domain.TransportGoIM {
			return nil
		}
		if n.compatPeers == nil {
			n.compatPeers = make(map[compatibilityEndpoint]domain.Peer)
		}
		key := compatibilityEndpoint{value.Transport, value.ID}
		n.compatPeers[key] = value
		if peerIndex(s, value.ID) < 0 {
			s.Peers = append(s.Peers, value)
		}
		linked = n.refreshCompatibilityContactLocked(data, value.ID)
		return nil
	})
	for _, id := range linked {
		n.async(func() { n.replay(id) })
	}
}

// Rebuild only the display profile and effective route. Chat IDs, messages and
// already issued transfer requests always retain their original identities.
func (n *Node) refreshCompatibilityContactLocked(data *persisted, id string) []string {
	s := &data.State
	ids := []string{id}
	primary := id
	if bridge, ok := bridgeFor(data, id); ok {
		ids = []string{bridge.IPMSGID, bridge.NeiwangtongID}
		primary = bridge.CanonicalID
	}
	var replay []string
	for _, contactID := range ids {
		i := peerIndex(s, contactID)
		if i < 0 || s.Peers[i].Transport == domain.TransportGoIM {
			continue
		}
		p := &s.Peers[i]
		if p.CompatibilityAliasOf != "" && len(ids) == 1 {
			continue // suppressed GoIM compatibility advertisement
		}
		p.CompatibilityAliasOf = ""
		if contactID != primary {
			p.CompatibilityAliasOf = primary
		}
		p.Online = false
		for _, transport := range []string{transportNWT, legacyIPMSG} {
			wireID := wireCompatibilityID(data, contactID, transport)
			route := n.compatPeers[compatibilityEndpoint{transport, wireID}]
			if !route.Online {
				continue
			}
			p.Online, p.Transport, p.Address, p.Presence = true, transport, route.Address, route.Presence
			p.LastSeen = time.Now()
			if transport == transportNWT || p.Name == "" || len(ids) == 1 {
				p.Name = route.Name
			}
			if transport == transportNWT {
				p.Signature, p.Details, p.Version = route.Signature, route.Details, route.Version
				p.ClientType = domain.ClientNeiwangtong
			} else if p.ClientType != domain.ClientNeiwangtong {
				p.ClientType = route.ClientType
			}
			replay = append(replay, contactID)
			break
		}
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

func (n *Node) linkCompatibility(primary, secondary string) error {
	if primary == "" || secondary == "" || primary == secondary {
		return errors.New("请选择同一台内网通设备的两条连接")
	}
	var problem error
	var replay []string
	err := n.durable(func(data *persisted) []pending {
		a, b := data.State.Peer(primary), data.State.Peer(secondary)
		ipmsg, native := a, b
		if b.Transport == legacyIPMSG {
			ipmsg, native = b, a
		}
		_, hasPrimary := bridgeFor(data, primary)
		_, hasSecondary := bridgeFor(data, secondary)
		if ipmsg.ClientType != domain.ClientNeiwangtong || native.ClientType != domain.ClientNeiwangtong ||
			ipmsg.Transport != legacyIPMSG || native.Transport != transportNWT ||
			a.CompatibilityAliasOf != "" || b.CompatibilityAliasOf != "" || hasPrimary || hasSecondary ||
			!n.compatPeers[compatibilityEndpoint{legacyIPMSG, ipmsg.ID}].Online ||
			!n.compatPeers[compatibilityEndpoint{transportNWT, native.ID}].Online {
			problem = errors.New("只能关联尚未关联的内网通原生和 2425 兼容联系人，两条连接都须已验证")
			return nil
		}
		data.PeerBridges[primary] = peerBridge{
			CanonicalID: primary, IPMSGID: ipmsg.ID, NeiwangtongID: native.ID, Confirmed: true,
		}
		if index := peerIndex(&data.State, primary); index >= 0 {
			data.State.Peers[index].Avatar = native.Avatar
		}
		replay = n.refreshCompatibilityContactLocked(data, primary)
		return nil
	})
	if err != nil {
		return err
	}
	if problem == nil {
		for _, id := range replay {
			n.async(func() { n.replay(id) })
		}
	}
	return problem
}
