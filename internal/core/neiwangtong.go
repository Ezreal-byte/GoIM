package core

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"goim/internal/domain"
	"goim/internal/neiwangtong"
)

const transportNWT = domain.TransportNeiwangtong

func (n *Node) connectNeiwangtong(address string) error {
	target, err := netip.ParseAddrPort(address)
	if err != nil || !target.Addr().Unmap().Is4() {
		return errors.New("内网通连接需要 IPv4 地址，例如 192.168.1.20:9011")
	}
	n.mu.Lock()
	service := n.neiwangtong
	n.mu.Unlock()
	if service == nil {
		return errors.New("内网通互通服务未开启，请先在设置中启用")
	}
	if err := service.Probe(n.ctx, target, 9012, 9013); err != nil {
		return fmt.Errorf("连接内网通设备 %s 失败: %w", target, err)
	}
	n.notice("已连接内网通设备")
	return nil
}

func (n *Node) newNeiwangtong(port int, live *atomic.Bool) (*neiwangtong.Service, error) {
	s := n.snapshot(false)
	name := strings.TrimSpace(s.Settings.Nickname)
	if name == "" {
		name = "GoIM"
	}
	var bind netip.Addr
	if address, ok := n.listener.Addr().(*net.TCPAddr); ok {
		ip := address.AddrPort().Addr().Unmap()
		if ip.Is4() && !ip.IsUnspecified() {
			bind = ip
		}
	}

	var avatarBytes []byte
	if s.Self.Avatar != "" {
		if b, err := readAvatar(s.Self.Avatar); err == nil && len(b) <= 1<<20 {
			avatarBytes = b
		}
	}

	var service *neiwangtong.Service
	service, err := neiwangtong.Start(neiwangtong.Config{
		ID:               nativeIDForGoIM(s.Self.ID),
		GoIM:             true,
		Profile:          nativeLocalProfile(s.Self, name),
		DiscoveryPort:    port,
		ControlPort:      9012,
		TransferPort:     9013,
		BindIP:           bind,
		AvatarPNG:        avatarBytes,
		DisableBroadcast: true,
		AllowRemote: func(remote netip.AddrPort) bool {
			return n.allowedRemote(net.UDPAddrFromAddrPort(remote))
		},
		Interface: func() string {
			n.mu.Lock()
			defer n.mu.Unlock()
			name := n.data.State.Settings.Network
			if name == "auto" {
				return ""
			}
			return name
		},
		Callbacks: neiwangtong.Callbacks{
			Peer: func(p neiwangtong.Peer) {
				if live.Load() {
					n.receiveNeiwangtongPeer(p)
				}
			},
			Text: func(peerID string, packetID uint64, text string) {
				if live.Load() {
					n.receiveNeiwangtongText(peerID, packetID, text)
				}
			},
			Files: func(peerID string, offer neiwangtong.XMLSendFile) {
				if live.Load() {
					n.receiveNeiwangtongFiles(peerID, offer.TaskID, offer.FileID, offer.Name, offer.Size)
				}
			},
			FileProgress: func(peerID string, taskID uint64, done, total int64, complete bool, err error) {
				if live.Load() {
					n.receiveNeiwangtongProgress(peerID, taskID, done, total, complete, err)
				}
			},
			AvatarUpdated: func(peerID string, revision uint64, png []byte) {
				if live.Load() {
					n.receiveNeiwangtongAvatar(service, peerID, revision, png)
				}
			},
		},
	})
	return service, err
}

func nativeLocalProfile(p domain.Peer, name string) neiwangtong.Peer {
	info := neiwangtong.Peer{Name: name, Sign: p.Signature, Group: p.ContactGroup,
		Gender: p.Details.Gender, Company: p.Details.Company, Department: p.Details.Department,
		Phone: p.Details.Phone, Mobile: p.Details.Mobile, Email: p.Details.Email}
	switch p.Presence {
	case domain.PresenceAway:
		info.Status = neiwangtong.StatusAway
	case domain.PresenceBusy:
		info.Status = neiwangtong.StatusBusy
	case domain.PresenceInvisible:
		info.Status = neiwangtong.StatusInvisible
	}
	return info
}

