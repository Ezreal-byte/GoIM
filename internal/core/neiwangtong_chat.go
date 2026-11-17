package core

import (
	"bytes"
	"context"
	"crypto/md5" // the digest in the native chat image reference
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
	"goim/internal/neiwangtong"
)

func nativeMessagePacket(value pending) uint64 {
	// Outbox sequence numbers survive restarts and are unique among the
	// practical number of outstanding messages to any one contact.
	return uint64(max(uint32(value.Sequence), 1))
}

func (n *Node) receiveNeiwangtongMessage(peerID string, message neiwangtong.DirectMessage) error {
	if !validNativeChatParts(message) {
		return errors.New("invalid native chat message")
	}
	now := time.Now()
	stamp := now
	if at := message.OfflineAt; !at.IsZero() && !at.After(now.Add(5*time.Minute)) {
		stamp = at
	}
	var downloads, cancelled []string
	var recognized bool
	err := n.durable(func(p *persisted) []pending {
		s := &p.State
		peer := n.compatibilitySenderLocked(transportNWT, peerID)
		if peer.ID == "" {
			return nil
		}
		recognized = true
		target := nativeChatTarget{
			conversation: ensureDirect(s, peer), sender: peer.ID, wirePeer: peerID,
		}
		downloads, cancelled = applyNativeChat(p, target, message, now, stamp)
		return nil
	})
	if err != nil {
		return err
	}
	if !recognized {
		return neiwangtong.ErrNotConnected
	}
	n.cancelRecalledTransfers(cancelled)
	for _, id := range downloads {
		n.startReceive(id, "")
	}
	return nil
}

type nativeChatTarget struct {
	conversation, sender, wirePeer, group string
}

func (target nativeChatTarget) partID(messageID string, index int) string {
	if target.group == "" {
		return legacyStableID("nwt-chat", target.wirePeer, messageID, strconv.Itoa(index))
	}
	if index == 0 {
		return legacyStableID("nwt-group-message", target.group, target.wirePeer, messageID)
	}
	return legacyStableID("nwt-group-message", target.group, target.wirePeer, messageID, strconv.Itoa(index))
}

func validNativeChatParts(message neiwangtong.DirectMessage) bool {
	text := message.Text()
	return len(text) <= 256<<10 && utf8.ValidString(text) && len(message.Parts) <= maxNativeChatParts
}

const maxNativeChatParts = 64

func applyNativeChat(p *persisted, target nativeChatTarget, message neiwangtong.DirectMessage, now, stamp time.Time) (downloads, cancelled []string) {
	if message.RecallID != "" {
		return nil, recallNativeChat(p, target, message.RecallID)
	}
	wireID := nativeMessageIdentity(message)
	if p.MessageRecalls[messageRecallKey(target.sender, target.conversation, target.partID(wireID, 0))] {
		return nil, nil
	}
	s := &p.State
	for index := 0; index < len(message.Parts); {
		part := message.Parts[index]
		id := target.partID(wireID, index)
		entry := domain.Message{ID: id, ConversationID: target.conversation, SenderID: target.sender,
			Status: "delivered", CreatedAt: stamp.Add(time.Duration(index)), Kind: "text"}
		index++
		if part.File != nil {
			name := filepath.Base(strings.ReplaceAll(part.File.Name, "\\", "/"))
			file := domain.File{ID: id, Name: name, Size: part.File.Size, Hash: strings.Repeat("0", 64),
				MIME:     mime.TypeByExtension(strings.ToLower(filepath.Ext(name))),
				SenderID: target.sender, ConversationID: target.conversation, CreatedAt: entry.CreatedAt}
			if !validFile(file) || fileIndex(s, id) >= 0 {
				continue
			}
			s.Files = append(s.Files, file)
			s.Transfers = append(s.Transfers, domain.Transfer{ID: id, FileID: id, ConversationID: target.conversation,
				PeerID: target.sender, Direction: "receive", Status: "offered", Name: name, Size: file.Size, UpdatedAt: now,
				LegacyTransport: transportNWT, RemotePeerID: target.wirePeer, RemoteShareID: part.File.ID})
			entry.Kind, entry.Text, entry.FileID = "file", name, id
			addMessageState(s, entry)
			continue
		}
		if part.Image == nil {
			var span strings.Builder
			span.WriteString(part.Text)
			for index < len(message.Parts) && message.Parts[index].Image == nil && message.Parts[index].File == nil {
				span.WriteString(message.Parts[index].Text)
				index++
			}
			entry.Text = span.String()
			if entry.Text != "" && !hasNativeMessage(s, id) {
				addMessageState(s, entry)
			}
			continue
		}
		if fileIndex(s, id) >= 0 {
			continue
		}
		file := domain.File{ID: id, Name: "图片", Hash: strings.Repeat("0", 64),
			InlineImage: true, SenderID: target.sender, ConversationID: target.conversation, CreatedAt: entry.CreatedAt}
		s.Files = append(s.Files, file)
		s.Transfers = append(s.Transfers, domain.Transfer{ID: id, FileID: id, ConversationID: target.conversation,
			PeerID: target.sender, Direction: "receive", Status: "waiting", Name: file.Name, UpdatedAt: now,
			LegacyTransport: transportNWT, RemotePeerID: target.wirePeer,
			RemoteFile: strconv.FormatUint(uint64(part.Image.Selector), 10), RemoteImageMD5: hex.EncodeToString(part.Image.Digest[:])})
		entry.Kind, entry.Text, entry.FileID = "image", "[图片]", id
		addMessageState(s, entry)
		downloads = append(downloads, id)
	}
	return downloads, nil
}

