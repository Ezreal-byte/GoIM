package neiwangtong

import (
	"encoding/xml"
	"errors"
	"strings"
)

const CmdSendFolder uint32 = 1013

const (
	CmdOperateSendFolder uint32 = 1014
	CmdOperateRecvFolder uint32 = 1015
)

type FolderOffer struct {
	XMLName     xml.Name `xml:"X_SEND_FOLDER"`
	DocVer      string   `xml:"docver,attr"`
	TaskID      string   `xml:"TASK_ID"`
	Name        string   `xml:"FOLDER_NAME"`
	Size        int64    `xml:"FOLDER_SIZE"`
	MaxItemPath int      `xml:"MAX_ITEM_PATH"`
	MaxItemSize int64    `xml:"MAX_ITEM_SIZE"`
	Offline     int      `xml:"OFFLINE"`
}

func ParseFolderOffer(data []byte) (FolderOffer, error) {
	var offer FolderOffer
	if err := decodeNativeXML(data, "X_SEND_FOLDER", &offer); err != nil {
		return FolderOffer{}, err
	}
	offer.Name = offer.Name[strings.LastIndexAny(offer.Name, "/\\")+1:]
	if !validFolderOffer(offer) {
		return FolderOffer{}, errors.New("invalid native folder offer")
	}
	return offer, nil
}

func validFolderOffer(offer FolderOffer) bool {
	return validNativeID(offer.TaskID) && validDirectoryComponent(offer.Name) &&
		offer.Size >= 0 && offer.MaxItemSize >= 0 && offer.MaxItemSize <= offer.Size &&
		offer.MaxItemPath >= 0 && offer.MaxItemPath <= 32768
}

func (s *Service) SendFolderOffer(peerID string, offer FolderOffer) error {
	if !validFolderOffer(offer) {
		return errors.New("invalid native folder offer")
	}
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	offer.DocVer = "1"
	// Stock 3.4 extracts the folder label from a Windows path in the offer.
	// Use a neutral path so the sender's local folder layout stays private.
	offer.Name = `C:\` + offer.Name
	payload, err := xml.Marshal(offer)
	if err != nil {
		return err
	}
	return s.enet.SendAppCommand(control, CmdSendFolder, payload)
}

// AcceptFolderOffer acknowledges that the recipient selected a directory
// offer. The separate folder data channel handles the contents.
func (s *Service) AcceptFolderOffer(peerID, taskID string) error {
	return s.folderTaskCommand(peerID, taskID, CmdOperateRecvFolder, "X_OPERATE_RECV_FOLDER", "<OP>2</OP>")
}

func (s *Service) folderTaskCommand(peerID, taskID string, kind uint32, root, body string) error {
	if !validNativeID(taskID) {
		return errors.New("invalid native folder task")
	}
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	payload := []byte("<" + root + " docver=\"1\"><TASK_ID>" + taskID + "</TASK_ID>" + body + "</" + root + ">")
	return s.enet.SendAppCommand(control, kind, payload)
}