func (n *Node) receiveNeiwangtongPeer(peer neiwangtong.Peer) {
	name := strings.TrimSpace(peer.Name)
	if peer.ID == "" || name == "" || len(peer.Name) > 160 || !utf8.ValidString(peer.Name) || peer.GoIM {
		return
	}
	presence := domain.PresenceOnline
	switch peer.Status {
	case neiwangtong.StatusBusy:
		presence = domain.PresenceBusy
	case neiwangtong.StatusRightBack, neiwangtong.StatusAway:
		presence = domain.PresenceAway
	}
	n.observeCompatibilityPeer(domain.Peer{
		ID: peer.ID, Name: name, Address: peer.Address, Online: peer.Online,
		Signature: peer.Sign, Presence: presence, Version: peer.Version,
		Details:   domain.ProfileDetails{Gender: peer.Gender, Company: peer.Company, Department: peer.Department, Phone: peer.Phone, Mobile: peer.Mobile, Email: peer.Email},
		Transport: transportNWT, ClientType: domain.ClientNeiwangtong,
	})
}

func (n *Node) receiveNeiwangtongText(peerID string, packetID uint64, text string) {
	if text == "" || len(text) > 256<<10 || !utf8.ValidString(text) {
		return
	}
	_ = n.change(func(s *domain.State) {
		peer := n.compatibilitySenderLocked(transportNWT, peerID)
		if peer.ID == "" {
			return
		}
		conversationID := ensureDirect(s, peer)
		messageID := legacyStableID("nwt-msg", peerID, fmt.Sprint(packetID), text)
		for _, existing := range s.Messages {
			if existing.ID == messageID {
				return
			}
		}
		addMessageState(s, domain.Message{
			ID:             messageID,
			ConversationID: conversationID,
			SenderID:       peer.ID,
			Kind:           "text",
			Text:           text,
			CreatedAt:      time.Now(),
			Status:         "delivered",
		})
	})
}

func (n *Node) receiveNeiwangtongFiles(peerID string, taskID, remoteFileID uint64, name string, size int64) {
	if size < 0 || strings.TrimSpace(name) == "" {
		return
	}
	_ = n.change(func(s *domain.State) {
		peer := n.compatibilitySenderLocked(transportNWT, peerID)
		if peer.ID == "" {
			return
		}
		conversationID := ensureDirect(s, peer)
		taskHex := fmt.Sprint(taskID)
		fileNumber := fmt.Sprint(remoteFileID)
		fileID := legacyStableID("nwt-file", peerID, taskHex, fileNumber)
		transferID := legacyStableID("nwt-transfer", peerID, taskHex, fileNumber)
		if fileIndex(s, fileID) >= 0 || transferIndex(s, transferID) >= 0 {
			return
		}
		receivedAt := time.Now()
		mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		file := domain.File{
			ID:             fileID,
			Name:           name,
			Size:           size,
			Hash:           strings.Repeat("0", 64),
			MIME:           mediaType,
			SenderID:       peer.ID,
			ConversationID: conversationID,
			CreatedAt:      receivedAt,
		}
		if !validFile(file) {
			return
		}
		s.Files = append(s.Files, file)
		s.Transfers = append(s.Transfers, domain.Transfer{
			ID:              transferID,
			FileID:          fileID,
			ConversationID:  conversationID,
			PeerID:          peer.ID,
			Direction:       "receive",
			Status:          "offered",
			Name:            file.Name,
			Size:            file.Size,
			UpdatedAt:       receivedAt,
			LegacyTransport: transportNWT,
			RemotePacket:    taskHex,
			RemoteFile:      fileNumber,
			RemotePeerID:    peerID,
		})
		addMessageState(s, domain.Message{
			ID:             fileID,
			ConversationID: conversationID,
			SenderID:       peer.ID,
			Kind:           "file",
			FileID:         fileID,
			Text:           name,
			CreatedAt:      file.CreatedAt,
			Status:         "delivered",
		})
	})
}

