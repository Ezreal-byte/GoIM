package neiwangtong

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CmdHandshark        uint32 = 1000
	CmdChangeStatus     uint32 = 1001
	CmdChangeSign       uint32 = 1002
	CmdAvatarOffer      uint32 = 1003
	CmdSendMsg          uint32 = 1004
	CmdSendMsgAck       uint32 = 1005
	CmdSendFile         uint32 = 1009
	CmdOperateSendFile  uint32 = 1010
	CmdOperateRecvFile  uint32 = 1011
	CmdProgressRecvFile uint32 = 1012
	CmdHeartbeat        uint32 = 1016
	CmdQuit             uint32 = 1017
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

type MsgJSON struct {
	App  string       `json:"app"`
	Dt   []MsgDataTxt `json:"dt"`
	ID   string       `json:"id"`
	Type string       `json:"type"`
	Ver  string       `json:"ver"`
}

type MsgDataTxt struct {
	Txt *MsgTxt `json:"txt,omitempty"`
}

type MsgTxt struct {
	T string `json:"t"`
	V string `json:"v"`
}

func FormatSendMsg(msgID uint64, text string) ([]byte, error) {
	var randomID [16]byte
	_, _ = rand.Read(randomID[:])
	hexID := hex.EncodeToString(randomID[:])

	mj := MsgJSON{
		App: "shiyeline",
		Dt: []MsgDataTxt{
			{
				Txt: &MsgTxt{
					T: "normal",
					V: text,
				},
			},
		},
		ID:   hexID,
		Type: "0",
		Ver:  "6.0",
	}
	jb, err := json.Marshal(mj)
	if err != nil {
		return nil, err
	}

	xmlStr := fmt.Sprintf(`<X_SEND_MSG docver="1"><MSG_ID>%d</MSG_ID><RECEIPT>0</RECEIPT><MSG>%s</MSG><MSG_TIME>%d</MSG_TIME><OFFLINE>0</OFFLINE><HIDE_RECORD>0</HIDE_RECORD></X_SEND_MSG>`,
		msgID, string(jb), time.Now().Unix())
	return []byte(xmlStr), nil
}

func ParseSendMsg(data []byte) (msgID uint64, text string, id string, err error) {
	s := string(data)
	start := strings.Index(s, "<X_SEND_MSG")
	end := strings.Index(s, "</X_SEND_MSG>")
	if start < 0 || end < 0 {
		return 0, "", "", errors.New("no X_SEND_MSG tag found")
	}
	snippet := s[start : end+len("</X_SEND_MSG>")]
	var x XMLSendMsg
	if err := xml.Unmarshal([]byte(snippet), &x); err != nil {
		return 0, "", "", err
	}
	var mj MsgJSON
	if err := json.Unmarshal([]byte(x.Msg), &mj); err != nil {
		return x.MsgID, x.Msg, "", nil
	}
	var sb strings.Builder
	for _, dt := range mj.Dt {
		if dt.Txt != nil {
			sb.WriteString(dt.Txt.V)
		}
	}
	return x.MsgID, sb.String(), mj.ID, nil
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
	return []byte(fmt.Sprintf(`<X_SEND_FILE docver="1"><TASK_ID>%d</TASK_ID><FILE_ID>%d</FILE_ID><NAME>%s</NAME><PWD></PWD><SIZE>%d</SIZE><LAST_MODIFY>%d</LAST_MODIFY><OFFLINE>0</OFFLINE></X_SEND_FILE>`,
		taskID, fileID, name, size, lastMod))
}

func ParseSendFile(data []byte) (XMLSendFile, error) {
	s := string(data)
	start := strings.Index(s, "<X_SEND_FILE")
	end := strings.Index(s, "</X_SEND_FILE>")
	if start < 0 || end < 0 {
		return XMLSendFile{}, errors.New("no X_SEND_FILE tag found")
	}
	var x XMLSendFile
	err := xml.Unmarshal([]byte(s[start:end+len("</X_SEND_FILE>")]), &x)
	return x, err
}

type XMLOperateRecvFile struct {
	XMLName xml.Name `xml:"X_OPERATE_RECV_FILE"`
	DocVer  string   `xml:"docver,attr"`
	TaskID  uint64   `xml:"TASK_ID"`
	OP      uint32   `xml:"OP"`
}

func FormatOperateRecvFile(taskID uint64, op uint32) []byte {
	return []byte(fmt.Sprintf(`<X_OPERATE_RECV_FILE docver="1"><TASK_ID>%d</TASK_ID><OP>%d</OP></X_OPERATE_RECV_FILE>`, taskID, op))
}

