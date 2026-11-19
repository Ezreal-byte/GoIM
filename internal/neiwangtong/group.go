package neiwangtong

import (
	"bytes"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	CmdGroupRoster  uint32 = 3003
	CmdGroupDismiss uint32 = 3008
	CmdGroupExit    uint32 = 3009
	CmdGroupKick    uint32 = 3010
	CmdGroupText    uint32 = 3011
)

type GroupMember struct {
	ID   string `xml:",chardata"`
	Name string `xml:"name,attr"`
}

type GroupRoster struct {
	ID, Name, OwnerID           string
	InfoVersion, MembersVersion uint64
	PhotoMD5                    string
	Members                     []GroupMember
}

type GroupMessage struct {
	GroupID string
	Message DirectMessage
}

func (s *Service) SendGroupExit(peerID, groupID string) error {
	return s.sendGroupDeparture(peerID, groupID, CmdGroupExit)
}

func (s *Service) SendGroupDismiss(peerID, groupID string) error {
	return s.sendGroupDeparture(peerID, groupID, CmdGroupDismiss)
}

func (s *Service) SendGroupKick(peerID, groupID string) error {
	// The original sends this only to the removed member. Other members
	// receive an updated roster from the owner.
	return s.sendGroupDeparture(peerID, groupID, CmdGroupKick)
}

func (s *Service) sendGroupDeparture(peerID, groupID string, kind uint32) error {
	if !validNativeID(groupID) {
		return errors.New("invalid native group ID")
	}
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	root := groupDepartureRoot(kind)
	body := []byte(fmt.Sprintf(`<%s docver="1"><QGROUP_ID>%s</QGROUP_ID></%s>`, root, groupID, root))
	return s.enet.SendAppCommand(control, kind, body)
}

func groupDepartureRoot(kind uint32) string {
	switch kind {
	case CmdGroupDismiss:
		return "X_QGROUP_DISMISS"
	case CmdGroupKick:
		return "X_QGROUP_KICK"
	default:
		return "X_QGROUP_EXIT"
	}
}

func parseGroupDeparture(data []byte, kind uint32) (string, error) {
	var body struct {
		ID string `xml:"QGROUP_ID"`
	}
	if err := decodeNativeXML(data, groupDepartureRoot(kind), &body); err != nil {
		return "", err
	}
	if !validNativeID(body.ID) {
		return "", errors.New("invalid native group ID")
	}
	return strings.ToLower(body.ID), nil
}

type groupRosterWire struct {
	XMLName xml.Name `xml:"X_QGROUP_PUSH_USER"`
	DocVer  string   `xml:"docver,attr"`
	Info    struct {
		ID             string `xml:"QGROUP_ID"`
		Name           string `xml:"QGROUP_NAME"`
		OwnerID        string `xml:"QGROUP_MASTER"`
		Intro          string `xml:"QGROUP_INTR"`
		Announcement   string `xml:"QGROUP_ANN"`
		InfoVersion    uint64 `xml:"QGROUP_INFO_VER"`
		MembersVersion uint64 `xml:"QGROUP_USER_VER"`
		AllowInvite    int    `xml:"QGROUP_ALLOW_OTHER_ADD"`
		PhotoMD5       string `xml:"QGROUP_PHOTO_MD5"`
	} `xml:"INFO"`
	Members []GroupMember `xml:"USER>ID"`
}

func (s *Service) SendGroupRoster(peerID string, roster GroupRoster) error {
	if roster.OwnerID != s.selfID {
		return errors.New("cannot announce another device's native group")
	}
	if err := roster.validate(); err != nil {
		return err
	}
	member := false
	for _, m := range roster.Members {
		member = member || m.ID == peerID
	}
	if !member {
		return errors.New("native group recipient is not a member")
	}
	wire := groupRosterWire{DocVer: "1", Members: roster.Members}
	wire.Info.ID, wire.Info.Name, wire.Info.OwnerID = roster.ID, roster.Name, roster.OwnerID
	wire.Info.InfoVersion, wire.Info.MembersVersion = roster.InfoVersion, roster.MembersVersion
	wire.Info.PhotoMD5 = roster.PhotoMD5
	data, err := xml.Marshal(wire)
	if err != nil {
		return err
	}
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	return s.enet.SendAppCommand(control, CmdGroupRoster, data)
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
	message, err := parseChatParts(wire.Packet, wire.Content, true)
	return GroupMessage{strings.ToLower(wire.ID), message}, err
}

func (s *Service) SendGroupText(peerID, groupID string, packet uint64, id, text string) error {
	if text == "" {
		return errors.New("invalid native group message")
	}
	body, err := formatChatPayload(id, nativeTextParts(text))
	if err != nil {
		return err
	}
	payload, err := formatGroupBody(groupID, packet, body)
	if err != nil {
		return err
	}
	return s.sendGroupPayload(peerID, payload)
}

