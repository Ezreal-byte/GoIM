package neiwangtong

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"time"
)

const (
	CmdHandshark        uint32 = 1000
	CmdChangeStatus     uint32 = 1001
	CmdChangeSign       uint32 = 1002
	CmdChangeInfo       uint32 = 1003
	CmdSendMsg          uint32 = 1004
	CmdSendMsgAck       uint32 = 1005
	CmdSendFile         uint32 = 1009
	CmdOperateSendFile  uint32 = 1010
	CmdOperateRecvFile  uint32 = 1011
	CmdProgressRecvFile uint32 = 1012
	CmdHeartbeat        uint32 = 1016
	CmdQuit             uint32 = 1017
	CmdReady            uint32 = 1018
	CmdFileReverseReq   uint32 = 1019
)

type XMLSendMsg struct {
	XMLName    xml.Name `xml:"X_SEND_MSG"`
	DocVer     string   `xml:"docver,attr"`
	MsgID      uint64   `xml:"MSG_ID"`
	Receipt    int      `xml:"RECEIPT"`
	Msg        string   `xml:"MSG"`
	MsgTime    uint64   `xml:"MSG_TIME"`
	Offline    int      `xml:"OFFLINE"`
	HideRecord int      `xml:"HIDE_RECORD"`
}

type XMLSendMsgAck struct {
	XMLName xml.Name `xml:"X_SEND_MSG_ACK"`
	DocVer  string   `xml:"docver,attr"`
	MsgID   uint64   `xml:"MSG_ID"`
}

type MsgJSON struct {
	App  string       `json:"app"`
	Dt   []MsgDataTxt `json:"dt"`
	Font messageFont  `json:"ft"`
	ID   string       `json:"id"`
	Type string       `json:"type"`
	Ver  string       `json:"ver"`
}

type messageFont struct {
	Bold      string `json:"b,omitempty"`
	Color     string `json:"c,omitempty"`
	Italic    string `json:"i,omitempty"`
	Name      string `json:"n,omitempty"`
	Size      string `json:"s"`
	Underline string `json:"u,omitempty"`
}

type MsgDataTxt struct {
	Txt      *MsgTxt       `json:"txt,omitempty"`
	Img      *MsgTxt       `json:"img,omitempty"`
	Retrieve *recallTarget `json:"retrieve,omitempty"`
	File     *chatFileWire `json:"file2,omitempty"`
}

type recallTarget struct {
	Original string `json:"m"`
	ID       string `json:"v"`
}

type MsgTxt struct {
	T string `json:"t"`
	V string `json:"v"`
}

type ChatImageRef struct {
	Selector uint32
	Digest   [16]byte
}

type MessagePart struct {
	Text  string
	Image *ChatImageRef
	File  *GroupFileRef
}

type DirectMessage struct {
	PacketID  uint64
	ID        string
	RecallID  string
	Parts     []MessagePart
	OfflineAt time.Time
}

func FormatSendMsg(msgID uint64, text string) ([]byte, error) {
	return formatMessage(msgID, "", nativeTextParts(text), time.Time{})
}

func formatMessage(msgID uint64, id string, parts []MsgDataTxt, offlineAt time.Time) ([]byte, error) {
	text, err := formatChatPayload(id, parts)
	if err != nil {
		return nil, err
	}
	return formatDirectBody(msgID, text, offlineAt), nil
}

func formatDirectBody(msgID uint64, body string, offlineAt time.Time) []byte {
	stamp, offline := int64(0), 0
	if !offlineAt.IsZero() && offlineAt.Unix() > 0 {
		stamp, offline = offlineAt.Unix(), 1
	}
	return []byte(fmt.Sprintf(
		`<X_SEND_MSG docver="1"><MSG_ID>%d</MSG_ID><RECEIPT>0</RECEIPT><MSG>%s</MSG><MSG_TIME>%d</MSG_TIME><OFFLINE>%d</OFFLINE><HIDE_RECORD>0</HIDE_RECORD></X_SEND_MSG>`,
		msgID, body, stamp, offline,
	))
}

func formatChatPayload(id string, parts []MsgDataTxt) (string, error) {
	return encodeChatPayload(newChatPayload(id, parts))
}

