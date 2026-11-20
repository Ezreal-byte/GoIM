package core

import (
	"errors"

	"goim/internal/domain"
)

type contactVisibility string

const (
	contactHidden  contactVisibility = "hidden"
	contactDeleted contactVisibility = "deleted"
)

func (data *persisted) restoreDeletedContact(id string) {
	if data.ContactVisibility[id] == contactDeleted {
		delete(data.ContactVisibility, id)
	}
}

func (n *Node) setContactVisibility(c domain.Command) error {
	var validation error
	err := n.durable(func(data *persisted) []pending {
		peer, ok := data.State.LookupPeer(c.PeerID)
		if !ok || peer.ID == data.State.Self.ID || !peer.ListedContact() || data.ContactVisibility[peer.ID] == contactDeleted {
			validation = errors.New("联系人不存在")
			return nil
		}
		var visibility contactVisibility
		switch {
		case c.Kind == domain.DeleteContact:
			if peer.Online || n.connections[peer.ID] != nil {
				validation = errors.New("只能删除离线联系人")
				return nil
			}
			visibility = contactDeleted
		case c.Bool:
			visibility = contactHidden
		default:
			delete(data.ContactVisibility, peer.ID)
			return nil
		}
		if data.ContactVisibility == nil {
			data.ContactVisibility = make(map[string]contactVisibility)
		}
		data.ContactVisibility[peer.ID] = visibility
		return nil
	})
	if err != nil {
		return err
	}
	return validation
}
