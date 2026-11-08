package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goim/internal/domain"
	ipmsgproto "goim/internal/ipmsg"
)

const legacyIPMSG = "ipmsg"

type legacyRemote struct {
	packet uint64
	file   uint32
}

func (n *Node) startIPMSG(version string) {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "goim-host"
	}
	s := n.snapshot(false)
	name := strings.TrimSpace(s.Settings.Nickname)
	if name == "" {
		name = "GoIM"
	}
	service, err := ipmsgproto.Start(ipmsgproto.Config{
		Host: host, Name: name, Version: version,
		Hidden: s.Settings.Presence == domain.PresenceInvisible,
		Away:   s.Settings.Presence == domain.PresenceAway || s.Settings.Presence == domain.PresenceBusy,
		Callbacks: ipmsgproto.Callbacks{
			Peer:         n.receiveLegacyPeer,
			Text:         n.receiveLegacyText,
			Files:        n.receiveLegacyFiles,
			FileProgress: n.receiveLegacyProgress,
			AllowFile: func(transferID string) bool {
				n.mu.Lock()
				defer n.mu.Unlock()
				index := transferIndex(&n.data.State, transferID)
				if n.closing || index < 0 {
					return false
				}
				transfer := n.data.State.Transfers[index]
				return transfer.Direction == "send" && !transfer.Revoked && transfer.Status != "cancelled" && transfer.Status != "rejected"
			},
			Error: func(err error) {
				if err != nil {
					n.report(fmt.Errorf("内网通兼容模式: %w", err))
				}
			},
		},
	})
	if err != nil {
		n.report(fmt.Errorf("内网通兼容模式未启动: %w", err))
		return
	}
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		service.Close()
		return
	}
	n.legacy = service
	n.mu.Unlock()
}

func (n *Node) receiveLegacyPeer(peer ipmsgproto.Peer) {
	if peer.ID == "" || !utf8.ValidString(peer.Name) {
		return
	}
	name := strings.TrimSpace(peer.Name)
	if name == "" || len(peer.Name) > 160 {
		return
	}
	presence := domain.PresenceOnline
	if peer.Away {
		presence = domain.PresenceAway
	}
	_ = n.change(func(s *domain.State) {
		i := peerIndex(s, peer.ID)
		group := ""
		if i >= 0 {
			group = s.Peers[i].ContactGroup
		}
		value := domain.Peer{
			ID: peer.ID, Name: name, Address: peer.Address, ContactGroup: group,
			Online: peer.Online, LastSeen: time.Now(), Presence: presence,
			Transport: legacyIPMSG,
		}
		if i < 0 {
			s.Peers = append(s.Peers, value)
		} else {
			s.Peers[i] = value
		}
		if peer.Online {
			ensureDirect(s, value)
		}
	})
	if peer.Online {
		n.async(func() { n.flushPeer(peer.ID) })
	}
}

func (n *Node) receiveLegacyText(peerID string, packetNumber uint64, text string) {
	if text == "" || len(text) > 256<<10 || !utf8.ValidString(text) {
		return
	}
	_ = n.change(func(s *domain.State) {
		peer := s.Peer(peerID)
		if peer.ID != peerID || peer.Transport != legacyIPMSG {
			return
		}
		conversationID := ensureDirect(s, peer)
		messageID := legacyStableID("message", peerID, fmt.Sprint(packetNumber), text)
		for _, existing := range s.Messages {
			if existing.ID == messageID {
				return
			}
		}
		addMessageState(s, domain.Message{
			ID: messageID, ConversationID: conversationID, SenderID: peerID,
			Kind: "text", Text: text, CreatedAt: time.Now(), Status: "delivered",
		})
	})
}

