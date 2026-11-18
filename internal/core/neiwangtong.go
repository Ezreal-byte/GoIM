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

// The stock client reliably sends files larger than 4 GiB to us. Its
// receiver has yet to complete the corresponding large-file fixture.
const neiwangtongSendLimit = 1 << 32

var errNeedsGoIMConnection = errors.New("已发现 GoIM 设备，但尚未建立 GoIM 连接")

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
	peer, err := service.ProbePeer(n.ctx, target, 9012, 9013)
	if err != nil {
		return fmt.Errorf("连接内网通设备 %s 失败: %w", target, err)
	}
	n.mu.Lock()
	knownGoIM := goIMPeerForNativeID(&n.data.State, peer.ID) != ""
	n.mu.Unlock()
	if peer.GoIM || knownGoIM {
		return errNeedsGoIMConnection
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
		EnableFolders:    true,
		BindIP:           bind,
		AvatarPNG:        avatarBytes,
		DisableBroadcast: true,
		AllowRemote: func(remote netip.AddrPort) bool {
			return n.allowedRemote(net.UDPAddrFromAddrPort(remote))
		},
		CanServeGroupFile: func(peer, group string) bool {
			return live.Load() && n.canServeNativeGroupFile(peer, group)
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
			Message: func(peerID string, message neiwangtong.DirectMessage) error {
				if live.Load() {
					return n.receiveNeiwangtongMessage(peerID, message)
				}
				return neiwangtong.ErrNotConnected
			},
			GroupRoster: func(peerID string, roster neiwangtong.GroupRoster) {
				if live.Load() {
					n.receiveNeiwangtongGroupRoster(peerID, roster)
				}
			},
			GroupDeparture: func(peerID, groupID string, kind uint32) {
				if live.Load() {
					n.receiveNeiwangtongGroupDeparture(peerID, groupID, kind)
				}
			},
			GroupText: func(peerID string, message neiwangtong.GroupMessage) error {
				if live.Load() {
					return n.receiveNeiwangtongGroupText(peerID, message)
				}
				return neiwangtong.ErrNotConnected
			},
			Files: func(peerID string, offer neiwangtong.XMLSendFile) {
				if live.Load() {
					n.receiveNeiwangtongFile(peerID, offer)
				}
			},
			FileCancelled: func(peerID string, taskID uint64) {
				if live.Load() {
					n.receiveNeiwangtongCancel(peerID, taskID)
				}
			},
			FileProgress: func(peerID string, taskID uint64, done, total int64, complete bool, err error) {
				if live.Load() {
					n.receiveNeiwangtongProgress(peerID, taskID, done, total, complete, err)
				}
			},
			Folders: func(peerID string, offer neiwangtong.FolderOffer) {
				if live.Load() {
					n.receiveNeiwangtongDirectory(peerID, offer)
				}
			},
			FolderCancelled: func(peerID, taskID string) {
				if live.Load() {
					n.receiveNeiwangtongTaskCancel(peerID, taskID)
				}
			},
			FolderProgress: func(peerID, taskID string, done, total int64, complete bool, err error) {
				if live.Load() {
					n.receiveNeiwangtongTaskProgress(peerID, taskID, done, total, complete, err)
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

func (n *Node) receiveNeiwangtongFile(peerID string, offer neiwangtong.XMLSendFile) {
	if offer.Size < 0 || strings.TrimSpace(offer.Name) == "" {
		return
	}
	_ = n.change(func(s *domain.State) {
		peer := n.compatibilitySenderLocked(transportNWT, peerID)
		if peer.ID == "" {
			return
		}
		conversationID := ensureDirect(s, peer)
		taskHex, fileNumber := fmt.Sprint(offer.TaskID), fmt.Sprint(offer.FileID)
		identity := []string{peerID, taskHex, fileNumber, offer.Name, fmt.Sprint(offer.Size), fmt.Sprint(offer.LastModify)}
		fileID := legacyStableID(append([]string{"nwt-file"}, identity...)...)
		transferID := legacyStableID(append([]string{"nwt-transfer"}, identity...)...)
		if fileIndex(s, fileID) >= 0 || transferIndex(s, transferID) >= 0 {
			return
		}
		receivedAt := time.Now()
		mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(offer.Name)))
		file := domain.File{
			ID:             fileID,
			Name:           offer.Name,
			Size:           offer.Size,
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
			Text:           file.Name,
			CreatedAt:      file.CreatedAt,
			Status:         "delivered",
		})
	})
}

func (n *Node) receiveNeiwangtongCancel(peerID string, taskID uint64) {
	n.receiveNeiwangtongTaskCancel(peerID, strconv.FormatUint(taskID, 10))
}

func (n *Node) receiveNeiwangtongTaskCancel(peerID, remoteTask string) {
	var partials []string
	err := n.durableFromReader(func(p *persisted) []pending {
		s := &p.State
		for i := range s.Transfers {
			t := &s.Transfers[i]
			if t.LegacyTransport != transportNWT || t.RemotePeerID != peerID ||
				t.RemotePacket != remoteTask || t.Direction != "receive" || terminalTransfer(t.Status) || receiveAlreadyPublished(p, t.ID) {
				continue
			}
			if stop := n.transfers[t.ID]; stop != nil {
				stop()
			}
			t.Status, t.Error = "cancelled", neiwangtong.ErrFileCancelled.Error()
			t.Speed = 0
			t.UpdatedAt = time.Now()
			partials = append(partials, t.ID)
		}
		return nil
	})
	if err != nil {
		return
	}
	for _, id := range partials {
		_ = os.Remove(n.nativePartialPath(id))
	}
}

func (n *Node) receiveNeiwangtongProgress(peerID string, taskID uint64, done, total int64, complete bool, failure error) {
	n.receiveNeiwangtongTaskProgress(peerID, strconv.FormatUint(taskID, 10), done, total, complete, failure)
}

func (n *Node) receiveNeiwangtongTaskProgress(peerID, taskHex string, done, total int64, complete bool, failure error) {
	status := "transferring"
	if complete {
		status = "completed"
	}
	problem := ""
	if failure != nil {
		status, problem = "failed", failure.Error()
		switch {
		case errors.Is(failure, neiwangtong.ErrFileRejected):
			status = "rejected"
		case errors.Is(failure, neiwangtong.ErrFileCancelled):
			status = "cancelled"
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
			t.Done = max(t.Done, done)
			if terminalTransfer(status) {
				t.Speed = 0
			}
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
	case nativeGroupExitDelivery:
		return service.SendGroupExit(peer, e.Target)
	case nativeGroupDismissDelivery:
		return service.SendGroupDismiss(peer, e.Target)
	case nativeGroupKickDelivery:
		return service.SendGroupKick(peer, e.Target)
	case "message_recall":
		if groupID, ok := strings.CutPrefix(e.Message.ConversationID, "group:"); ok {
			room, err := n.activeNativeGroup(groupID, value.PeerID)
			if err != nil {
				return err
			}
			return service.SendGroupTextRecall(peer, room, nativeMessagePacket(value), e.ID, e.Message.ID)
		}
		return service.SendTextRecall(n.ctx, peer, nativeMessagePacket(value), e.ID, e.Message.ID)
	case nativeGroupRosterDelivery:
		roster, err := n.nativeRosterForDelivery(e.Target, value.PeerID)
		if err != nil {
			return err
		}
		return service.SendGroupRoster(peer, roster)
	case "message":
		if groupID, ok := strings.CutPrefix(e.Message.ConversationID, "group:"); ok {
			room, err := n.activeNativeGroup(groupID, value.PeerID)
			if err != nil {
				return err
			}
			return service.SendGroupText(peer, room, nativeMessagePacket(value), e.Message.ID, e.Message.Text)
		}
		var queuedAt time.Time
		if value.QueuedOffline {
			queuedAt = e.Message.CreatedAt
		}
		return service.SendTextConfirmed(n.ctx, peer, nativeMessagePacket(value), e.Message.ID, e.Message.Text, queuedAt)
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
		if f.Folder {
			return n.neiwangtongDirectoryPending(service, peer, value, t, f)
		}
		if f.Size >= neiwangtongSendLimit {
			n.setTransfer(t.ID, "failed", t.Done, 0, "发给内网通的单个文件需小于 4 GiB，请拆分后重发")
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
		if f.InlineImage {
			var room string
			if groupID, ok := strings.CutPrefix(t.ConversationID, "group:"); ok {
				var err error
				room, err = n.activeNativeGroup(groupID, value.PeerID)
				if err != nil {
					return err
				}
			}
			return n.sendNeiwangtongImage(service, peer, room, value, taskID, fileID, f)
		}
		if groupID, ok := strings.CutPrefix(t.ConversationID, "group:"); ok {
			room, err := n.activeNativeGroup(groupID, value.PeerID)
			if err != nil {
				return err
			}
			ref := neiwangtong.GroupFileRef{ID: legacyStableID("nwt-group-file", f.ID), Name: f.Name, Size: f.Size}
			return service.SendGroupFile(peer, room, nativeMessagePacket(value), f.ID, ref, taskID, fileID, f.SourcePath)
		}
		info, err := os.Stat(f.SourcePath)
		if err != nil {
			return err
		}
		return service.SendFileOfferWithIDs(peer, taskID, fileID, f.Name, f.Size, info.ModTime().Unix(), f.SourcePath)
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
		if t.LegacyTransport == transportNWT && t.RemoteShareID != "" && s.File(t.FileID).Folder {
			if t.Direction == "send" {
				return service.CancelGroupFolder(peer, t.RemoteShareID)
			}
			return nil // the local receive context closes the folder channel
		}
		if t.LegacyTransport == transportNWT && t.RemotePacket != "" && s.File(t.FileID).Folder {
			if t.Direction == "send" {
				return service.CancelDirectorySend(peer, t.RemotePacket)
			}
			return service.StopDirectoryReceive(peer, t.RemotePacket)
		}
		task, err := strconv.ParseUint(t.RemotePacket, 10, 64)
		if err != nil || t.LegacyTransport != transportNWT {
			return nil
		}
		if t.Direction == "send" {
			return service.CancelSend(peer, task)
		}
		return service.StopReceiving(peer, task)
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
	if terminalTransfer(t.Status) {
		return context.Canceled
	}
	n.mu.Lock()
	service := n.neiwangtong
	wireID := transferWireID(&n.data, t)
	n.mu.Unlock()
	if service == nil || wireID == "" {
		return errors.New("内网通互通服务未开启")
	}
	if t.RemoteImageMD5 != "" {
		return n.downloadNeiwangtongImage(ctx, service, wireID, t, f, destination)
	}
	if f.Folder {
		return n.downloadNeiwangtongDirectory(ctx, service, wireID, t, f, destination)
	}
	isGroupFile := t.RemoteShareID != ""
	var taskID, fileID uint64
	var err error
	if isGroupFile {
		groupID, ok := strings.CutPrefix(t.ConversationID, "group:")
		if !ok {
			return errors.New("内网通群文件引用无效")
		}
		if _, err := n.activeNativeGroup(groupID, t.PeerID); err != nil {
			return err
		}
	} else {
		taskID, err = strconv.ParseUint(t.RemotePacket, 10, 64)
		if err != nil {
			return fmt.Errorf("无效的内网通任务编号: %s", t.RemotePacket)
		}
		fileID, err = strconv.ParseUint(t.RemoteFile, 10, 32)
		if err != nil || fileID == 0 {
			return errors.New("内网通文件编号无效")
		}
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
	partial := n.nativePartialPath(t.ID)
	if err := os.MkdirAll(filepath.Dir(partial), 0700); err != nil {
		return err
	}
	// Explicit cancellation discards the prefix, while a time-out, a lost
	// connection, and application shutdown keep it for the next attempt.
	defer func() {
		n.mu.Lock()
		i := transferIndex(&n.data.State, t.ID)
		discard := i < 0 || terminalTransfer(n.data.State.Transfers[i].Status)
		n.mu.Unlock()
		if discard {
			_ = os.Remove(partial)
		}
	}()
	var have int64
	if info, err := os.Lstat(partial); err == nil && info.Mode().IsRegular() {
		have = info.Size()
	}
	ready := false
	if err := n.change(func(s *domain.State) {
		if i := transferIndex(s, t.ID); i >= 0 && !terminalTransfer(s.Transfers[i].Status) {
			task := &s.Transfers[i]
			task.Status, task.Path, task.Done, task.Error = "transferring", destination, have, ""
			task.UpdatedAt = time.Now()
			ready = true
		}
	}); err != nil {
		return err
	}
	if !ready || ctx.Err() != nil {
		return context.Canceled
	}
	if have == 0 || have != t.Size {
		var lastProgress time.Time
		progress := func(done int64) {
			if done == t.Size || time.Since(lastProgress) >= 150*time.Millisecond {
				n.setTransfer(t.ID, "transferring", done, 0, "")
				lastProgress = time.Now()
			}
		}
		if isGroupFile {
			err = n.resumeNeiwangtongGroupFile(ctx, service, wireID, t, partial, have, progress)
		} else {
			err = service.ResumeFile(ctx, wireID, taskID, fileID, t.Size, partial, progress)
		}
		if err != nil {
			return err
		}
	}
	return n.publishReceivedFile(ctx, t.ID, partial, destination, t.Size, "")
}

func (n *Node) resumeNeiwangtongGroupFile(ctx context.Context, service *neiwangtong.Service, peer string,
	task domain.Transfer, partial string, have int64, progress func(int64)) error {
	download, err := service.RequestGroupFile(ctx, peer, task.RemoteShareID, task.Size)
	if err != nil {
		return err
	}
	// Reuse the prefix only if the same invitation still names the same
	// size and revision. The data channel can assign a different selector.
	if have > 0 && (have > task.Size || task.RemoteFileModTime <= 0 || task.RemoteFileModTime != download.ModTime) {
		if err := os.Remove(partial); err != nil {
			return err
		}
		have = 0
	}
	ready := false
	if err := n.change(func(s *domain.State) {
		i := transferIndex(s, task.ID)
		if i >= 0 && !terminalTransfer(s.Transfers[i].Status) && ctx.Err() == nil {
			s.Transfers[i].RemoteFileModTime = download.ModTime
			s.Transfers[i].Done = have
			ready = true
		}
	}); err != nil {
		return err
	}
	if !ready {
		return context.Canceled
	}
	return download.Resume(ctx, partial, func(done, _ int64) { progress(done) })
}

func (n *Node) nativePartialPath(transferID string) string {
	return filepath.Join(n.dir, "nwt-partials", transferID+".part")
}
