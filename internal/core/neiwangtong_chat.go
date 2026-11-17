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
	text := message.Text()
	if len(text) > 256<<10 || !utf8.ValidString(text) || len(message.Parts) > 64 {
		return errors.New("invalid native chat message")
	}
	now := time.Now()
	stamp := now
	if at := message.OfflineAt; !at.IsZero() && !at.After(now.Add(5*time.Minute)) {
		stamp = at
	}
	wireID := nativeMessageIdentity(message)
	var downloads []string
	var recognized bool
	err := n.change(func(s *domain.State) {
		peer := n.compatibilitySenderLocked(transportNWT, peerID)
		if peer.ID == "" {
			return
		}
		recognized = true
		conv := ensureDirect(s, peer)
		for i := 0; i < len(message.Parts); {
			part := message.Parts[i]
			id := legacyStableID("nwt-chat", peerID, wireID, strconv.Itoa(i))
			m := domain.Message{ID: id, ConversationID: conv, SenderID: peer.ID,
				Status: "delivered", CreatedAt: stamp.Add(time.Duration(i)), Kind: "text"}
			i++
			if part.Image == nil {
				var span strings.Builder
				span.WriteString(part.Text)
				for i < len(message.Parts) && message.Parts[i].Image == nil {
					span.WriteString(message.Parts[i].Text)
					i++
				}
				m.Text = span.String()
				if m.Text != "" && !hasNativeMessage(s, id) {
					addMessageState(s, m)
				}
				continue
			}
			if fileIndex(s, id) >= 0 {
				continue
			}
			f := domain.File{ID: id, Name: "图片", Hash: strings.Repeat("0", 64),
				InlineImage: true, SenderID: peer.ID, ConversationID: conv, CreatedAt: m.CreatedAt}
			s.Files = append(s.Files, f)
			s.Transfers = append(s.Transfers, domain.Transfer{ID: id, FileID: id, ConversationID: conv,
				PeerID: peer.ID, Direction: "receive", Status: "waiting", Name: f.Name, UpdatedAt: now,
				LegacyTransport: transportNWT, RemotePeerID: peerID,
				RemoteFile: strconv.FormatUint(uint64(part.Image.Selector), 10), RemoteImageMD5: hex.EncodeToString(part.Image.Digest[:])})
			m.Kind, m.Text, m.FileID = "image", "[图片]", id
			addMessageState(s, m)
			downloads = append(downloads, id)
		}
	})
	if err != nil {
		return err
	}
	if !recognized {
		return neiwangtong.ErrNotConnected
	}
	for _, id := range downloads {
		n.startReceive(id, "")
	}
	return nil
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

func (n *Node) sendNeiwangtongImage(service *neiwangtong.Service, peer string, value pending, taskID, fileID uint64, f domain.File) error {
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