func newChatPayload(id string, parts []MsgDataTxt) MsgJSON {
	var randomID [16]byte
	if id == "" {
		_, _ = rand.Read(randomID[:])
		id = hex.EncodeToString(randomID[:])
	}

	return MsgJSON{
		App:  "shiyeline",
		Dt:   parts,
		Font: messageFont{Name: "微软雅黑", Size: "9", Color: "0x000000", Bold: "0", Italic: "0", Underline: "0"},
		ID:   id,
		Type: "0",
		Ver:  "6.0",
	}
}

func formatRecallPayload(id, targetID string) (string, error) {
	if !validNativeID(id) || !validNativeID(targetID) || strings.EqualFold(id, targetID) {
		return "", fmt.Errorf("invalid native recall ID")
	}
	// The original includes a copy of the original JSON in m. Its ID is
	// sufficient to identify the message without retaining the removed text.
	original, err := json.Marshal(newChatPayload(targetID, []MsgDataTxt{}))
	if err != nil {
		return "", err
	}
	return encodeChatPayload(MsgJSON{App: "shiyeline", ID: id, Type: "4", Ver: "6.0", Font: messageFont{Size: "10"}, Dt: []MsgDataTxt{
		{Txt: &MsgTxt{T: "compatible", V: "对方请求撤回一条消息，当前版本不支持。请升级到最新版本。"}},
		{Retrieve: &recallTarget{ID: targetID, Original: string(original)}},
	}})
}

func encodeChatPayload(payload MsgJSON) (string, error) {
	jb, err := json.MarshalIndent(payload, "", "   ")
	if err != nil {
		return "", err
	}

	// Native 3.4.3055 uses named entities and literal line breaks in the XML
	// text node. Match that representation; its parser is stricter than
	// encoding/xml even though both forms are valid XML.
	messageText := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		" ", "&nbsp;",
	).Replace(string(jb) + "\n")
	return strings.ReplaceAll(messageText, "&quot;:", "&quot;&nbsp;:"), nil
}

func ParseSendMsg(data []byte) (msgID uint64, text string, id string, err error) {
	m, err := ParseDirectMessage(data)
	if err != nil {
		return 0, "", "", err
	}
	return m.PacketID, m.Text(), m.ID, nil
}

func (m DirectMessage) Text() string {
	var text strings.Builder
	for _, part := range m.Parts {
		text.WriteString(part.Text)
	}
	return text.String()
}

func ParseDirectMessage(data []byte) (DirectMessage, error) {
	var x XMLSendMsg
	if err := decodeNativeXML(data, "X_SEND_MSG", &x); err != nil {
		return DirectMessage{}, err
	}
	m, err := parseChatPayload(x.MsgID, x.Msg)
	if x.Offline == 1 && x.MsgTime > 0 && x.MsgTime <= 253402300799 {
		m.OfflineAt = time.Unix(int64(x.MsgTime), 0)
	}
	return m, err
}

func parseChatPayload(packet uint64, content string) (DirectMessage, error) {
	return parseChatParts(packet, content, false)
}