func recallNativeChat(p *persisted, target nativeChatTarget, wireID string) []string {
	ids := make(map[string]bool, maxNativeChatParts)
	for i := range maxNativeChatParts {
		ids[target.partID(wireID, i)] = true
	}
	var cancelled []string
	for _, m := range p.State.Messages {
		if m.SenderID == target.sender && m.ConversationID == target.conversation && ids[m.ID] && !m.Recalled {
			cancelled = append(cancelled, applyMessageRecall(p, m)...)
			// An original message can contain several local bubbles. Mark its
			// first part so a retry cannot re-create any of those bubbles.
			p.MessageRecalls[messageRecallKey(target.sender, target.conversation, target.partID(wireID, 0))] = true
		}
	}
	return cancelled
}

func nativeMessageIdentity(message neiwangtong.DirectMessage) string {
	wireID := strings.ToLower(message.ID)
	if id, err := hex.DecodeString(wireID); err != nil || len(id) != 16 {
		return legacyStableID(strconv.FormatUint(message.PacketID, 10), message.Text())
	}
	return wireID
}

func hasNativeMessage(s *domain.State, id string) bool {
	for _, m := range s.Messages {
		if m.ID == id {
			return true
		}
	}
	return false
}

func (n *Node) sendNeiwangtongImage(service *neiwangtong.Service, peer, room string, value pending, taskID, fileID uint64, f domain.File) error {
	fh, err := os.Open(f.SourcePath)
	if err != nil {
		return err
	}
	defer fh.Close()
	if !validInlineImage(f) {
		return errors.New("图片超出了内网通聊天图片的范围")
	}
	h, d := sha256.New(), md5.New()
	written, err := io.Copy(io.MultiWriter(h, d), io.LimitReader(fh, f.Size+1))
	if err != nil {
		return err
	}
	if written != f.Size || hex.EncodeToString(h.Sum(nil)) != f.Hash {
		return errors.New("源图片已变化，请重新发送")
	}
	var digest [16]byte
	d.Sum(digest[:0])
	if room != "" {
		return service.SendGroupImage(peer, room, nativeMessagePacket(value), f.ID, taskID, fileID, f.Size, digest, f.SourcePath)
	}
	return service.SendChatImage(n.ctx, peer, nativeMessagePacket(value), f.ID, taskID, fileID, f.Size, digest, f.SourcePath)
}

func (n *Node) downloadNeiwangtongImage(ctx context.Context, service *neiwangtong.Service, peer string,
	t domain.Transfer, f domain.File, destination string) error {
	selector, err := strconv.ParseUint(t.RemoteFile, 10, 32)
	if err != nil || len(t.RemoteImageMD5) != 32 {
		return errors.New("内网通图片引用无效")
	}
	ref := neiwangtong.ChatImageRef{Selector: uint32(selector)}
	if _, err := hex.Decode(ref.Digest[:], []byte(t.RemoteImageMD5)); err != nil {
		return err
	}
	n.setTransfer(t.ID, "transferring", 0, 0, "")
	data, err := service.FetchChatImage(ctx, peer, ref)
	if err != nil {
		return err
	}
	mime := inspectInlineImage(bytes.NewReader(data))
	if mime == "" {
		return errors.New("内网通图片格式或尺寸不受支持")
	}
	name := "图片" + imageSuffix(mime)
	if destination == "" {
		destination = filepath.Join(n.dir, "chat-images", f.ID+imageSuffix(mime))
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := saveNativeImage(ctx, destination, data, digest); err != nil {
		return err
	}
	return n.change(func(s *domain.State) {
		i, j := transferIndex(s, t.ID), fileIndex(s, f.ID)
		if i < 0 || j < 0 || terminalTransfer(s.Transfers[i].Status) || ctx.Err() != nil {
			return
		}
		size := int64(len(data))
		local := &s.Files[j]
		local.Name, local.Size, local.Hash, local.MIME = name, size, digest, mime
		task := &s.Transfers[i]
		task.Name, task.Path, task.Status, task.Error = name, destination, "completed", ""
		task.Done, task.Size, task.Speed, task.UpdatedAt = size, size, 0, time.Now()
	})
}

func saveNativeImage(ctx context.Context, dest string, data []byte, digest string) error {
	if existing, err := fileHashContext(ctx, dest); err == nil && existing == digest {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(dest), ".goim-image-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, err = temp.Write(data)
	closeErr := temp.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return installFile(temp.Name(), dest)
}
