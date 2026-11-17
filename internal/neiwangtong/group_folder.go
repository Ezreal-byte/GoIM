package neiwangtong

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"net/netip"
	"strings"
	"time"
)

const (
	cmdGroupFolderRequest  uint32 = 5006
	cmdGroupFolderResponse uint32 = 5007
)

type groupFolderRequest struct {
	XMLName  xml.Name `xml:"X_SHARE_DOWNLOAD_FOLDER"`
	DocVer   string   `xml:"docver,attr"`
	ID       uint64   `xml:"REQ_ID"`
	ShareID  string   `xml:"SHARE_ID"`
	Path     string   `xml:"SHARE_PATH"`
	Password string   `xml:"SHARE_PWD"`
	Channel  string   `xml:"CHANNEL_ID"`
	Check    int      `xml:"CHECK_PWD"`
	Resume   int      `xml:"GO_ON"`
}

type groupFolderResponse struct {
	XMLName xml.Name `xml:"X_SHARE_DOWNLOAD_FOLDER_RSP"`
	DocVer  string   `xml:"docver,attr"`
	Error   int      `xml:"ERR"`
	ID      uint64   `xml:"REQ_ID"`
	Channel string   `xml:"CHANNEL_ID"`
}

type groupFolderOffer struct {
	group, task string
	directory   Directory
}

func (s *Service) HasGroupFolderOffer(peer, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.groupFolders[groupFileKey{peer, strings.ToLower(id)}]
	return ok
}