func parseChatParts(packet uint64, content string, group bool) (DirectMessage, error) {
	m := DirectMessage{PacketID: packet}
	var mj struct {
		Dt   []MsgDataTxt `json:"dt"`
		ID   string       `json:"id"`
		Type string       `json:"type"`
	}
	if err := json.Unmarshal([]byte(content), &mj); err != nil {
		if strings.HasPrefix(strings.TrimSpace(content), "{") {
			return DirectMessage{}, err
		}
		if content != "" {
			m.Parts = []MessagePart{{Text: content}}
		}
		return m, nil
	}
	m.ID = mj.ID
	if mj.Type == "4" {
		for _, dt := range mj.Dt {
			if dt.Retrieve == nil {
				continue
			}
			target := dt.Retrieve.ID
			if m.RecallID != "" || !validNativeID(m.ID) || !validNativeID(target) || strings.EqualFold(target, m.ID) {
				return DirectMessage{}, fmt.Errorf("invalid native recall target")
			}
			m.RecallID = strings.ToLower(target)
		}
		if m.RecallID == "" {
			return DirectMessage{}, fmt.Errorf("native recall has no target")
		}
		return m, nil // A recall's compatibility notice is not a new chat message.
	}
	var hasFile bool
	if group {
		for _, dt := range mj.Dt {
			if _, ok := dt.File.reference(); ok {
				hasFile = true
				break
			}
		}
	}
	for _, dt := range mj.Dt {
		if dt.Txt != nil && dt.Txt.V != "" && !(hasFile && dt.Txt.T == "compatible") {
			m.Parts = append(m.Parts, MessagePart{Text: dt.Txt.V})
		}
		if dt.Img != nil && dt.Img.T == "feihu" {
			if ref, ok := parseChatImageRef(dt.Img.V); ok {
				m.Parts = append(m.Parts, MessagePart{Image: &ref})
			} else if text, ok := parseStockEmoticon(dt.Img.V); ok {
				m.Parts = append(m.Parts, MessagePart{Text: text})
			}
		}
		if hasFile {
			if ref, ok := dt.File.reference(); ok {
				m.Parts = append(m.Parts, MessagePart{File: &ref})
			}
		}
	}
	return m, nil
}

func parseChatImageRef(value string) (ChatImageRef, bool) {
	key, hash, ok := strings.Cut(value, "|")
	id, err := strconv.ParseUint(key, 10, 32)
	if !ok || err != nil || id == 0 || id == avatarSelector || len(hash) != 32 {
		return ChatImageRef{}, false
	}
	ref := ChatImageRef{Selector: uint32(id)}
	_, err = hex.Decode(ref.Digest[:], []byte(hash))
	return ref, err == nil
}

func FormatSendMsgAck(msgID uint64) []byte {
	return []byte(fmt.Sprintf(`<X_SEND_MSG_ACK docver="1"><MSG_ID>%d</MSG_ID></X_SEND_MSG_ACK>`, msgID))
}

func ParseSendMsgAck(data []byte) (uint64, error) {
	var ack XMLSendMsgAck
	if err := decodeNativeXML(data, "X_SEND_MSG_ACK", &ack); err != nil {
		return 0, err
	}
	return ack.MsgID, nil
}

func decodeNativeXML(data []byte, root string, out any) error {
	// The original 3.4 client uses &nbsp; for JSON indentation in XML.
	// Decode that as a space; reject other unknown entities and malformed XML.
	data = bytes.Trim(data, "\x00\r\n\t ")
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Entity = map[string]string{"nbsp": " "}
	for {
		token, err := d.Token()
		if err != nil {
			return err
		}
		if start, ok := token.(xml.StartElement); ok {
			if start.Name.Local != root || start.Name.Space != "" {
				return fmt.Errorf("unexpected native XML record %q", start.Name.Local)
			}
			return d.DecodeElement(out, &start)
		}
	}
}

type XMLSendFile struct {
	XMLName    xml.Name `xml:"X_SEND_FILE"`
	DocVer     string   `xml:"docver,attr"`
	TaskID     uint64   `xml:"TASK_ID"`
	FileID     uint64   `xml:"FILE_ID"`
	Name       string   `xml:"NAME"`
	Pwd        string   `xml:"PWD"`
	Size       int64    `xml:"SIZE"`
	LastModify int64    `xml:"LAST_MODIFY"`
	Offline    int      `xml:"OFFLINE"`
}

func FormatSendFile(taskID, fileID uint64, name string, size, lastMod int64) []byte {
	if lastMod <= 0 {
		// The native 3.4.3055 file handler rejects epoch-zero metadata by
		// dropping the authenticated ENet session. Always send real time
		// metadata when a caller does not have a source timestamp.
		lastMod = time.Now().Unix()
	}
	// LAST_MODIFY is a Windows FILETIME, here derived from the source's
	// stable modification time rather than from the time of each retry.
	const windowsEpoch = 11644473600
	if lastMod > (1<<63-1)/10000000-windowsEpoch {
		lastMod = time.Now().Unix()
	}
	stamp := (lastMod + windowsEpoch) * 10000000
	payload, _ := xml.Marshal(XMLSendFile{DocVer: "1", TaskID: taskID, FileID: fileID, Name: name, Size: size, LastModify: stamp})
	return payload
}