func (n *Node) receiveNeiwangtongProgress(peerID string, taskID uint64, done, total int64, complete bool, failure error) {
	taskHex := fmt.Sprint(taskID)
	status := "transferring"
	if complete {
		status = "completed"
	}
	problem := ""
	if failure != nil {
		status, problem = "failed", failure.Error()
		if errors.Is(failure, neiwangtong.ErrFileRejected) {
			status = "rejected"
		}
	}
	_ = n.change(func(s *domain.State) {
		for i := range s.Transfers {
			t := &s.Transfers[i]
			if t.LegacyTransport != transportNWT || t.RemotePacket != taskHex ||
				t.RemotePeerID != peerID || t.Direction != "send" || total != t.Size || done < 0 || done > total {
				continue
			}
			if terminalTransfer(t.Status) {
				return
			}
			t.Status = status
			t.Done = done
			t.Size = total
			t.Error = problem
			t.UpdatedAt = time.Now()
		}
	})
}

func (n *Node) receiveNeiwangtongAvatar(service *neiwangtong.Service, peerID string, revision uint64, png []byte) {
	n.async(func() {
		if !service.AvatarCurrent(peerID, revision) {
			return
		}
		path, err := n.cacheAvatar(peerID, png)
		if err != nil {
			return
		}
		_ = n.durable(func(data *persisted) []pending {
			if n.neiwangtong != service || !service.AvatarCurrent(peerID, revision) {
				return nil
			}
			canonical := n.compatibilitySenderLocked(transportNWT, peerID)
			if canonical.ID == "" {
				return nil
			}
			for i := range data.State.Peers {
				p := &data.State.Peers[i]
				if p.ID == canonical.ID || p.CompatibilityAliasOf == canonical.ID {
					p.Avatar = path
				}
			}
			return nil
		})
	})
}

func (n *Node) refreshNeiwangtongAvatar(peerID string) error {
	n.mu.Lock()
	service := n.neiwangtong
	wireID := wireCompatibilityID(&n.data, peerID, transportNWT)
	n.mu.Unlock()
	if service == nil || wireID == "" {
		return errors.New("刷新头像需要内网通原生连接")
	}
	return service.FetchAvatar(wireID)
}

func (n *Node) neiwangtongPending(peer string, value pending) error {
	n.mu.Lock()
	service := n.neiwangtong
	n.mu.Unlock()
	if service == nil {
		return neiwangtong.ErrNotConnected
	}
	e := value.Envelope
	switch e.Type {
	case "message":
		packet, _ := strconv.ParseUint(e.ID[:min(len(e.ID), 16)], 16, 64)
		return service.SendTextWithID(peer, max(packet, 1), e.Message.Text)
	case "file_offer", "file_retry":
		s := n.snapshot(false)
		index := transferIndex(&s, e.TransferID)
		if index < 0 {
			return nil
		}
		t := s.Transfers[index]
		if t.Revoked || terminalTransfer(t.Status) {
			return nil
		}
		f := s.File(t.FileID)
		if f.SourcePath == "" {
			n.setTransfer(t.ID, "failed", t.Done, 0, "源文件不可用")
			return nil
		}
		taskID, fileID := neiwangtongOutgoingIDs(t)
		if err := n.change(func(st *domain.State) {
			if idx := transferIndex(st, t.ID); idx >= 0 {
				st.Transfers[idx].RemotePacket = fmt.Sprint(taskID)
				st.Transfers[idx].RemoteFile = fmt.Sprint(fileID)
				st.Transfers[idx].Status = "offered"
				st.Transfers[idx].LegacyTransport = transportNWT
				st.Transfers[idx].RemotePeerID = peer
			}
		}); err != nil {
			return err
		}
		return service.SendFileOfferWithIDs(peer, taskID, fileID, f.Name, f.Size, time.Now().Unix(), f.SourcePath)
	case "file_status":
		if e.Status != "rejected" && e.Status != "cancelled" {
			return nil
		}
		s := n.snapshot(false)
		i := transferIndex(&s, e.TransferID)
		if i < 0 {
			return nil
		}
		t := s.Transfers[i]
		task, err := strconv.ParseUint(t.RemotePacket, 10, 64)
		if err != nil || t.LegacyTransport != transportNWT {
			return nil
		}
		if t.Direction == "send" {
			service.CancelSend(task)
			return nil
		}
		return service.RejectFile(peer, task)
	default:
		return nil
	}
}

