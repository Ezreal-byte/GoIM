package core

import (
	"context"
	"crypto/sha256"
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
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("内网通设备 %s 未回复发现探测，请检查网络设置", target)
		}
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
		if b, err := os.ReadFile(s.Self.Avatar); err == nil && len(b) > 0 {
			avatarBytes = b
		}
	}

	var nodeID [16]byte
	if hash := sha256.Sum256([]byte(s.Self.ID)); len(hash) >= 16 {
		copy(nodeID[:], hash[:16])
	}

	return neiwangtong.Start(neiwangtong.Config{
		ID:               nodeID,
		Name:             name,
		Group:            s.Self.ContactGroup,
		Sign:             s.Self.Signature,
		DiscoveryPort:    port,
		ControlPort:      9012,
		TransferPort:     9013,
		BindIP:           bind,
		AvatarPNG:        avatarBytes,
		DisableBroadcast: s.Settings.DisableAutoDiscovery,
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
			Files: func(peerID string, taskID uint64, name string, size int64) {
				if live.Load() {
					n.receiveNeiwangtongFiles(peerID, taskID, name, size)
				}
			},
			FileProgress: func(taskID uint64, done, total int64, complete bool, err error) {
				if live.Load() {
					n.receiveNeiwangtongProgress(taskID, done, total, complete, err)
				}
			},
			AvatarUpdated: func(peerID string, png []byte) {
				if live.Load() {
					n.receiveNeiwangtongAvatar(peerID, png)
				}
			},
			Error: func(err error) {
				if err != nil && live.Load() {
					n.report(fmt.Errorf("内网通互通: %w", err))
				}
			},
		},
	})
}

func (n *Node) receiveNeiwangtongPeer(peer neiwangtong.Peer) {
	if peer.ID == "" || !utf8.ValidString(peer.Name) {
		return
	}
	name := strings.TrimSpace(peer.Name)
	if name == "" || len(peer.Name) > 160 {
		return
	}
	_ = n.change(func(s *domain.State) {
		i := peerIndex(s, peer.ID)
		group := peer.Group
		avatar := ""
		if i >= 0 {
			if group == "" {
				group = s.Peers[i].ContactGroup
			}
			avatar = s.Peers[i].Avatar
		}
		value := domain.Peer{
			ID:           peer.ID,
			Name:         name,
			Address:      peer.Address,
			ContactGroup: group,
			Signature:    peer.Sign,
			Avatar:       avatar,
			Online:       peer.Online,
			LastSeen:     time.Now(),
			Presence:     domain.PresenceOnline,
			Transport:    transportNWT,
			ClientType:   domain.ClientNeiwangtong,
		}
		if i < 0 {
			s.Peers = append(s.Peers, value)
		} else {
			s.Peers[i] = value
		}
	})
	if peer.Online {
		n.async(func() { n.flushPeer(peer.ID) })
	}
}

func (n *Node) receiveNeiwangtongText(peerID string, packetID uint64, text string) {
	if text == "" || len(text) > 256<<10 || !utf8.ValidString(text) {
		return
	}
	_ = n.change(func(s *domain.State) {
		peer := s.Peer(peerID)
		if peer.ID != peerID || peer.Transport != transportNWT {
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
			SenderID:       peerID,
			Kind:           "text",
			Text:           text,
			CreatedAt:      time.Now(),
			Status:         "delivered",
		})
	})
}

func (n *Node) receiveNeiwangtongFiles(peerID string, taskID uint64, name string, size int64) {
	if size < 0 || strings.TrimSpace(name) == "" {
		return
	}
	_ = n.change(func(s *domain.State) {
		peer := s.Peer(peerID)
		if peer.ID != peerID || peer.Transport != transportNWT {
			return
		}
		conversationID := ensureDirect(s, peer)
		taskHex := fmt.Sprint(taskID)
		fileID := legacyStableID("nwt-file", peerID, taskHex, name)
		transferID := legacyStableID("nwt-transfer", peerID, taskHex, name)
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
			SenderID:       peerID,
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
			PeerID:          peerID,
			Direction:       "receive",
			Status:          "offered",
			Name:            file.Name,
			Size:            file.Size,
			UpdatedAt:       receivedAt,
			LegacyTransport: transportNWT,
			RemotePacket:    taskHex,
			RemoteFile:      taskHex,
		})
		addMessageState(s, domain.Message{
			ID:             fileID,
			ConversationID: conversationID,
			SenderID:       peerID,
			Kind:           "file",
			FileID:         fileID,
			Text:           name,
			CreatedAt:      file.CreatedAt,
			Status:         "delivered",
		})
	})
}

func (n *Node) receiveNeiwangtongProgress(taskID uint64, done, total int64, complete bool, failure error) {
	taskHex := fmt.Sprint(taskID)
	status := "transferring"
	if complete {
		status = "completed"
	}
	problem := ""
	if failure != nil {
		status, problem = "failed", failure.Error()
	}
	_ = n.change(func(s *domain.State) {
		for i := range s.Transfers {
			t := &s.Transfers[i]
			if t.LegacyTransport != transportNWT || t.RemotePacket != taskHex {
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

func (n *Node) receiveNeiwangtongAvatar(peerID string, png []byte) {
	if len(png) == 0 {
		return
	}
	avatarDir := filepath.Join(n.dir, "avatars")
	_ = os.MkdirAll(avatarDir, 0755)
	avatarPath := filepath.Join(avatarDir, "nwt-"+peerID+".png")
	if err := os.WriteFile(avatarPath, png, 0644); err != nil {
		return
	}
	_ = n.change(func(s *domain.State) {
		i := peerIndex(s, peerID)
		if i >= 0 {
			s.Peers[i].Avatar = avatarPath
		}
	})
}

func (n *Node) neiwangtongPending(peer string, value pending) error {
	n.mu.Lock()
	service := n.neiwangtong
	n.mu.Unlock()
	if service == nil {
		return errors.New("内网通互通服务未开启")
	}
	e := value.Envelope
	switch e.Type {
	case "message":
		return service.SendText(peer, e.Message.Text)
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
		taskID := uint64(time.Now().UnixNano())
		_ = n.change(func(st *domain.State) {
			if idx := transferIndex(st, t.ID); idx >= 0 {
				st.Transfers[idx].RemotePacket = fmt.Sprint(taskID)
				st.Transfers[idx].RemoteFile = fmt.Sprint(taskID)
				st.Transfers[idx].Status = "offered"
				st.Transfers[idx].LegacyTransport = transportNWT
			}
		})
		return service.SendFileOffer(peer, taskID, f.Name, f.Size, time.Now().Unix(), f.SourcePath)
	default:
		return nil
	}
}

func (n *Node) downloadNeiwangtong(ctx context.Context, t domain.Transfer, f domain.File, destination string) error {
	n.mu.Lock()
	service := n.neiwangtong
	n.mu.Unlock()
	if service == nil {
		return errors.New("内网通互通服务未开启")
	}
	taskID, err := strconv.ParseUint(t.RemotePacket, 10, 64)
	if err != nil {
		return fmt.Errorf("无效的内网通任务编号: %s", t.RemotePacket)
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

	n.setTransfer(t.ID, "transferring", 0, 0, "")
	err = service.AcceptFile(ctx, t.PeerID, taskID, t.Size, destination)
	if err != nil {
		n.setTransfer(t.ID, "failed", 0, 0, err.Error())
		return err
	}
	return n.completeReceive(t, destination)
}
