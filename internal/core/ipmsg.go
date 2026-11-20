package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
	ipmsgproto "goim/internal/ipmsg"
)

const legacyIPMSG = domain.TransportIPMSG

func (n *Node) connectLegacy(address string) error {
	target, err := netip.ParseAddrPort(address)
	if err != nil || !target.Addr().Unmap().Is4() {
		return errors.New("旧版兼容连接需要 IPv4 地址，例如 192.168.1.20:2425")
	}
	n.mu.Lock()
	service := n.legacy
	n.mu.Unlock()
	if service == nil {
		return errors.New("飞鸽／飞秋互通未运行，请检查网络设置")
	}
	peer, err := service.Probe(n.ctx, target)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("旧版设备 %s 未回复 UDP 探测，请检查客户端互通设置和防火墙", target)
		}
		return fmt.Errorf("连接旧版设备 %s 失败: %w", target, err)
	}
	if peer.Neiwangtong {
		return errors.New("已发现内网通设备，但尚未建立内网通连接")
	}
	return nil
}

func (n *Node) newIPMSG(port int, live *atomic.Bool) (*ipmsgproto.Service, error) {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "goim-host"
	}
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
	return ipmsgproto.Start(ipmsgproto.Config{
		Host: host, User: "GoIM-" + s.Self.ID, Name: name, Version: n.version, Port: port, RequireFiles: true,
		BindIP:           bind,
		DisableBroadcast: true,
		Hidden:           s.Settings.Presence == domain.PresenceInvisible,
		Away:             s.Settings.Presence == domain.PresenceAway || s.Settings.Presence == domain.PresenceBusy,
		Callbacks: ipmsgproto.Callbacks{
			Peer: func(p ipmsgproto.Peer) {
				if live.Load() {
					n.receiveLegacyPeer(p)
				}
			},
			Message: func(id string, packet uint64, text string, files []ipmsgproto.Attachment) error {
				if !live.Load() {
					return context.Canceled
				}
				return n.receiveLegacyMessage(id, packet, text, files)
			},
			FileRequested: func(id string) {
				if live.Load() {
					n.confirmLegacyFileRequest(id)
				}
			},
			FileProgress: func(packet uint64, file uint32, done, total int64, complete bool, err error) {
				if live.Load() {
					n.receiveLegacyProgress(packet, file, done, total, complete, err)
				}
			},
			AllowFile: func(transferID string) bool {
				n.mu.Lock()
				defer n.mu.Unlock()
				index := transferIndex(&n.data.State, transferID)
				if n.closing || !live.Load() || index < 0 {
					return false
				}
				transfer := n.data.State.Transfers[index]
				return transfer.Direction == "send" && !transfer.Revoked && transfer.Status != "cancelled" && transfer.Status != "rejected"
			},
			Error: func(err error) {
				if err != nil && live.Load() {
					n.report(fmt.Errorf("飞鸽／飞秋互通: %w", err))
				}
			},
		},
	})
}

func (n *Node) receiveLegacyPeer(peer ipmsgproto.Peer) {
	name := strings.TrimSpace(peer.Name)
	if peer.ID == "" || name == "" || len(peer.Name) > 160 || !utf8.ValidString(peer.Name) {
		return
	}
	presence := domain.PresenceOnline
	if peer.Away {
		presence = domain.PresenceAway
	}
	client := domain.ClientIPMSG
	if peer.Neiwangtong {
		client = domain.ClientNeiwangtong
	}
	n.observeCompatibilityPeer(domain.Peer{
		ID: peer.ID, Name: name, Address: peer.Address, Online: peer.Online,
		Presence: presence, Transport: legacyIPMSG, ClientType: client,
	})
}

var errUnboundIPMSGPeer = errors.New("旧飞鸽／飞秋会话的登录身份未确认，请选择当前联系人重新发送")

func unboundIPMSGTransfer(t domain.Transfer) bool {
	if t.LegacyTransport != legacyIPMSG {
		return false
	}
	id := t.RemotePeerID
	if id == "" {
		id = t.PeerID
	}
	return domain.UnboundIPMSGPeerID(id)
}