func (s *Service) SendGroupTextRecall(peerID, groupID string, packet uint64, id, targetID string) error {
	body, err := formatRecallPayload(id, targetID)
	if err != nil {
		return err
	}
	payload, err := formatGroupBody(groupID, packet, body)
	if err != nil {
		return err
	}
	return s.sendGroupPayload(peerID, payload)
}

func (s *Service) sendGroupPayload(peerID string, payload []byte) error {
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	// Unlike direct chat, stock 3.4.3055 did not return an application
	// receipt here. The outbox can record "sent" after ENet accepts it.
	return s.enet.SendAppCommand(control, CmdGroupText, payload)
}

func (s *Service) SendGroupImage(peerID, groupID string, packet uint64, id string, taskID, fileID uint64, size int64, digest [16]byte, path string) error {
	part, err := chatImagePart(fileID, size, digest)
	if err != nil {
		return err
	}
	payload, err := formatGroupMessage(groupID, packet, id, part)
	if err != nil {
		return err
	}
	control, err := s.prepareFileOffer(peerID, taskID, fileID, size, path)
	if err != nil {
		return err
	}
	if err = s.enet.SendAppCommand(control, CmdGroupText, payload); err != nil && !errors.Is(err, ErrDeliveryUncertain) {
		s.dropFile(taskID)
	}
	return err
}

func formatGroupMessage(groupID string, packet uint64, id string, part MsgDataTxt) ([]byte, error) {
	body, err := formatChatPayload(id, []MsgDataTxt{part})
	if err != nil {
		return nil, err
	}
	return formatGroupBody(groupID, packet, body)
}

func formatGroupBody(groupID string, packet uint64, body string) ([]byte, error) {
	if !validNativeID(groupID) || packet == 0 {
		return nil, errors.New("invalid native group message")
	}
	return []byte(fmt.Sprintf(
		`<X_QGROUP_SEND_MSG docver="1"><QGROUP_ID>%s</QGROUP_ID><MSG_ID>%d</MSG_ID><MSG>%s</MSG><HIDE_RECORD>0</HIDE_RECORD></X_QGROUP_SEND_MSG>`,
		groupID, packet, body)), nil
}

func ParseGroupRoster(data []byte) (GroupRoster, error) {
	var wire groupRosterWire
	// The stock client can close this specific field without its Q prefix.
	// Keep the rest of the record under the normal strict XML decoder.
	data = bytes.ReplaceAll(data, []byte("</GROUP_INFO_VER>"), []byte("</QGROUP_INFO_VER>"))
	if err := decodeNativeXML(data, "X_QGROUP_PUSH_USER", &wire); err != nil {
		return GroupRoster{}, err
	}
	for i := range wire.Members {
		member := &wire.Members[i]
		member.ID = strings.ToLower(strings.TrimSpace(member.ID))
	}
	info := wire.Info
	roster := GroupRoster{ID: strings.ToLower(info.ID), Name: strings.TrimSpace(info.Name),
		OwnerID: strings.ToLower(info.OwnerID), InfoVersion: info.InfoVersion,
		MembersVersion: info.MembersVersion, PhotoMD5: strings.ToLower(strings.TrimSpace(info.PhotoMD5)),
		Members: wire.Members}
	if err := roster.validate(); err != nil {
		return GroupRoster{}, err
	}
	return roster, nil
}

func (r GroupRoster) validate() error {
	if !validNativeID(r.ID) || !validNativeID(r.OwnerID) || strings.TrimSpace(r.Name) == "" ||
		len(r.Name) > 160 || !utf8.ValidString(r.Name) ||
		r.InfoVersion == 0 || r.MembersVersion == 0 || len(r.Members) == 0 || len(r.Members) > 256 {
		return errors.New("invalid native group roster")
	}
	if r.PhotoMD5 != "" {
		digest, err := hex.DecodeString(r.PhotoMD5)
		if err != nil || len(digest) != 16 || strings.ToLower(r.PhotoMD5) != r.PhotoMD5 {
			return errors.New("invalid native group photo digest")
		}
	}
	members := make(map[string]bool, len(r.Members))
	for _, member := range r.Members {
		id := strings.ToLower(member.ID)
		if !validNativeID(id) || members[id] || !utf8.ValidString(member.Name) ||
			len(member.Name) > 160 {
			return errors.New("invalid native group member")
		}
		members[id] = true
	}
	if !members[strings.ToLower(r.OwnerID)] {
		return errors.New("native group owner is not a member")
	}
	return nil
}

func validNativeID(id string) bool {
	value, err := hex.DecodeString(id)
	return err == nil && len(value) == 16
}