func (n *Node) receiveLegacyFiles(peerID string, packetNumber uint64, message string, files []ipmsgproto.Attachment) {
	if len(files) == 0 || len(files) > 64 {
		return
	}
	_ = n.change(func(s *domain.State) {
		peer := s.Peer(peerID)
		if peer.ID != peerID || peer.Transport != legacyIPMSG {
			return
		}
		conversationID := ensureDirect(s, peer)
		packetHex := fmt.Sprintf("%x", packetNumber)
		for _, attachment := range files {
			if attachment.Size < 0 || strings.TrimSpace(attachment.Name) == "" {
				continue
			}
			fileID := legacyStableID("file", peerID, packetHex, fmt.Sprint(attachment.ID))
			transferID := legacyStableID("transfer", peerID, packetHex, fmt.Sprint(attachment.ID))
			if fileIndex(s, fileID) >= 0 || transferIndex(s, transferID) >= 0 {
				continue
			}
			mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(attachment.Name)))
			file := domain.File{
				ID: fileID, Name: attachment.Name, Size: attachment.Size,
				Hash: strings.Repeat("0", 64), MIME: mediaType, SenderID: peerID,
				ConversationID: conversationID, CreatedAt: time.Unix(attachment.ModTime, 0),
			}
			if file.CreatedAt.IsZero() || file.CreatedAt.Unix() <= 0 {
				file.CreatedAt = time.Now()
			}
			if !validFile(file) {
				continue
			}
			// IPMSG attachments are unverified files; a filename alone cannot
			// opt them into the native chat-image download path.
			status, kind := "offered", "file"
			s.Files = append(s.Files, file)
			s.Transfers = append(s.Transfers, domain.Transfer{
				ID: transferID, FileID: fileID, ConversationID: conversationID,
				PeerID: peerID, Direction: "receive", Status: status,
				Name: file.Name, Size: file.Size, UpdatedAt: time.Now(),
				LegacyTransport: legacyIPMSG, RemotePacket: packetHex,
				RemoteFile: fmt.Sprintf("%x", attachment.ID),
			})
			text := message
			if strings.TrimSpace(text) == "" {
				text = file.Name
			}
			addMessageState(s, domain.Message{
				ID: fileID, ConversationID: conversationID, SenderID: peerID,
				Kind: kind, FileID: fileID, Text: text,
				CreatedAt: file.CreatedAt, Status: "delivered",
			})
		}
	})
}

func (n *Node) receiveLegacyProgress(packetNumber uint64, fileID uint32, done, total int64, complete bool) {
	packetHex, fileHex := fmt.Sprintf("%x", packetNumber), fmt.Sprintf("%x", fileID)
	status := "transferring"
	if complete {
		status = "completed"
	}
	_ = n.change(func(s *domain.State) {
		for i := range s.Transfers {
			t := &s.Transfers[i]
			if t.LegacyTransport != legacyIPMSG || t.RemotePacket != packetHex || t.RemoteFile != fileHex || t.Direction != "send" {
				continue
			}
			if terminalTransfer(t.Status) {
				return
			}
			t.Status = status
			t.Done = done
			t.Size = total
			t.Speed = 0
			t.Error = ""
			t.UpdatedAt = time.Now()
			return
		}
	})
}

func legacyStableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func (n *Node) legacyPending(peer string, value pending) (legacyRemote, bool, error) {
	n.mu.Lock()
	service := n.legacy
	n.mu.Unlock()
	if service == nil {
		return legacyRemote{}, false, errors.New("内网通兼容服务尚未就绪")
	}
	e := value.Envelope
	switch e.Type {
	case "message":
		return legacyRemote{}, true, service.SendText(peer, e.Message.Text)
	case "file_offer", "file_retry":
		s := n.snapshot(false)
		index := transferIndex(&s, e.TransferID)
		if index < 0 {
			return legacyRemote{}, false, errors.New("找不到兼容文件任务")
		}
		t := s.Transfers[index]
		f := s.File(t.FileID)
		if f.SourcePath == "" {
			return legacyRemote{}, false, errors.New("源文件不可用")
		}
		packet, fileID, err := service.OfferFile(peer, f.SourcePath, t.ID, "")
		return legacyRemote{packet: packet, file: fileID}, true, err
	default:
		// Protocol-specific control state has no IPMSG equivalent. Removing it
		// from the outbox keeps legacy peers from blocking later text/files.
		return legacyRemote{}, false, nil
	}
}

func (n *Node) legacyCompleted(peer string, done []pending, remote map[string]legacyRemote) error {
	return n.durable(func(data *persisted) []pending {
		for _, value := range done {
			delete(data.Outbox, value.Envelope.ID)
			e := value.Envelope
			if e.Type != "message" {
				if r, ok := remote[value.Envelope.ID]; ok {
					if i := transferIndex(&data.State, e.TransferID); i >= 0 && !terminalTransfer(data.State.Transfers[i].Status) {
						data.State.Transfers[i].RemotePacket = fmt.Sprintf("%x", r.packet)
						data.State.Transfers[i].RemoteFile = fmt.Sprintf("%x", r.file)
						data.State.Transfers[i].LegacyTransport = legacyIPMSG
						data.State.Transfers[i].Status = "offered"
						data.State.Transfers[i].Error = ""
						data.State.Transfers[i].UpdatedAt = time.Now()
					}
				}
				continue
			}
			for i := range data.State.Messages {
				if data.State.Messages[i].ID == e.Message.ID {
					data.State.Messages[i].Status = "sent"
				}
			}
		}
		return nil
	})
}

