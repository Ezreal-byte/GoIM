package neiwangtong

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	CmdGroupRoster uint32 = 3003
	CmdGroupText   uint32 = 3011
)

type GroupMember struct {
	ID   string `xml:",chardata"`
	Name string `xml:"name,attr"`
}

type GroupRoster struct {
	ID, Name, OwnerID           string
	InfoVersion, MembersVersion uint64
	Members                     []GroupMember
}

type GroupMessage struct {
	GroupID string
	Message DirectMessage
}

func ParseGroupMessage(data []byte) (GroupMessage, error) {
	var wire struct {
		ID      string `xml:"QGROUP_ID"`
		Packet  uint64 `xml:"MSG_ID"`
		Content string `xml:"MSG"`
	}
	if err := decodeNativeXML(data, "X_QGROUP_SEND_MSG", &wire); err != nil {
		return GroupMessage{}, err
	}
	if !validNativeID(wire.ID) || wire.Packet == 0 {
		return GroupMessage{}, errors.New("invalid native group message")
	}
	message, err := parseChatPayload(wire.Packet, wire.Content)
	return GroupMessage{strings.ToLower(wire.ID), message}, err
}

func (s *Service) SendGroupText(peerID, groupID string, packet uint64, id, text string) error {
	if !validNativeID(groupID) || packet == 0 || text == "" {
		return errors.New("invalid native group message")
	}
	body, err := formatChatPayload(id, []MsgDataTxt{{Txt: &MsgTxt{T: "normal", V: text}}})
	if err != nil {
		return err
	}
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	payload := []byte(fmt.Sprintf(
		`<X_QGROUP_SEND_MSG docver="1"><QGROUP_ID>%s</QGROUP_ID><MSG_ID>%d</MSG_ID><MSG>%s</MSG><HIDE_RECORD>0</HIDE_RECORD></X_QGROUP_SEND_MSG>`,
		groupID, packet, body))
	// Unlike direct chat, stock 3.4.3055 did not return an application
	// receipt here. The outbox can record "sent" after ENet accepts it.
	return s.enet.SendAppCommand(control, CmdGroupText, payload)
}

func ParseGroupRoster(data []byte) (GroupRoster, error) {
	var wire struct {
		Info struct {
			ID             string `xml:"QGROUP_ID"`
			Name           string `xml:"QGROUP_NAME"`
			OwnerID        string `xml:"QGROUP_MASTER"`
			InfoVersion    uint64 `xml:"QGROUP_INFO_VER"`
			MembersVersion uint64 `xml:"QGROUP_USER_VER"`
		} `xml:"INFO"`
		Members []GroupMember `xml:"USER>ID"`
	}
	// The stock client can close this specific field without its Q prefix.
	// Keep the rest of the record under the normal strict XML decoder.
	data = bytes.ReplaceAll(data, []byte("</GROUP_INFO_VER>"), []byte("</QGROUP_INFO_VER>"))
	if err := decodeNativeXML(data, "X_QGROUP_PUSH_USER", &wire); err != nil {
		return GroupRoster{}, err
	}
	info := wire.Info
	info.ID, info.OwnerID = strings.ToLower(info.ID), strings.ToLower(info.OwnerID)
	info.Name = strings.TrimSpace(info.Name)
	if !validNativeID(info.ID) || !validNativeID(info.OwnerID) ||
		info.Name == "" || len(info.Name) > 160 || !utf8.ValidString(info.Name) ||
		info.InfoVersion == 0 || info.MembersVersion == 0 || len(wire.Members) == 0 || len(wire.Members) > 256 {
		return GroupRoster{}, errors.New("invalid native group roster")
	}
	foundOwner := false
	members := make(map[string]bool, len(wire.Members))
	for i := range wire.Members {
		member := &wire.Members[i]
		member.ID = strings.ToLower(strings.TrimSpace(member.ID))
		if !validNativeID(member.ID) || members[member.ID] || !utf8.ValidString(member.Name) ||
			len(member.Name) > 160 {
			return GroupRoster{}, errors.New("invalid native group member")
		}
		members[member.ID] = true
		foundOwner = foundOwner || member.ID == info.OwnerID
	}
	if !foundOwner {
		return GroupRoster{}, errors.New("native group owner is not a member")
	}
	return GroupRoster{ID: info.ID, Name: info.Name, OwnerID: info.OwnerID,
		InfoVersion: info.InfoVersion, MembersVersion: info.MembersVersion, Members: wire.Members}, nil
}

func validNativeID(id string) bool {
	value, err := hex.DecodeString(id)
	return err == nil && len(value) == 16
}