// The old host-and-address IDs do not identify a logon. Keep their history and
// end any pending sends that still depend on the old IPMSG route.
func retireUnboundIPMSGDeliveries(p *persisted) {
	s := &p.State
	for id, d := range p.Outbox {
		transport, wire := d.WireTransport, d.WirePeerID
		if transport == "" {
			transport = s.Peer(d.PeerID).Transport
			if index := transferIndex(s, d.Envelope.TransferID); index >= 0 && unboundIPMSGTransfer(s.Transfers[index]) {
				transport, wire = legacyIPMSG, s.Transfers[index].RemotePeerID
			}
		}
		if wire == "" {
			wire = d.PeerID
		}
		if transport != legacyIPMSG || !domain.UnboundIPMSGPeerID(wire) {
			continue
		}
		delete(p.Outbox, id)
		if index := messageIndex(s, d.Envelope.Message.ConversationID, d.Envelope.Message.ID); index >= 0 {
			s.Messages[index].Status = "failed"
		}
	}
	for i := range s.Transfers {
		t := &s.Transfers[i]
		if t.Direction == "send" && unboundIPMSGTransfer(*t) && !terminalTransfer(t.Status) {
			t.Status, t.Error, t.Speed, t.Revoked = "failed", errUnboundIPMSGPeer.Error(), 0, true
		}
	}
}

func (n *Node) receiveLegacyMessage(peerID string, packetNumber uint64, message string, files []ipmsgproto.Attachment) error {
	if len(message) > 256<<10 || !utf8.ValidString(message) || len(files) > 64 || message == "" && len(files) == 0 {
		return errors.New("无效的飞鸽／飞秋消息")
	}
	for _, file := range files {
		if file.Size < 0 || !validFileName(file.Name) {
			return errors.New("无效的飞鸽／飞秋附件")
		}
	}
	var downloads []string
	admitted := false
	err := n.durableFromReader(func(p *persisted) []pending {
		s := &p.State
		peer := n.compatibilitySenderLocked(legacyIPMSG, peerID)
		if peer.ID == "" {
			return nil
		}
		admitted = true
		conversationID := ensureDirect(s, peer)
		packetHex := fmt.Sprintf("%x", packetNumber)
		receivedAt := time.Now()
		// A single-file offer often repeats only the filename as its text body.
		filenameOnly := len(files) == 1 && strings.TrimSpace(message) == files[0].Name
		if message != "" && !filenameOnly {
			messageID := legacyStableID("message", peerID, fmt.Sprint(packetNumber), message)
			if messageIndex(s, conversationID, messageID) < 0 {
				addMessageState(s, domain.Message{ID: messageID, ConversationID: conversationID, SenderID: peer.ID,
					Kind: "text", Text: message, CreatedAt: receivedAt, Status: "delivered"})
			}
		}
		for _, attachment := range files {
			fileID := legacyStableID("file", peerID, packetHex, fmt.Sprint(attachment.ID))
			transferID := legacyStableID("transfer", peerID, packetHex, fmt.Sprint(attachment.ID))
			if fileIndex(s, fileID) >= 0 || transferIndex(s, transferID) >= 0 {
				continue
			}
			mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(attachment.Name)))
			file := domain.File{
				ID: fileID, Name: attachment.Name, Size: attachment.Size,
				Hash: strings.Repeat("0", 64), MIME: mediaType, SenderID: peer.ID,
				// The source modification time is unrelated to chat order.
				ConversationID: conversationID, CreatedAt: receivedAt,
			}
			// IPMSG attachments are unverified files; a filename alone cannot
			// opt them into the native chat-image download path.
			status, kind := "offered", "file"
			if file.AutoReceiveVideo(s.Settings) {
				status = "waiting"
				downloads = append(downloads, transferID)
			}
			s.Files = append(s.Files, file)
			s.Transfers = append(s.Transfers, domain.Transfer{
				ID: transferID, FileID: fileID, ConversationID: conversationID,
				PeerID: peer.ID, Direction: "receive", Status: status,
				Name: file.Name, Size: file.Size, UpdatedAt: receivedAt,
				LegacyTransport: legacyIPMSG, RemotePacket: packetHex,
				RemoteFile: fmt.Sprintf("%x", attachment.ID), RemotePeerID: peerID,
			})
			text := message
			if strings.TrimSpace(text) == "" {
				text = file.Name
			}
			addMessageState(s, domain.Message{
				ID: fileID, ConversationID: conversationID, SenderID: peer.ID,
				Kind: kind, FileID: fileID, Text: text,
				CreatedAt: file.CreatedAt, Status: "delivered",
			})
		}
		return nil
	})
	if err == nil && !admitted {
		return errors.New("飞鸽／飞秋联系人尚未登记")
	}
	if err == nil {
		for _, id := range downloads {
			n.startReceive(id, "")
		}
	}
	return err
}