func neiwangtongOutgoingIDs(t domain.Transfer) (taskID, fileID uint64) {
	parse := func(value string) uint64 {
		id, err := strconv.ParseUint(value, 10, 32)
		if err != nil || id == 0 {
			return 0
		}
		return id
	}
	taskID, fileID = parse(t.RemotePacket), parse(t.RemoteFile)
	if taskID == 0 {
		taskID = neiwangtongSelector("nwt-task", t.ID)
	}
	if fileID == 0 || fileID == taskID {
		fileID = neiwangtongSelector("nwt-file", t.ID)
		if fileID == taskID {
			fileID = fileID%0x3fffffff + 65536
		}
	}
	return taskID, fileID
}

func neiwangtongSelector(kind, transferID string) uint64 {
	value, _ := strconv.ParseUint(legacyStableID(kind, transferID)[:8], 16, 32)
	value &= 0x3fffffff
	if value < 65536 {
		value += 65536
	}
	return value
}

func (n *Node) downloadNeiwangtong(ctx context.Context, t domain.Transfer, f domain.File, destination string) error {
	n.mu.Lock()
	service := n.neiwangtong
	wireID := transferWireID(&n.data, t)
	n.mu.Unlock()
	if service == nil || wireID == "" {
		return errors.New("内网通互通服务未开启")
	}
	taskID, err := strconv.ParseUint(t.RemotePacket, 10, 64)
	if err != nil {
		return fmt.Errorf("无效的内网通任务编号: %s", t.RemotePacket)
	}
	fileID, err := strconv.ParseUint(t.RemoteFile, 10, 32)
	if err != nil || fileID == 0 {
		return errors.New("内网通文件编号无效")
	}

	s := n.snapshot(false)
	if destination == "" {
		destination = t.Path
	}
	if destination == "" {
		destination = filepath.Join(s.Settings.DownloadDir, f.Name)
		if _, err := os.Stat(destination); err == nil {
			if s.Settings.DuplicatePolicy == "ask" {
				return errors.New("同名文件存在，请选择另存为")
			}
			ext := filepath.Ext(f.Name)
			base := strings.TrimSuffix(f.Name, ext)
			for index := 1; ; index++ {
				candidate := filepath.Join(s.Settings.DownloadDir, fmt.Sprintf("%s (%d)%s", base, index, ext))
				if _, statErr := os.Lstat(candidate); os.IsNotExist(statErr) {
					destination = candidate
					break
				}
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(filepath.Dir(destination), ".goim-nwt-")
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	partial := filepath.Join(temp, "incoming")
	defer os.Remove(partial)
	n.setTransfer(t.ID, "transferring", 0, 0, "")
	lastProgress := time.Now()
	err = service.AcceptFile(ctx, wireID, taskID, fileID, t.Size, partial, func(done int64) {
		if done == t.Size || time.Since(lastProgress) >= 150*time.Millisecond {
			n.setTransfer(t.ID, "transferring", done, 0, "")
			lastProgress = time.Now()
		}
	})
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := installFile(partial, destination); err != nil {
		return err
	}
	return n.completeReceive(t, destination)
}