func ParseSendFile(data []byte) (XMLSendFile, error) {
	var x XMLSendFile
	if err := decodeNativeXML(data, "X_SEND_FILE", &x); err != nil {
		return XMLSendFile{}, err
	}
	// The original client sends its local Windows path. Only the last
	// component belongs in the offer, even on receivers running on Unix.
	x.Name = x.Name[strings.LastIndexAny(x.Name, "/\\")+1:]
	return x, nil
}

type XMLOperateRecvFile struct {
	XMLName xml.Name `xml:"X_OPERATE_RECV_FILE"`
	DocVer  string   `xml:"docver,attr"`
	TaskID  uint64   `xml:"TASK_ID"`
	OP      uint32   `xml:"OP"`
}

const (
	fileSendCancel        uint32 = 1
	fileReceiveAccept     uint32 = 2
	fileReceiveStopLegacy uint32 = 3 // old GoIM compatibility endpoint
	fileReceiveStop       uint32 = 4 // stock client uses this before and after starting a download
)

func FormatOperateRecvFile(taskID uint64, op uint32) []byte {
	return []byte(fmt.Sprintf(`<X_OPERATE_RECV_FILE docver="1"><TASK_ID>%d</TASK_ID><OP>%d</OP></X_OPERATE_RECV_FILE>`, taskID, op))
}

func ParseOperateRecvFile(data []byte) (taskID uint64, op uint32, err error) {
	var x XMLOperateRecvFile
	err = decodeNativeXML(data, "X_OPERATE_RECV_FILE", &x)
	return x.TaskID, x.OP, err
}

type XMLOperateSendFile struct {
	XMLName xml.Name `xml:"X_OPERATE_SEND_FILE"`
	DocVer  string   `xml:"docver,attr"`
	TaskID  uint64   `xml:"TASK_ID"`
	OP      uint32   `xml:"OP"`
}

func FormatOperateSendFile(taskID uint64, op uint32) []byte {
	return []byte(fmt.Sprintf(`<X_OPERATE_SEND_FILE docver="1"><TASK_ID>%d</TASK_ID><OP>%d</OP></X_OPERATE_SEND_FILE>`, taskID, op))
}

func ParseOperateSendFile(data []byte) (taskID uint64, op uint32, err error) {
	var x XMLOperateSendFile
	err = decodeNativeXML(data, "X_OPERATE_SEND_FILE", &x)
	return x.TaskID, x.OP, err
}

type XMLProgressRecvFile struct {
	XMLName   xml.Name `xml:"X_PROGRESS_RECV_FILE"`
	DocVer    string   `xml:"docver,attr"`
	TaskID    uint64   `xml:"TASK_ID"`
	TotalSize int64    `xml:"TOTAL_SIZE"`
	RecvdSize int64    `xml:"RECVD_SIZE"`
	Speed     string   `xml:"SPEED"`
}

func FormatProgressRecvFile(taskID uint64, total, recvd int64, speed string) []byte {
	return []byte(fmt.Sprintf(`<X_PROGRESS_RECV_FILE docver="1"><TASK_ID>%d</TASK_ID><TOTAL_SIZE>%d</TOTAL_SIZE><RECVD_SIZE>%d</RECVD_SIZE><SPEED>%s</SPEED></X_PROGRESS_RECV_FILE>`,
		taskID, total, recvd, speed))
}

func ParseProgressRecvFile(data []byte) (XMLProgressRecvFile, error) {
	var x XMLProgressRecvFile
	err := decodeNativeXML(data, "X_PROGRESS_RECV_FILE", &x)
	return x, err
}

type XMLReverseFileReq struct {
	XMLName xml.Name `xml:"X_REVERSE_FILE_REQ"`
	DocVer  string   `xml:"docver,attr"`
	TaskID  uint64   `xml:"TASK_ID"`
}

func FormatReverseFileReq(taskID uint64) []byte {
	return []byte(fmt.Sprintf(`<X_REVERSE_FILE_REQ docver="1"><TASK_ID>%d</TASK_ID></X_REVERSE_FILE_REQ>`, taskID))
}