func ParseOperateRecvFile(data []byte) (taskID uint64, op uint32, err error) {
	s := string(data)
	start := strings.Index(s, "<X_OPERATE_RECV_FILE")
	end := strings.Index(s, "</X_OPERATE_RECV_FILE>")
	if start < 0 || end < 0 {
		return 0, 0, errors.New("no X_OPERATE_RECV_FILE tag found")
	}
	var x XMLOperateRecvFile
	err = xml.Unmarshal([]byte(s[start:end+len("</X_OPERATE_RECV_FILE>")]), &x)
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
	s := string(data)
	start := strings.Index(s, "<X_PROGRESS_RECV_FILE")
	end := strings.Index(s, "</X_PROGRESS_RECV_FILE>")
	if start < 0 || end < 0 {
		return XMLProgressRecvFile{}, errors.New("no X_PROGRESS_RECV_FILE tag found")
	}
	var x XMLProgressRecvFile
	err := xml.Unmarshal([]byte(s[start:end+len("</X_PROGRESS_RECV_FILE>")]), &x)
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
	s := string(data)
	start := strings.Index(s, "<X_REVERSE_FILE_REQ")
	end := strings.Index(s, "</X_REVERSE_FILE_REQ>")
	if start < 0 || end < 0 {
		return 0, errors.New("no X_REVERSE_FILE_REQ tag found")
	}
	var x XMLReverseFileReq
	err := xml.Unmarshal([]byte(s[start:end+len("</X_REVERSE_FILE_REQ>")]), &x)
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
	s := string(data)
	start := strings.Index(s, "<X_CHANGE_STATUS")
	end := strings.Index(s, "</X_CHANGE_STATUS>")
	if start < 0 || end < 0 {
		return 0, errors.New("no X_CHANGE_STATUS tag found")
	}
	var x XMLChangeStatus
	err := xml.Unmarshal([]byte(s[start:end+len("</X_CHANGE_STATUS>")]), &x)
	return x.Status, err
}

type XMLHandshark struct {
	XMLName xml.Name `xml:"X_HANDSHARK"`
	DocVer  string   `xml:"docver,attr"`
	Info    struct {
		Name     string `xml:"NAME"`
		Group    string `xml:"GROUP"`
		Sign     string `xml:"SIGN"`
		Sex      uint32 `xml:"SEX"`
		Status   uint32 `xml:"STATUS"`
		UserFace struct {
			ID   uint64 `xml:"id,attr"`
			Hash string `xml:",chardata"`
		} `xml:"USER_FACE"`
	} `xml:"INFO"`
	Net struct {
		FileTranTCPPort  uint16 `xml:"FILE_TRAN_TCP_PORT"`
		FileTranENetPort uint16 `xml:"FILE_TRAN_ENET_PORT"`
	} `xml:"NET"`
}

func FormatHandshark(name, group, sign string, status uint32, tcpPort, enetPort uint16, faceID uint64, faceMD5 string) []byte {
	return []byte(fmt.Sprintf(`<X_HANDSHARK docver="1"><FEATURE><MAJOR>3</MAJOR><MINOR>4</MINOR><BUILD>3055</BUILD><APP>ShiYeLine</APP><MSG>1</MSG><FILE_TRAN>1</FILE_TRAN><FOLDER_TRAN>1</FOLDER_TRAN><REMOTE_ASSISTANCE>1</REMOTE_ASSISTANCE><FILE_SHARE>1</FILE_SHARE><QGROUP>1</QGROUP><QGROUP_MSG>1</QGROUP_MSG><QGROUP_FILE>1</QGROUP_FILE><QGROUP_FOLDER>1</QGROUP_FOLDER><OFFLINE_MSG>1</OFFLINE_MSG><OFFLINE_FILE>1</OFFLINE_FILE><LAN_UPDATE>0</LAN_UPDATE><HIDE_RECORD>0</HIDE_RECORD></FEATURE><INFO><CORP_ID></CORP_ID><CORP_TIME>0</CORP_TIME><NAME>%s</NAME><GROUP>%s</GROUP><CORPORATION></CORPORATION><DEPARTMENT></DEPARTMENT><SEX>1</SEX><POST></POST><TEL></TEL><MOBILE></MOBILE><EMAIL></EMAIL><SIGN>%s</SIGN><SYS_FACE>0</SYS_FACE><USER_FACE id="%d">%s</USER_FACE><STATUS>%d</STATUS><ACTIVE_SEND>0</ACTIVE_SEND><ONLINE_POPUP>0</ONLINE_POPUP><HIDE_IP>0</HIDE_IP><COLOR_NAME>0</COLOR_NAME><SORT_NAME>0</SORT_NAME><DECORATE_NAME>0</DECORATE_NAME></INFO><NET><FILE_TRAN_TCP_PORT>%d</FILE_TRAN_TCP_PORT><FILE_TRAN_ENET_PORT>%d</FILE_TRAN_ENET_PORT><FOLDER_TRAN_TCP_PORT>0</FOLDER_TRAN_TCP_PORT><FOLDER_TRAN_ENET_PORT>0</FOLDER_TRAN_ENET_PORT><FILE_TRAN_TCP_REVERSE_PORT>%d</FILE_TRAN_TCP_REVERSE_PORT><FILE_TRAN_ENET_REVERSE_PORT>0</FILE_TRAN_ENET_REVERSE_PORT><FOLDER_TRAN_TCP_REVERSE_PORT>0</FOLDER_TRAN_TCP_REVERSE_PORT><FOLDER_TRAN_ENET_REVERSE_PORT>0</FOLDER_TRAN_ENET_REVERSE_PORT><LAN_UPDATE_HTTP_PORT>0</LAN_UPDATE_HTTP_PORT></NET></X_HANDSHARK>`,
		name, group, sign, faceID, faceMD5, status, tcpPort, enetPort, tcpPort))
}

func ParseHandshark(data []byte) (XMLHandshark, error) {
	s := string(data)
	start := strings.Index(s, "<X_HANDSHARK")
	end := strings.Index(s, "</X_HANDSHARK>")
	if start < 0 || end < 0 {
		return XMLHandshark{}, errors.New("no X_HANDSHARK tag found")
	}
	var x XMLHandshark
	err := xml.Unmarshal([]byte(s[start:end+len("</X_HANDSHARK>")]), &x)
	return x, err
}