func (n *Node) downloadLegacy(ctx context.Context, transfer domain.Transfer, file domain.File, destination string) error {
	n.mu.Lock()
	service := n.legacy
	n.mu.Unlock()
	if service == nil {
		return errors.New("内网通兼容服务尚未就绪")
	}
	packet, err := strconv.ParseUint(transfer.RemotePacket, 16, 64)
	if err != nil {
		return errors.New("兼容文件邀请标识无效")
	}
	remoteFile, err := parseHexUint32(transfer.RemoteFile)
	if err != nil {
		return errors.New("兼容文件标识无效")
	}
	s := n.snapshot(false)
	if destination == "" {
		destination = transfer.Path
	}
	if destination == "" && file.InlineImage {
		destination = filepath.Join(n.dir, "chat-images", file.ID+imageSuffix(file.MIME))
	}
	if destination == "" {
		destination = filepath.Join(s.Settings.DownloadDir, file.Name)
		if _, err := os.Stat(destination); err == nil {
			if s.Settings.DuplicatePolicy == "ask" {
				return errors.New("同名文件存在，请选择另存为")
			}
			ext := filepath.Ext(file.Name)
			base := strings.TrimSuffix(file.Name, ext)
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
	partialDir := filepath.Join(n.dir, "partials")
	if err := os.MkdirAll(partialDir, 0700); err != nil {
		return err
	}
	partial := filepath.Join(partialDir, transfer.ID+".part")
	output, err := os.OpenFile(partial, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
	}()
	info, err := output.Stat()
	if err != nil {
		return err
	}
	offset := info.Size()
	if offset > file.Size {
		if err := output.Truncate(0); err != nil {
			return err
		}
		offset = 0
	}
	if _, err := output.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	n.change(func(s *domain.State) {
		if i := transferIndex(s, transfer.ID); i >= 0 {
			s.Transfers[i].Path = destination
			s.Transfers[i].Done = offset
			s.Transfers[i].Status = "transferring"
			s.Transfers[i].Error = ""
		}
	})
	started := time.Now()
	_, err = service.DownloadFile(ctx, transfer.PeerID, packet, remoteFile, file.Size, offset, output, func(done int64) {
		speed := float64(done-offset) / time.Since(started).Seconds()
		n.setTransfer(transfer.ID, "transferring", done, speed, "")
	})
	if err != nil {
		return err
	}
	if err := output.Truncate(file.Size); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	closed = true
	n.setTransfer(transfer.ID, "verifying", file.Size, 0, "")
	info, err = os.Stat(partial)
	if err != nil || !info.Mode().IsRegular() || info.Size() != file.Size {
		return errors.New("兼容文件接收不完整，请重试")
	}
	if file.InlineImage && inlineImageType(partial, file.Size) != file.MIME {
		_ = os.Truncate(partial, 0)
		return errors.New("对方发送的内容不是受支持的聊天图片")
	}
	hash, err := fileHashContext(ctx, partial)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := installFile(partial, destination); err != nil {
		return err
	}
	if err := n.change(func(s *domain.State) {
		if i := fileIndex(s, file.ID); i >= 0 {
			s.Files[i].Hash = hash
		}
	}); err != nil {
		return err
	}
	transfer.Path = destination
	return n.completeReceive(transfer, destination)
}

func parseHexUint32(value string) (uint32, error) {
	if value == "" || len(value) > 8 {
		return 0, errors.New("invalid uint32")
	}
	n, err := parseHex(value)
	if err != nil || n > uint64(^uint32(0)) {
		return 0, errors.New("invalid uint32")
	}
	return uint32(n), nil
}

func parseHex(value string) (uint64, error) {
	var out uint64
	for _, r := range value {
		var digit uint64
		switch {
		case r >= '0' && r <= '9':
			digit = uint64(r - '0')
		case r >= 'a' && r <= 'f':
			digit = uint64(r-'a') + 10
		case r >= 'A' && r <= 'F':
			digit = uint64(r-'A') + 10
		default:
			return 0, errors.New("invalid hex")
		}
		out = out*16 + digit
	}
	return out, nil
}