func ParseReverseFileReq(data []byte) (uint64, error) {
	var x XMLReverseFileReq
	err := decodeNativeXML(data, "X_REVERSE_FILE_REQ", &x)
	return x.TaskID, err
}

type XMLChangeStatus struct {
	XMLName xml.Name `xml:"X_CHANGE_STATUS"`
	DocVer  string   `xml:"docver,attr"`
	Status  uint32   `xml:"STATUS"`
}

func FormatChangeStatus(status uint32) []byte {
	return []byte(fmt.Sprintf(`<X_CHANGE_STATUS docver="1"><STATUS>%d</STATUS></X_CHANGE_STATUS>`, status))
}

func ParseChangeStatus(data []byte) (uint32, error) {
	var x XMLChangeStatus
	err := decodeNativeXML(data, "X_CHANGE_STATUS", &x)
	return x.Status, err
}

const (
	StatusOnline uint32 = iota
	StatusBusy
	StatusRightBack
	StatusAway
	StatusInvisible = ^uint32(0) // local-only: native 3.4 has no invisible state
)

type wireAvatar struct {
	ID   uint64 `xml:"id,attr"`
	Hash string `xml:",chardata"`
}

type wireProfile struct {
	CorpID       string     `xml:"CORP_ID"`
	CorpTime     uint64     `xml:"CORP_TIME"`
	Name         string     `xml:"NAME"`
	Group        string     `xml:"GROUP"`
	Company      string     `xml:"CORPORATION"`
	Department   string     `xml:"DEPARTMENT"`
	Sex          uint32     `xml:"SEX"`
	Post         string     `xml:"POST"`
	Phone        string     `xml:"TEL"`
	Mobile       string     `xml:"MOBILE"`
	Email        string     `xml:"EMAIL"`
	Sign         string     `xml:"SIGN"`
	SysFace      uint64     `xml:"SYS_FACE"`
	UserFace     wireAvatar `xml:"USER_FACE"`
	Status       uint32     `xml:"STATUS"`
	ActiveSend   uint32     `xml:"ACTIVE_SEND"`
	OnlinePopup  uint32     `xml:"ONLINE_POPUP"`
	HideIP       uint32     `xml:"HIDE_IP"`
	ColorName    uint32     `xml:"COLOR_NAME"`
	SortName     uint32     `xml:"SORT_NAME"`
	DecorateName uint32     `xml:"DECORATE_NAME"`
}

type wireFeatures struct {
	Major            int    `xml:"MAJOR"`
	Minor            int    `xml:"MINOR"`
	Build            int    `xml:"BUILD"`
	App              string `xml:"APP"`
	Message          string `xml:"MSG"`
	File             string `xml:"FILE_TRAN"`
	Folder           string `xml:"FOLDER_TRAN"`
	Share            string `xml:"FILE_SHARE"`
	Group            string `xml:"QGROUP"`
	GroupMsg         string `xml:"QGROUP_MSG"`
	GroupFile        string `xml:"QGROUP_FILE"`
	GroupFolder      string `xml:"QGROUP_FOLDER"`
	OfflineMsg       string `xml:"OFFLINE_MSG"`
	OfflineFile      string `xml:"OFFLINE_FILE"`
	LANUpdate        string `xml:"LAN_UPDATE"`
	HideRecord       string `xml:"HIDE_RECORD"`
	RemoteAssistance string `xml:"REMOTE_ASSISTANCE"`
	GoIM             bool   `xml:"GOIM,omitempty"`
}

