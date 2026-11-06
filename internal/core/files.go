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

func terminalTransfer(status string) bool {
	return status == "completed" || status == "cancelled" || status == "rejected"
}
func validFile(f domain.File) bool {
	if f.ID != strings.ToLower(f.ID) || f.Hash != strings.ToLower(f.Hash) {
		return false
	}
	identifier, e := hex.DecodeString(f.ID)
	hash, he := hex.DecodeString(f.Hash)
	name := strings.ToUpper(strings.TrimSpace(strings.Split(f.Name, ".")[0]))
	reserved := name == "CON" || name == "PRN" || name == "AUX" || name == "NUL" || len(name) == 4 && (strings.HasPrefix(name, "COM") || strings.HasPrefix(name, "LPT")) && name[3] >= '1' && name[3] <= '9'
	for _, r := range f.Name {
		if r < 32 || r == 127 {
			return false
		}
	}
	return e == nil && len(identifier) == 16 && he == nil && len(hash) == 32 && f.Name != "" && f.Name != "." && f.Name != ".." && len(f.Name) <= 255 && !strings.ContainsAny(f.Name, "/\\:*?\"<>|\x00") && !reserved && !strings.HasSuffix(f.Name, ".") && !strings.HasSuffix(f.Name, " ") && f.Size >= 0
}
func fileHash(path string) (string, error) {
	return fileHashContext(context.Background(), path)
}
func fileHashContext(ctx context.Context, path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 256<<10)
	for {
		if e = ctx.Err(); e != nil {
			return "", e
		}
		nr, err := f.Read(buf)
		if nr > 0 {
			_, _ = h.Write(buf[:nr])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
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
	hash, err := fileHashContext(n.ctx, path)
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
	if target != "" {
		targets = []string{target}
	}
	return n.durable(func(p *persisted) []pending {
		s := &p.State
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
		var out []pending
		for _, id := range targets {
			out = append(out, appendFileOffer(s, id, convID, f, ""))
		}
		return out
	})
}
func (n *Node) offerFile(peer, convID string, f domain.File, tid string) {
	_ = n.durable(func(p *persisted) []pending { return []pending{appendFileOffer(&p.State, peer, convID, f, tid)} })
}
func appendFileOffer(s *domain.State, peer, convID string, f domain.File, tid string) pending {
	if tid == "" {
		tid = newID()
	}
	t := domain.Transfer{ID: tid, FileID: f.ID, ConversationID: convID, PeerID: peer, Direction: "send", Status: "offered", Name: f.Name, Size: f.Size, Path: f.SourcePath, UpdatedAt: time.Now(), FromShare: f.Shared}
	if i := transferIndex(s, tid); i < 0 {
		s.Transfers = append(s.Transfers, t)
	} else {
		s.Transfers[i].Status = "offered"
		s.Transfers[i].Error = ""
	}
	f.ConversationID = convID
	return pending{PeerID: peer, Envelope: envelope{Type: "file_offer", File: publicFile(f), TransferID: tid}}
}
func (n *Node) receiveOffer(peer string, e envelope) error {
	f := e.File
	f.SourcePath = ""
	s := n.Snapshot()
	n.mu.Lock()
	tombstone := n.data.TransferTombstones[peer+":"+e.TransferID]
	n.mu.Unlock()
	if tombstone != "" {
		return nil
	}
	if old := s.File(f.ID); old.ID != "" && old.SenderID != peer {
		return errors.New("文件身份冲突")
	}
	transferBytes, transferErr := hex.DecodeString(e.TransferID)
	if !validFile(f) || f.SenderID != peer || transferErr != nil || len(transferBytes) != 16 || e.TransferID != strings.ToLower(e.TransferID) {
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
	requested := false
	err := n.change(func(s *domain.State) {
		for i := range s.Transfers {
			v := &s.Transfers[i]
			if v.FileID == f.ID && v.PeerID == peer && v.Direction == "receive" && v.Status == "retry_requested" {
				v.Status = "cancelled"
				v.Error = "已重新发起接收"
				requested = true
			}
		}
		if i := fileIndex(s, f.ID); i < 0 {
			s.Files = append(s.Files, f)
		} else if s.Files[i].ConversationID == "" {
			s.Files[i].ConversationID = f.ConversationID
		}
		if transferIndex(s, e.TransferID) < 0 {
			s.Transfers = append(s.Transfers, domain.Transfer{ID: e.TransferID, FileID: f.ID, ConversationID: f.ConversationID, PeerID: peer, Direction: "receive", Status: "offered", Name: f.Name, Size: f.Size, UpdatedAt: time.Now()})
			kind := "file"
			if strings.HasPrefix(f.MIME, "image/") {
				kind = "image"
			}
			found := false
			for _, m := range s.Messages {
				if m.ID == f.ID {
					found = true
					break
				}
			}
			if !found {
				s.Messages = append(s.Messages, domain.Message{ID: f.ID, ConversationID: f.ConversationID, SenderID: peer, Kind: kind, Text: f.Name, FileID: f.ID, Status: "delivered", CreatedAt: f.CreatedAt})
			}
			if i := convIndex(s, f.ConversationID); i >= 0 {
				s.Conversations[i].Unread++
				s.Conversations[i].Preview = "[文件] " + f.Name
				s.Conversations[i].UpdatedAt = f.CreatedAt
			}
		}
	})
	if err != nil {
		return err
	}
	if s.Settings.AutoAccept || requested {
		n.startReceive(e.TransferID, "")
	}
	return nil
}
func (n *Node) setTransfer(id, status string, done int64, speed float64, problem string) {
	n.change(func(s *domain.State) {
		if i := transferIndex(s, id); i >= 0 {
			t := &s.Transfers[i]
			// Control acknowledgements and data-stream completion run independently.
			// A late progress update must not undo a terminal task state.
			if terminalTransfer(t.Status) {
				return
			}
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
				n.reliable(t.PeerID, envelope{Type: "file_status", TransferID: id, Status: status})
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
	if t.Direction != "receive" {
		return errors.New("此任务不是接收任务")
	}
	if t.Status == "completed" {
		return nil
	}
	f := s.File(t.FileID)
	if !validFile(f) {
		return errors.New("文件信息不完整")
	}
	p := s.Peer(t.PeerID)
	if destination == "" {
		destination = t.Path
	}
	if destination != "" {
		if info, e := os.Stat(destination); e == nil && info.Mode().IsRegular() && info.Size() == f.Size {
			if hash, e := fileHashContext(ctx, destination); e == nil && hash == f.Hash {
				return n.completeReceive(t, destination)
			}
		}
	}
	if !p.Online {
		n.setTransfer(id, "waiting", t.Done, 0, "")
		return nil
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
	hash, err := fileHashContext(ctx, partial)
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
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = installFile(partial, destination); err != nil {
		return err
	}
	return n.completeReceive(t, destination)
}
func installFile(source, dest string) error {
	in, e := os.Open(source)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.CreateTemp(filepath.Dir(dest), ".goim-install-*")
	if e != nil {
		return e
	}
	temporary := out.Name()
	defer func() {
		out.Close()
		_ = os.Remove(temporary)
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
	// Linking a completed same-volume temporary file atomically refuses an
	// existing destination on all supported hosts; it never overwrites user data.
	if e = os.Link(temporary, dest); e != nil {
		return fmt.Errorf("无法安全完成文件（请选择支持硬链接的本地目录）: %w", e)
	}
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
	if t.Direction != "send" || t.PeerID != peer || t.FileID != f.ID || f.SenderID != s.Self.ID || e.Offset < 0 || e.Offset > f.Size || t.Status == "cancelled" || t.Status == "rejected" || t.Revoked || t.FromShare && !f.Shared {
		return
	}
	if strings.HasPrefix(t.ConversationID, "group:") {
		g := s.Group(strings.TrimPrefix(t.ConversationID, "group:"))
		if g.Dissolved || !contains(g.Members, peer) || !contains(g.Members, s.Self.ID) {
			return
		}
	}
	ctx, finish, ok := n.transferContext(t.ID)
	if !ok {
		return
	}
	defer finish()
	stop := watchConnection(ctx, conn)
	defer stop()
	hash, err := fileHashContext(ctx, f.SourcePath)
	if err != nil || hash != f.Hash {
		_ = writeFrame(conn, envelope{Type: "file_error", Status: "源文件已变化或不可用"})
		return
	}
	current := n.Snapshot()
	if i := transferIndex(&current, t.ID); i < 0 || current.Transfers[i].Status == "cancelled" || current.Transfers[i].Status == "rejected" {
		return
	}
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
		if strings.HasPrefix(t.ConversationID, "group:") {
			now := n.Snapshot()
			g := now.Group(strings.TrimPrefix(t.ConversationID, "group:"))
			if g.Left || g.Dissolved || !contains(g.Members, peer) || !contains(g.Members, now.Self.ID) {
				return
			}
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
	_ = n.send(peer, envelope{Type: "catalog", Files: list, CatalogRevision: s.Revision})
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
