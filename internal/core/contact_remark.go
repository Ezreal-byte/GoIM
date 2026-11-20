package core

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (n *Node) setContactRemark(id, value string) error {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 40 || strings.ContainsFunc(value, unicode.IsControl) {
		return errors.New("备注最多 40 个字符，请保持单行。")
	}
	var validation error
	err := n.durable(func(data *persisted) []pending {
		index := peerIndex(&data.State, id)
		if index < 0 || id == data.State.Self.ID {
			validation = errors.New("联系人不存在")
			return nil
		}
		peer := data.State.Peers[index]
		if peer.CompatibilityAliasOf != "" {
			id = peer.CompatibilityAliasOf
		}
		if value == "" {
			delete(data.PeerRemarks, id)
		} else {
			if data.PeerRemarks == nil {
				data.PeerRemarks = make(map[string]string)
			}
			data.PeerRemarks[id] = value
		}
		data.State.Error, data.State.Notice = "", "备注已更新"
		return nil
	})
	if err != nil {
		return err
	}
	return validation
}