func (s *Service) RememberGroupFolder(peer, group, id, task string, directory Directory) error {
	if s.directories == nil || !validNativeID(peer) || !validNativeID(group) || !validNativeID(id) ||
		!validNativeID(task) || len(directory.items) == 0 {
		return errors.New("内网通群文件夹发送未就绪")
	}
	key := groupFileKey{peer, strings.ToLower(id)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	old, known := s.groupFolders[key]
	if !known && len(s.groupFolders) >= 1024 ||
		known && (old.group != group || old.task != task || old.directory.Path != directory.Path || old.directory.Stamp != directory.Stamp) {
		return errors.New("内网通群文件夹邀请不可用")
	}
	s.groupFolders[key] = groupFolderOffer{group: group, task: task, directory: directory}
	return nil
}

func (s *Service) SendGroupFolder(peer, group string, packet uint64, messageID, id, task string, directory Directory) error {
	body, err := formatGroupAttachment(group, packet, messageID,
		GroupFileRef{ID: id, Name: directory.Name, Folder: true})
	if err != nil {
		return err
	}
	control, _, err := s.control(peer)
	if err != nil {
		return err
	}
	if err := s.RememberGroupFolder(peer, group, id, task, directory); err != nil {
		return err
	}
	err = s.enet.SendAppCommand(control, CmdGroupText, body)
	if err != nil && !errors.Is(err, ErrDeliveryUncertain) {
		_ = s.CancelGroupFolder(peer, id)
	}
	return err
}

func (s *Service) onGroupFolderRequest(remote netip.AddrPort, peer string, data []byte) {
	var req groupFolderRequest
	if decodeNativeXML(data, "X_SHARE_DOWNLOAD_FOLDER", &req) != nil || req.ID == 0 || !validNativeID(req.ShareID) ||
		!validNativeID(req.Channel) || req.Path != "" || req.Password != "" || req.Check != 0 || req.Resume != 0 {
		return
	}
	select {
	case s.groupFileWorkers <- struct{}{}:
	default:
		return
	}
	go func() {
		defer func() { <-s.groupFileWorkers }()
		rsp := groupFolderResponse{DocVer: "1", ID: req.ID, Channel: req.Channel, Error: 1}
		key := groupFileKey{peer, strings.ToLower(req.ShareID)}
		task := strings.ToLower(req.Channel)
		s.mu.Lock()
		offer, known := s.groupFolders[key]
		s.mu.Unlock()
		var up *directorySend
		allowed := func() bool {
			s.mu.Lock()
			current, live := s.groupFolders[key]
			s.mu.Unlock()
			return live && current.task == offer.task && current.directory.Stamp == offer.directory.Stamp &&
				(s.cfg.CanServeGroupFile == nil || s.cfg.CanServeGroupFile(peer, offer.group))
		}
		if known && s.directories != nil && allowed() {
			dir, err := InspectDirectory(offer.directory.Path)
			if err == nil && dir.Stamp == offer.directory.Stamp {
				d := s.directories
				d.mu.Lock()
				if len(d.sends) < 1024 && d.sends[task] == nil {
					ctx, cancel := context.WithCancel(s.ctx)
					up = &directorySend{peer: peer, directory: dir, ctx: ctx, cancel: cancel,
						active: true, allowed: allowed, reportTask: offer.task}
					d.sends[task] = up
					rsp.Error = 0
				}
				d.mu.Unlock()
			}
		}
		response, _ := xml.Marshal(rsp)
		err := s.enet.SendAppCommand(remote, cmdGroupFolderResponse, response)
		if up == nil {
			return
		}
		if err != nil && !errors.Is(err, ErrDeliveryUncertain) || !allowed() {
			s.directories.finishSend(task, up)
			return
		}
		s.uploadDirectory(task, up)
	}()
}

func (s *Service) CancelGroupFolder(peer, id string) error {
	key := groupFileKey{peer, strings.ToLower(id)}
	s.mu.Lock()
	offer, known := s.groupFolders[key]
	delete(s.groupFolders, key)
	s.mu.Unlock()
	if known && s.directories != nil {
		s.directories.mu.Lock()
		for task, up := range s.directories.sends {
			if up.peer == peer && up.reportTask == offer.task {
				delete(s.directories.sends, task)
				up.cancel()
			}
		}
		s.directories.mu.Unlock()
	}
	return nil
}

func (s *Service) onGroupFolderResponse(peer string, data []byte) {
	var response groupFolderResponse
	if decodeNativeXML(data, "X_SHARE_DOWNLOAD_FOLDER_RSP", &response) != nil || response.ID == 0 {
		return
	}
	s.mu.Lock()
	reply := s.groupFolderReplies[receiptKey{peer, response.ID}]
	s.mu.Unlock()
	if reply != nil {
		select {
		case reply <- response:
		default:
		}
	}
}

func (s *Service) ReceiveGroupDirectory(ctx context.Context, peer, id, name, destination string,
	progress func(done, total int64)) (int64, error) {
	if !validNativeID(id) || !validDirectoryComponent(name) {
		return 0, errors.New("内网通群文件夹引用无效")
	}
	control, _, err := s.control(peer)
	if err != nil {
		return 0, err
	}
	var channel [16]byte
	if _, err := rand.Read(channel[:]); err != nil {
		return 0, err
	}
	task := hex.EncodeToString(channel[:])
	reqID := uint64(s.messageSeq.Add(1))
	if reqID == 0 {
		reqID = uint64(s.messageSeq.Add(1))
	}
	key := receiptKey{peer, reqID}
	reply := make(chan groupFolderResponse, 1)
	s.mu.Lock()
	s.groupFolderReplies[key] = reply
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.groupFolderReplies, key)
		s.mu.Unlock()
	}()
	request := func() error {
		data, _ := xml.Marshal(groupFolderRequest{DocVer: "1", ID: reqID, ShareID: strings.ToLower(id), Channel: task})
		if err := s.enet.SendAppCommand(control, cmdGroupFolderRequest, data); err != nil && !errors.Is(err, ErrDeliveryUncertain) {
			return err
		}
		timeout := time.NewTimer(5 * time.Second)
		defer timeout.Stop()
		select {
		case response := <-reply:
			if response.Error != 0 || response.Channel != task {
				return errors.New("对方的内网通群文件夹已不可用")
			}
			return nil
		case <-timeout.C:
			return errors.New("等待内网通群文件夹响应超时")
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
	var total int64
	metadata := func(size int64) {
		total = size
		if progress != nil {
			progress(0, total)
		}
	}
	advance := func(done int64) {
		if progress != nil {
			progress(done, total)
		}
	}
	return s.receiveDirectoryOffer(ctx, peer, FolderOffer{TaskID: task, Name: name}, destination, true, metadata, advance, request)
}