// A request can arrive between receipt waits. Clear only the original offer.
func (n *Node) confirmLegacyFileRequest(id string) {
	n.mu.Lock()
	queued := false
	for _, p := range n.data.Outbox {
		if p.WireTransport == legacyIPMSG && p.Envelope.TransferID == id && p.Envelope.Type == "file_offer" {
			queued = true
			break
		}
	}
	n.mu.Unlock()
	if !queued {
		return
	}
	_ = n.durableFromReader(func(data *persisted) []pending {
		for key, p := range data.Outbox {
			if p.WireTransport == legacyIPMSG && p.Envelope.TransferID == id && p.Envelope.Type == "file_offer" {
				delete(data.Outbox, key)
			}
		}
		return nil
	})
}

func (n *Node) receiveLegacyProgress(packetNumber uint64, fileID uint32, done, total int64, complete bool, failure error) {
	packetHex, fileHex := fmt.Sprintf("%x", packetNumber), fmt.Sprintf("%x", fileID)
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
			if t.LegacyTransport != legacyIPMSG || t.RemotePacket != packetHex || t.RemoteFile != fileHex || t.Direction != "send" {
				continue
			}
			if terminalTransfer(t.Status) {
				return
			}
			t.Size = total
			updateTransferProgress(t, status, done, 0, problem)
			return
		}
	})
}

func legacyStableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func (n *Node) legacyPending(wireID string, value pending) error {
	n.mu.Lock()
	service := n.legacy
	n.mu.Unlock()
	if service == nil {
		return errors.New("飞鸽／飞秋互通服务尚未就绪")
	}
	e := value.Envelope
	switch e.Type {
	case "message":
		// Outbox sequence numbers survive restarts; a lost receipt must not
		// turn the next attempt into a different message on the remote client.
		packet := uint64(max(uint32(value.Sequence), 1))
		return service.SendTextConfirmed(n.ctx, wireID, packet, e.Message.Text)
	case "file_offer":
		return n.sendLegacyOffer(service, wireID, value)
	case "file_retry":
		// Older data may contain a native-style request from an inbound task.
		s := n.snapshot(false)
		if i := transferIndex(&s, e.TransferID); i >= 0 && s.Transfers[i].Direction == "receive" && s.Transfers[i].Status == "retry_requested" {
			return n.retryLegacyReceive(s.Transfers[i])
		}
		return nil
	default:
		// Protocol-specific control state has no IPMSG equivalent. Removing it
		// from the outbox keeps legacy peers from blocking later text/files.
		return nil
	}
}

func (n *Node) legacyCompleted(done []pending) error {
	return n.durable(func(data *persisted) []pending {
		for _, value := range done {
			delete(data.Outbox, value.Envelope.ID)
			e := value.Envelope
			if e.Type != "message" {
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
	wireID := transferWireID(&n.data, transfer)
	n.mu.Unlock()
	if service == nil || wireID == "" {
		return errors.New("飞鸽／飞秋互通服务尚未就绪")
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
		path, err := receiveDestination(s.Settings.DownloadDir, file.Name, s.Settings.DuplicatePolicy)
		if err != nil {
			return err
		}
		destination = path
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
	if err := n.beginReceive(ctx, transfer.ID, destination, offset); err != nil {
		return err
	}
	download := func(offset int64) error {
		started := time.Now()
		_, err := service.DownloadFile(ctx, wireID, packet, remoteFile, file.Size, offset, output, func(done int64) {
			speed := float64(done-offset) / time.Since(started).Seconds()
			n.setTransfer(transfer.ID, "transferring", done, speed, "")
		})
		return err
	}
	err = download(offset)
	if offset > 0 && offset < file.Size && errors.Is(err, io.EOF) && ctx.Err() == nil {
		current, statErr := output.Stat()
		if statErr != nil {
			return statErr
		}
		// Some originals close a nonzero request without sending any
		// data. Retain partial progress if this attempt wrote new bytes.
		if current.Size() == offset {
			if _, err := output.Seek(0, io.SeekStart); err != nil {
				return err
			}
			if err := output.Truncate(0); err != nil {
				return err
			}
			n.setTransfer(transfer.ID, "transferring", 0, 0, "")
			err = download(0)
		}
	}
	if err != nil {
		if errors.Is(err, io.EOF) && ctx.Err() == nil {
			if stat, statErr := output.Stat(); statErr == nil && stat.Size() == 0 && file.Size > 0 {
				return errors.New("对方未返回文件内容，原邀请可能已失效，请重试或让对方重新发送")
			}
		}
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
	return n.publishReceivedFile(ctx, transfer.ID, partial, destination, file.Size, "")
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