type XMLHandshark struct {
	XMLName  xml.Name     `xml:"X_HANDSHARK"`
	DocVer   string       `xml:"docver,attr"`
	Features wireFeatures `xml:"FEATURE"`
	Info     wireProfile  `xml:"INFO"`
	Net      struct {
		FileTranTCPPort           uint16 `xml:"FILE_TRAN_TCP_PORT"`
		FileTranENetPort          uint16 `xml:"FILE_TRAN_ENET_PORT"`
		FolderTranTCPPort         uint16 `xml:"FOLDER_TRAN_TCP_PORT"`
		FolderTranENetPort        uint16 `xml:"FOLDER_TRAN_ENET_PORT"`
		FileTranTCPReversePort    uint16 `xml:"FILE_TRAN_TCP_REVERSE_PORT"`
		FileTranENetReversePort   uint16 `xml:"FILE_TRAN_ENET_REVERSE_PORT"`
		FolderTranTCPReversePort  uint16 `xml:"FOLDER_TRAN_TCP_REVERSE_PORT"`
		FolderTranENetReversePort uint16 `xml:"FOLDER_TRAN_ENET_REVERSE_PORT"`
		LANUpdateHTTPPort         uint16 `xml:"LAN_UPDATE_HTTP_PORT"`
	} `xml:"NET"`
}

func formatProfile(info wireProfile, tcp, udp uint16, goim bool) []byte {
	return formatProfileWithReverse(info, tcp, 0, udp, goim)
}

func formatProfileWithReverse(info wireProfile, tcp, reverse, udp uint16, goim bool) []byte {
	return formatProfileWithFolder(info, tcp, reverse, udp, 0, goim)
}

func profileDefaults(info wireProfile) wireProfile {
	if info.Group == "" {
		// Native 3.4.3055 rejects an empty contact group even though the XML
		// field is optional on the wire.
		info.Group = "Default"
	}
	if info.SysFace == 0 {
		info.SysFace = 401
	}
	if info.ActiveSend == 0 {
		info.ActiveSend = 1
	}
	return info
}

func formatProfileWithFolder(info wireProfile, tcp, reverse, udp, folder uint16, goim bool) []byte {
	x := XMLHandshark{DocVer: "1", Info: profileDefaults(info), Features: wireFeatures{Major: 3, Minor: 4, Build: 3055,
		App: "1", Message: "2:1", File: "1", GoIM: goim,
		Folder: "2:1", Group: "2:1", GroupMsg: "2:1", OfflineMsg: "1", OfflineFile: "1"}}
	x.Net.FileTranTCPPort, x.Net.FileTranENetPort = tcp, udp
	if folder != 0 {
		x.Net.FolderTranTCPPort = bits.ReverseBytes16(folder)
		x.Net.FolderTranTCPReversePort = bits.ReverseBytes16(folder)
	}
	if tcp == AvatarPort {
		x.Net.FileTranTCPPort = bits.ReverseBytes16(tcp)
	}
	if reverse != 0 {
		// Native 3.4.3055 writes the TCP port in network byte order and reads
		// the numeric XML value back. Preserve that wire quirk so the original
		// client dials the real reverse listener.
		x.Net.FileTranTCPReversePort = bits.ReverseBytes16(reverse)
	}
	out, _ := xml.Marshal(x)
	return out
}

func FormatHandshark(name, group, sign string, status uint32, tcpPort, enetPort uint16, faceID uint64, faceHash string) []byte {
	return formatProfile(wireProfile{Name: name, Group: group, Sign: sign, Status: status, UserFace: wireAvatar{faceID, faceHash}}, tcpPort, enetPort, false)
}

func ParseHandshark(data []byte) (XMLHandshark, error) {
	var profile XMLHandshark
	err := decodeNativeXML(data, "X_HANDSHARK", &profile)
	return profile, err
}

type profileChange struct {
	XMLName xml.Name `xml:"X_CHANGE_INFO"`
	DocVer  string   `xml:"docver,attr"`
	wireProfile
}

func formatProfileChange(profile wireProfile) []byte {
	out, _ := xml.Marshal(profileChange{DocVer: "1", wireProfile: profileDefaults(profile)})
	return out
}

type wireReady struct {
	XMLName xml.Name `xml:"X_READY"`
	DocVer  string   `xml:"docver,attr"`
	UserID  string   `xml:"USER_ID"`
	Param   string   `xml:"PARAM"`
}

func formatReady(id string) []byte {
	out, _ := xml.Marshal(wireReady{DocVer: "1", UserID: id, Param: "0"})
	return out
}

func formatHeartbeat() []byte {
	return []byte(`<X_HEARTBEAT docver="1" />`)
}
