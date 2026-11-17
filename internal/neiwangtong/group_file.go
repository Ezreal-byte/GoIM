package neiwangtong

import (
	"context"
	"encoding/xml"
	"errors"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	cmdGroupFileRequest  uint32 = 5004
	cmdGroupFileResponse uint32 = 5005
)

// GroupFileRef names one regular file offered in a native group message.
// Name is display metadata from the sender; it is never a filesystem path.
type GroupFileRef struct {
	ID, Name string
	Size     int64
	Folder   bool
}

type chatFileWire struct {
	Directory string `json:"a"`
	ID        string `json:"i"`
	Size      string `json:"s"`
	Type      string `json:"t"`
	Name      string `json:"v"`
}

func (f *chatFileWire) reference() (GroupFileRef, bool) {
	if f == nil || (f.Type != "offline_file" && f.Type != "offline_folder") || f.Directory != "false" || !validNativeID(f.ID) ||
		f.Name == "" || len(f.Name) > 1024 || !utf8.ValidString(f.Name) {
		return GroupFileRef{}, false
	}
	size, err := strconv.ParseInt(f.Size, 10, 64)
	if err != nil || size < 0 {
		return GroupFileRef{}, false
	}
	return GroupFileRef{ID: strings.ToLower(f.ID), Name: f.Name, Size: size, Folder: f.Type == "offline_folder"}, true
}

type groupFileKey struct{ peer, id string }

type groupFileOffer struct {
	group      string
	task, file uint64
	source     string
	sourceInfo os.FileInfo
}

func (s *Service) HasGroupFileOffer(peer, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.groupFiles[groupFileKey{peer, strings.ToLower(id)}]
	return ok
}

type groupFileRequest struct {
	XMLName  xml.Name `xml:"X_SHARE_DOWNLOAD_FILE"`
	DocVer   string   `xml:"docver,attr"`
	ID       uint64   `xml:"REQ_ID"`
	ShareID  string   `xml:"SHARE_ID"`
	Path     string   `xml:"SHARE_PATH"`
	Password string   `xml:"SHARE_PWD"`
	Check    int      `xml:"CHECK_PWD"`
	Resume   int      `xml:"GO_ON"`
}

type groupFileResponse struct {
	XMLName xml.Name `xml:"X_SHARE_DOWNLOAD_FILE_RSP"`
	DocVer  string   `xml:"docver,attr"`
	Error   int      `xml:"ERR"`
	ID      uint64   `xml:"REQ_ID"`
	FileID  uint64   `xml:"FILE_ID"`
	Size    int64    `xml:"FILE_SIZE"`
	ModTime int64    `xml:"LAST_MODIFY"`
}

// SendGroupFile registers only the chosen file, for this recipient. The data
// listener receives a numeric selector after the recipient requests the file.
func (s *Service) SendGroupFile(peer, group string, packet uint64, messageID string, ref GroupFileRef, task, file uint64, source string) error {
	if !validNativeID(messageID) || !validNativeID(ref.ID) || ref.Name == "" || len(ref.Name) > 255 ||
		strings.ContainsAny(ref.Name, "/\\:\x00") || task == 0 || task > maxFileSelector ||
		file == 0 || file > maxFileSelector || file == avatarSelector || ref.Size < 0 || ref.Folder {
		return errors.New("invalid native group file")
	}
	payload, err := formatGroupAttachment(group, packet, messageID, ref)
	if err != nil {
		return err
	}
	control, _, err := s.control(peer)
	if err != nil {
		return err
	}
	if err := s.RememberGroupFile(peer, group, ref, task, file, source); err != nil {
		return err
	}
	if err = s.enet.SendAppCommand(control, CmdGroupText, payload); err != nil && !errors.Is(err, ErrDeliveryUncertain) {
		s.dropFile(task)
	}
	return err
}

func formatGroupAttachment(group string, packet uint64, messageID string, ref GroupFileRef) ([]byte, error) {
	if !validNativeID(messageID) || !validNativeID(ref.ID) || !validDirectoryComponent(ref.Name) || ref.Size < 0 {
		return nil, errors.New("invalid native group attachment")
	}
	kind, size := "offline_file", ref.Size
	if ref.Folder {
		kind, size = "offline_folder", 0
	}
	body, err := encodeChatPayload(MsgJSON{App: "shiyeline", ID: messageID, Type: "0", Ver: "6.0", Font: messageFont{Size: "10"}, Dt: []MsgDataTxt{
		{Txt: &MsgTxt{T: "compatible", V: "对方给您发送了一个离线文件，当前版本不支持。请升级到最新版本接收此文件。"}},
		{File: &chatFileWire{Directory: "false", ID: ref.ID, Name: ref.Name,
			Type: kind, Size: strconv.FormatInt(size, 10)}},
	}})
	if err != nil {
		return nil, err
	}
	return formatGroupBody(group, packet, body)
}

