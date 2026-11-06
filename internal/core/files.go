package core

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/domain"
)

func validFile(f domain.File) bool {
	identifier, e := hex.DecodeString(f.ID)
	hash, he := hex.DecodeString(f.Hash)
	return e == nil && len(identifier) == 16 && he == nil && len(hash) == 32 && f.Name != "" && f.Name != "." && f.Name != ".." && len(f.Name) <= 255 && !strings.ContainsAny(f.Name, "/\\:*?\"<>|\x00") && f.Size >= 0
}
func fileHash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func publicFile(f domain.File) domain.File { f.SourcePath = ""; return f }
func (n *Node) sendFile(convID, path string, shared bool, target string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("首期仅支持文件，请先将文件夹打包")
	}
	hash, err := fileHash(path)
	if err != nil {
		return err
	}
	s := n.Snapshot()
	f := domain.File{ID: newID(), Name: filepath.Base(path), Size: info.Size(), Hash: hash, MIME: mime.TypeByExtension(strings.ToLower(filepath.Ext(path))), SenderID: s.Self.ID, ConversationID: convID, SourcePath: path, Shared: shared, CreatedAt: time.Now()}
	if !validFile(f) {
		return errors.New("文件名不适合跨平台传输")
	}
	var targets []string
	if !shared {
		c := s.Conversation(convID)
		if c.ID == "" {
			return errors.New("请选择会话")
		}
		targets, err = recipients(s, c)
		if err != nil {
			return err
		}
	}
	n.change(func(s *domain.State) {
		s.Files = append(s.Files, f)
		if !shared {
			kind := "file"
			if strings.HasPrefix(f.MIME, "image/") {
				kind = "image"
			}
			s.Messages = append(s.Messages, domain.Message{ID: f.ID, ConversationID: convID, SenderID: s.Self.ID, Kind: kind, FileID: f.ID, Text: f.Name, CreatedAt: f.CreatedAt, Status: "sent"})
			if i := convIndex(s, convID); i >= 0 {
				s.Conversations[i].Preview = "[文件] " + f.Name
				s.Conversations[i].UpdatedAt = f.CreatedAt
			}
		}
	})
	if target != "" {
		targets = []string{target}
	}
	for _, id := range targets {
		n.offerFile(id, convID, f, "")
	}
	return nil
}
func (n *Node) offerFile(peer, convID string, f domain.File, tid string) {
	if tid == "" {
		tid = f.ID + "-" + peer
	}
	n.change(func(s *domain.State) {
		t := domain.Transfer{ID: tid, FileID: f.ID, ConversationID: convID, PeerID: peer, Direction: "send", Status: "offered", Name: f.Name, Size: f.Size, Path: f.SourcePath, UpdatedAt: time.Now()}
		if i := transferIndex(s, tid); i < 0 {
			s.Transfers = append(s.Transfers, t)
		} else {
			s.Transfers[i].Status = "offered"
			s.Transfers[i].Error = ""
		}
	})
	f.ConversationID = convID
	n.reliable(peer, envelope{Type: "file_offer", File: publicFile(f), TransferID: tid})
}
func (n *Node) receiveOffer(peer string, e envelope) error {
	f := e.File
	s := n.Snapshot()
	if !validFile(f) || f.SenderID != peer || e.TransferID != f.ID+"-"+s.Self.ID {
		return errors.New("无效文件邀请")
	}
	if strings.HasPrefix(f.ConversationID, "group:") {
		g := s.Group(strings.TrimPrefix(f.ConversationID, "group:"))
		if g.ID == "" || g.Left || g.Dissolved || !contains(g.Members, peer) || !contains(g.Members, s.Self.ID) {
			return errors.New("群文件权限无效")
		}
	} else {
		f.ConversationID = directID(s.Self.ID, peer)
		n.change(func(st *domain.State) { ensureDirect(st, s.Peer(peer)) })
	}
	n.change(func(s *domain.State) {
		if fileIndex(s, f.ID) < 0 {
			s.Files = append(s.Files, f)
		}
		if transferIndex(s, e.TransferID) < 0 {
			s.Transfers = append(s.Transfers, domain.Transfer{ID: e.TransferID, FileID: f.ID, ConversationID: f.ConversationID, PeerID: peer, Direction: "receive", Status: "offered", Name: f.Name, Size: f.Size, UpdatedAt: time.Now()})
			kind := "file"
			if strings.HasPrefix(f.MIME, "image/") {
				kind = "image"
			}
			s.Messages = append(s.Messages, domain.Message{ID: f.ID, ConversationID: f.ConversationID, SenderID: peer, Kind: kind, Text: f.Name, FileID: f.ID, Status: "delivered", CreatedAt: f.CreatedAt})
			if i := convIndex(s, f.ConversationID); i >= 0 {
				s.Conversations[i].Unread++
				s.Conversations[i].Preview = "[文件] " + f.Name
				s.Conversations[i].UpdatedAt = f.CreatedAt
			}
		}
	})
	if s.Settings.AutoAccept || strings.HasPrefix(f.MIME, "image/") && f.Size <= 20<<20 {
		n.startReceive(e.TransferID, "")
	}
	return nil
}
func (n *Node) setTransfer(id, status string, done int64, speed float64, problem string) {
	n.change(func(s *domain.State) {
		if i := transferIndex(s, id); i >= 0 {
			t := &s.Transfers[i]
			t.Status = status
			t.Done = done
			t.Speed = speed
			t.Error = problem
			t.UpdatedAt = time.Now()
		}
	})
}
func (n *Node) transferContext(id string) (context.Context, func(), bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.transfers[id]; ok || n.closing {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(n.ctx)
	n.transfers[id] = cancel
	return ctx, func() { cancel(); n.mu.Lock(); delete(n.transfers, id); n.mu.Unlock() }, true
}
func watchConnection(ctx context.Context, c net.Conn) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}
func (n *Node) startReceive(id, destination string) {
	ctx, finish, ok := n.transferContext(id)
	if !ok {
		return
	}
	if !n.async(func() {
		defer finish()
		if err := n.download(ctx, id, destination); err != nil {
			if ctx.Err() != nil {
				return
			}
			s := n.Snapshot()
			if i := transferIndex(&s, id); i >= 0 {
				t := s.Transfers[i]
				status := "failed"
				if !s.Peer(t.PeerID).Online {
					status = "waiting"
				}
				n.setTransfer(id, status, t.Done, 0, err.Error())
			}
		}
	}) {
		finish()
	}
}
func (n *Node) download(ctx context.Context, id, destination string) error {
	s := n.Snapshot()
	i := transferIndex(&s, id)
	if i < 0 {
		return errors.New("找不到任务")
	}
	t := s.Transfers[i]
	if t.Status == "completed" {
		return nil
	}
	f := s.File(t.FileID)
	if !validFile(f) {
		return errors.New("文件信息不完整")
	}
	p := s.Peer(t.PeerID)
	if !p.Online {
		n.setTransfer(id, "waiting", t.Done, 0, "")
		return nil
	}
	if destination == "" {
		destination = t.Path
	}
	if destination == "" {
		destination = filepath.Join(s.Settings.DownloadDir, f.Name)
		if _, e := os.Stat(destination); e == nil {
			if s.Settings.DuplicatePolicy == "ask" {
				return errors.New("同名文件存在，请选择另存为")
			}
			ext := filepath.Ext(f.Name)
			base := strings.TrimSuffix(f.Name, ext)
			for j := 1; ; j++ {
				candidate := filepath.Join(s.Settings.DownloadDir, fmt.Sprintf("%s (%d)%s", base, j, ext))
				if _, e := os.Lstat(candidate); os.IsNotExist(e) {
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
	partial := filepath.Join(partialDir, id+".part")
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	offset := info.Size()
	if offset > f.Size {
		if err = file.Truncate(0); err != nil {
			return err
		}
		offset = 0
	}
	if _, err = file.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	n.change(func(s *domain.State) {
		if i := transferIndex(s, id); i >= 0 {
			s.Transfers[i].Path = destination
			s.Transfers[i].Done = offset
			s.Transfers[i].Status = "transferring"
			s.Transfers[i].Error = ""
		}
	})
	d := net.Dialer{Timeout: 5 * time.Second}
	raw, err := d.DialContext(ctx, "tcp", p.Address)
	if err != nil {
		return err
	}
	conn := tls.Client(raw, n.tls.Clone())
	defer conn.Close()
	stop := watchConnection(ctx, conn)
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err = conn.HandshakeContext(ctx); err != nil {
		return err
	}
	server, err := readFrame(conn)
	if err != nil {
		return err
	}
	certKey, ok := conn.ConnectionState().PeerCertificates[0].PublicKey.(ed25519.PublicKey)
	if !ok || server.Type != "hello" || server.Peer.ID != p.ID || keyID(certKey) != p.ID {
		return errors.New("文件来源身份不匹配")
	}
	request := n.hello()
	request.Type = "file_request"
	request.TransferID = id
	request.File = domain.File{ID: f.ID}
	request.Offset = offset
	if err = writeFrame(conn, request); err != nil {
		return err
	}
	ready, err := readFrame(conn)
	if err != nil {
		return err
	}
	if ready.Type != "file_ready" || ready.File.Hash != f.Hash || ready.Offset != offset {
		return errors.New("源文件已变化或传输权限失效")
	}
	buffer := make([]byte, 256<<10)
	done := offset
	start := time.Now()
	last := start
	for done < f.Size {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
		count := int64(len(buffer))
		if remain := f.Size - done; remain < count {
			count = remain
		}
		nr, e := conn.Read(buffer[:count])
		if nr > 0 {
			nw, werr := file.Write(buffer[:nr])
			done += int64(nw)
			if werr != nil {
				return werr
			}
			if time.Since(last) >= 200*time.Millisecond {
				n.setTransfer(id, "transferring", done, float64(done-offset)/time.Since(start).Seconds(), "")
				last = time.Now()
			}
		}
		if e != nil {
			if e == io.EOF && done == f.Size {
				break
			}
			return e
		}
	}
	if err = file.Sync(); err != nil {
		return err
	}
	n.setTransfer(id, "verifying", done, 0, "")
	hash, err := fileHash(partial)
	if err != nil {
		return err
	}
	if hash != f.Hash {
		_ = file.Truncate(0)
		return errors.New("完整性校验失败，请重试")
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = installFile(partial, destination); err != nil {
		return err
	}
	n.setTransfer(id, "completed", done, 0, "")
	n.reliable(p.ID, envelope{Type: "file_status", TransferID: id, Status: "completed"})
	return nil
}
func installFile(source, dest string) error {
	in, e := os.Open(source)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	success := false
	defer func() {
		out.Close()
		if !success {
			_ = os.Remove(dest)
		}
	}()
	if _, e = io.Copy(out, in); e != nil {
		return e
	}
	if e = out.Sync(); e != nil {
		return e
	}
	if e = out.Close(); e != nil {
		return e
	}
	success = true
	in.Close()
	return os.Remove(source)
}
func (n *Node) serveFile(conn *tls.Conn, e envelope, peer string) {
	s := n.Snapshot()
	i := transferIndex(&s, e.TransferID)
	if i < 0 {
		return
	}
	t := s.Transfers[i]
	f := s.File(e.File.ID)
	if t.Direction != "send" || t.PeerID != peer || t.FileID != f.ID || f.SenderID != s.Self.ID || e.Offset < 0 || e.Offset > f.Size || t.Status == "cancelled" || t.Status == "rejected" {
		return
	}
	if strings.HasPrefix(t.ConversationID, "group:") {
		g := s.Group(strings.TrimPrefix(t.ConversationID, "group:"))
		if g.Dissolved || !contains(g.Members, peer) || !contains(g.Members, s.Self.ID) {
			return
		}
	}
	hash, err := fileHash(f.SourcePath)
	if err != nil || hash != f.Hash {
		return
	}
	ctx, finish, ok := n.transferContext(t.ID)
	if !ok {
		return
	}
	defer finish()
	stop := watchConnection(ctx, conn)
	defer stop()
	file, err := os.Open(f.SourcePath)
	if err != nil {
		return
	}
	defer file.Close()
	if _, err = file.Seek(e.Offset, io.SeekStart); err != nil {
		return
	}
	if err = writeFrame(conn, envelope{Type: "file_ready", File: publicFile(f), Offset: e.Offset}); err != nil {
		return
	}
	done := e.Offset
	start := time.Now()
	last := start
	n.setTransfer(t.ID, "transferring", done, 0, "")
	buf := make([]byte, 256<<10)
	for done < f.Size {
		if ctx.Err() != nil {
			return
		}
		count := int64(len(buf))
		if remain := f.Size - done; remain < count {
			count = remain
		}
		nr, err := file.Read(buf[:count])
		if nr > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
			nw, werr := conn.Write(buf[:nr])
			done += int64(nw)
			if werr != nil {
				n.setTransfer(t.ID, "waiting", done, 0, werr.Error())
				return
			}
			if time.Since(last) >= 200*time.Millisecond {
				n.setTransfer(t.ID, "transferring", done, float64(done-e.Offset)/time.Since(start).Seconds(), "")
				last = time.Now()
			}
		}
		if err != nil {
			if err != io.EOF {
				n.setTransfer(t.ID, "failed", done, 0, err.Error())
			}
			break
		}
	}
	if done == f.Size {
		n.setTransfer(t.ID, "verifying", done, 0, "")
	}
}
func (n *Node) sendCatalog(peer string) {
	s := n.Snapshot()
	var list []domain.File
	for _, f := range s.Files {
		if f.Shared && f.SenderID == s.Self.ID {
			list = append(list, publicFile(f))
		}
	}
	_ = n.send(peer, envelope{Type: "catalog", Files: list})
}
func (n *Node) broadcastCatalog() {
	s := n.Snapshot()
	for _, p := range s.Peers {
		if p.Online {
			n.sendCatalog(p.ID)
		}
	}
}
func (n *Node) offerShared(peer, id string) error {
	s := n.Snapshot()
	f := s.File(id)
	if !f.Shared || f.SenderID != s.Self.ID {
		return errors.New("此分享已不可用")
	}
	var conv string
	n.change(func(st *domain.State) { conv = ensureDirect(st, s.Peer(peer)) })
	n.offerFile(peer, conv, f, "")
	return nil
}