// RememberGroupFile restores the chosen file's invitation after a restart.
// It does not announce a new message or grant access to any other file.
func (s *Service) RememberGroupFile(peer, group string, ref GroupFileRef, task, file uint64, source string) error {
	if !validNativeID(peer) || !validNativeID(group) || !validNativeID(ref.ID) || ref.Size < 0 ||
		task == 0 || task > maxFileSelector || file == 0 || file > maxFileSelector || file == avatarSelector {
		return errors.New("invalid native group file offer")
	}
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() || info.Size() != ref.Size {
		return errors.New("源文件已变化，请重新发送")
	}
	key := groupFileKey{peer, strings.ToLower(ref.ID)}
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return s.ctx.Err()
	}
	if old, ok := s.groupFiles[key]; (ok && (old.task != task || old.file != file || old.source != source || old.group != group)) ||
		(!ok && len(s.groupFiles) >= 1024) {
		s.mu.Unlock()
		return errors.New("内网通群文件邀请不可用")
	}
	for k, old := range s.groupFiles {
		if (old.task == task || old.file == file) && k != key {
			s.mu.Unlock()
			return errors.New("内网通群文件编号已被占用")
		}
	}
	if current, ok := s.files[task]; ok && (current.peerID != peer || current.fileID != file || current.size != ref.Size) {
		s.mu.Unlock()
		return errors.New("内网通任务编号已被占用")
	}
	s.groupFiles[key] = groupFileOffer{group: group, task: task, file: file, source: source, sourceInfo: info}
	s.mu.Unlock()
	return nil
}

func (s *Service) onGroupFileRequest(remote netip.AddrPort, peer string, data []byte) {
	var req groupFileRequest
	if decodeNativeXML(data, "X_SHARE_DOWNLOAD_FILE", &req) != nil || req.ID == 0 ||
		!validNativeID(req.ShareID) || req.Path != "" || req.Password != "" || req.Check != 0 || req.Resume != 0 {
		return
	}
	select {
	case s.groupFileWorkers <- struct{}{}:
	default:
		return
	}
	go func() {
		defer func() { <-s.groupFileWorkers }()
		response := groupFileResponse{DocVer: "1", ID: req.ID, Error: 1}
		key := groupFileKey{peer, strings.ToLower(req.ShareID)}
		s.mu.Lock()
		offer, ok := s.groupFiles[key]
		s.mu.Unlock()
		allowed := func() bool { return s.cfg.CanServeGroupFile == nil || s.cfg.CanServeGroupFile(peer, offer.group) }
		if ok && allowed() {
			if info, err := os.Stat(offer.source); err == nil && os.SameFile(info, offer.sourceInfo) &&
				info.Size() == offer.sourceInfo.Size() && info.ModTime().Equal(offer.sourceInfo.ModTime()) {
				if current, err := s.prepareFileOfferWithAccess(peer, offer.task, offer.file, info.Size(), offer.source, allowed); err == nil {
					s.mu.Lock()
					registered, live := s.groupFiles[key]
					if current == remote && live && registered.task == offer.task && registered.source == offer.source {
						response.Error, response.FileID, response.Size = 0, offer.file, info.Size()
						response.ModTime = info.ModTime().Unix()
					} else if !live {
						s.dropFileLocked(offer.task)
					}
					s.mu.Unlock()
				}
			}
		}
		msg, _ := xml.Marshal(response)
		_ = s.enet.SendAppCommand(remote, cmdGroupFileResponse, msg)
	}()
}

func (s *Service) onGroupFileResponse(peer string, data []byte) {
	var rsp groupFileResponse
	if decodeNativeXML(data, "X_SHARE_DOWNLOAD_FILE_RSP", &rsp) != nil || rsp.ID == 0 {
		return
	}
	s.mu.Lock()
	reply := s.groupFileReplies[receiptKey{peer, rsp.ID}]
	s.mu.Unlock()
	if reply != nil {
		select {
		case reply <- rsp:
		default:
		}
	}
}

func (s *Service) DownloadGroupFile(ctx context.Context, peer, id string, size int64, destination string, progress func(int64, int64)) error {
	if !validNativeID(id) || size < 0 {
		return errors.New("内网通群文件引用无效")
	}
	control, endpoint, err := s.control(peer)
	if err != nil {
		return err
	}
	requestID := uint64(s.messageSeq.Add(1))
	if requestID == 0 {
		requestID = uint64(s.messageSeq.Add(1))
	}
	key := receiptKey{peer, requestID}
	reply := make(chan groupFileResponse, 1)
	s.mu.Lock()
	s.groupFileReplies[key] = reply
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.groupFileReplies, key)
		s.mu.Unlock()
	}()
	req, _ := xml.Marshal(groupFileRequest{DocVer: "1", ID: requestID, ShareID: strings.ToLower(id)})
	if err := s.enet.SendAppCommand(control, cmdGroupFileRequest, req); err != nil && !errors.Is(err, ErrDeliveryUncertain) {
		return err
	}
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	select {
	case rsp := <-reply:
		if rsp.Error != 0 {
			return errors.New("对方的内网通群文件已不可用")
		}
		if rsp.Size != size || rsp.FileID == 0 || rsp.FileID > maxFileSelector || rsp.FileID == avatarSelector {
			return errors.New("内网通群文件下载响应与邀请不匹配")
		}
		return downloadFile(ctx, s.cfg.BindIP, endpoint, rsp.FileID, size, destination, true, progress)
	case <-timeout.C:
		return errors.New("等待内网通群文件响应超时")
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
