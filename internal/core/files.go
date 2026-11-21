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
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"goim/internal/domain"
)

func terminalTransfer(status string) bool {
	return status == "completed" || status == "cancelled" || status == "rejected"
}
func validFile(f domain.File) bool {
	if f.ShareFolder || f.Folder {
		return false
	}
	if f.ID != strings.ToLower(f.ID) || f.Hash != strings.ToLower(f.Hash) {
		return false
	}
	identifier, e := hex.DecodeString(f.ID)
	hash, he := hex.DecodeString(f.Hash)
	return e == nil && len(identifier) == 16 && he == nil && len(hash) == 32 && validFileName(f.Name) && f.Size >= 0
}

func validFileName(filename string) bool {
	name := strings.ToUpper(strings.TrimSpace(strings.SplitN(filename, ".", 2)[0]))
	reserved := name == "CON" || name == "PRN" || name == "AUX" || name == "NUL" || len(name) == 4 && (strings.HasPrefix(name, "COM") || strings.HasPrefix(name, "LPT")) && name[3] >= '1' && name[3] <= '9'
	for _, r := range filename {
		if r < 32 || r == 127 {
			return false
		}
	}
	return utf8.ValidString(filename) && filename != "" && filename != "." && filename != ".." && len(filename) <= 255 &&
		!strings.ContainsAny(filename, "/\\:*?\"<>|\x00") && !reserved && !strings.HasSuffix(filename, ".") && !strings.HasSuffix(filename, " ")
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
	return fileReaderHashContext(ctx, f)
}

func fileReaderHashContext(ctx context.Context, f io.Reader) (string, error) {
	h := sha256.New()
	buf := make([]byte, 256<<10)
	for {
		if e := ctx.Err(); e != nil {
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
	return n.sendFileWithLink(convID, path, shared, target, browserShare{})
}
func (n *Node) sendFileWithLink(convID, path string, shared bool, target string, link browserShare) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if link.Token != "" {
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		if info.IsDir() && !shared {
			return n.sendNeiwangtongDirectory(convID, path, target)
		}
		return errors.New("首期仅支持文件，请先将文件夹打包")
	}
	if !shared && info.Size() >= neiwangtongLargeFileSize {
		// Use the last observed client version even when the contact is offline,
		// before reading a multi-gigabyte file that the route cannot deliver.
		n.mu.Lock()
		sizeErr := n.data.neiwangtongAttachmentSizeError(n.data.State.Conversation(convID), info.Size())
		n.mu.Unlock()
		if sizeErr != nil {
			return sizeErr
		}
	}
	hash, err := fileHashContext(n.ctx, path)
	if err != nil {
		return err
	}
	f := domain.File{ID: newID(), Name: filepath.Base(path), Size: info.Size(), Hash: hash, MIME: mime.TypeByExtension(strings.ToLower(filepath.Ext(path))), ConversationID: convID, SourcePath: path, Shared: shared, CreatedAt: time.Now()}
	if !validFile(f) {
		return errors.New("文件名不适合跨平台传输")
	}
	if !shared {
		if mediaType := inlineImageType(path, f.Size); mediaType != "" {
			f.MIME, f.InlineImage = mediaType, true
		}
	}
	var denied error
	err = n.durable(func(p *persisted) []pending {
		s := &p.State
		f.SenderID = s.Self.ID
		var targets []string
		if !shared {
			c := s.Conversation(convID)
			targets, denied = attachmentRecipients(*s, c, target)
			if denied != nil {
				return nil
			}
			if denied = p.neiwangtongAttachmentSizeError(c, f.Size); denied != nil {
				return nil
			}
			nativeGroup := p.NativeGroups[c.GroupID].WireID != ""
			for _, id := range targets {
				transport := s.Peer(id).Transport
				if c.Kind == "group" && (transport == legacyIPMSG || transport == transportNWT && !nativeGroup) {
					denied = errors.New("内网通兼容联系人仅支持单聊发送")
					return nil
				}
				if transport == legacyIPMSG {
					f.InlineImage = false
				}
			}
		} else if target != "" {
			targets = []string{target}
		}
		if link.Token != "" {
			for _, existing := range s.Files {
				same := existing.SourcePath == f.SourcePath || runtime.GOOS == "windows" && strings.EqualFold(existing.SourcePath, f.SourcePath)
				if existing.Shared && existing.SenderID == s.Self.ID && same {
					denied = errors.New("这个文件已在共享中，请从项目菜单管理链接或密码")
					return nil
				}
			}
			if p.BrowserShares == nil {
				p.BrowserShares = make(map[string]browserShare)
			}
			link.Modified = info.ModTime()
			p.BrowserShares[f.ID] = link
		}
		s.Files = append(s.Files, f)
		if !shared {
			kind := "file"
			if f.InlineImage {
				kind = "image"
			}
			p.MessageTargets[f.ID] = targets
			s.Messages = append(s.Messages, domain.Message{ID: f.ID, ConversationID: convID, SenderID: s.Self.ID, Kind: kind, FileID: f.ID, Text: f.Name, CreatedAt: f.CreatedAt, Status: "sent"})
			if i := convIndex(s, convID); i >= 0 {
				s.Conversations[i].Hidden = false
				s.Conversations[i].Preview = attachmentPreview(f)
				s.Conversations[i].UpdatedAt = f.CreatedAt
			}
		}
		var out []pending
		for _, id := range targets {
			out = append(out, appendFileOffer(s, id, convID, f, "", shared))
		}
		return out
	})
	return errors.Join(err, denied)
}
func (n *Node) reofferFile(previous domain.Transfer, reuseID string) error {
	var denied error
	err := n.durable(func(p *persisted) []pending {
		i := transferIndex(&p.State, previous.ID)
		if i < 0 {
			denied = errors.New("找不到原文件任务")
			return nil
		}
		t := p.State.Transfers[i]
		if t.Direction != "send" || t.PeerID != previous.PeerID || t.FileID != previous.FileID {
			denied = errors.New("无效的重新接收请求")
			return nil
		}
		f := p.State.File(t.FileID)
		if t.FromShare && (t.Revoked || !publicSharedFile(p, f)) {
			denied = errors.New("此分享已不可用")
			return nil
		}
		if !conversationAllows(&p.State, t.ConversationID, t.PeerID) {
			denied = errors.New("群文件权限已失效")
			return nil
		}
		if p.fileMessageRecalled(t) {
			denied = errors.New("文件消息已撤回")
			return nil
		}
		if f.SourcePath == "" {
			denied = errors.New("源文件不可用")
			return nil
		}
		if reuseID != "" && (t.Revoked || t.Status != "failed" && t.Status != "waiting") {
			return nil
		}
		return []pending{appendFileOffer(&p.State, t.PeerID, t.ConversationID, f, reuseID, t.FromShare)}
	})
	return errors.Join(err, denied)
}

func appendFileOffer(s *domain.State, peer, convID string, f domain.File, tid string, fromShare bool) pending {
	if tid == "" {
		tid = newID()
	}
	t := domain.Transfer{ID: tid, FileID: f.ID, ConversationID: convID, PeerID: peer, Direction: "send", Status: "offered", Name: f.Name, Size: f.Size, Path: f.SourcePath, UpdatedAt: time.Now(), FromShare: fromShare}
	p := s.Peer(peer)
	if p.Transport == legacyIPMSG {
		t.LegacyTransport = legacyIPMSG
	} else if p.Transport == domain.TransportNeiwangtong {
		t.LegacyTransport = domain.TransportNeiwangtong
	}
	if i := transferIndex(s, tid); i < 0 {
		s.Transfers = append(s.Transfers, t)
	} else {
		s.Transfers[i].Status = "offered"
		s.Transfers[i].Error = ""
		s.Transfers[i].UpdatedAt = t.UpdatedAt
	}
	f.ConversationID = convID
	return pending{PeerID: peer, Envelope: envelope{Type: "file_offer", File: publicFile(f), TransferID: tid}}
}
func validTransferID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && id == strings.ToLower(id)
}

func (n *Node) receiveOffer(peer string, e envelope) error {
	f := e.File
	f.SourcePath = ""
	if !validFile(f) || f.SenderID != peer || !validTransferID(e.TransferID) {
		return errors.New("无效文件邀请")
	}
	isPrint := e.PrintJob.ID != ""
	if f.InlineImage && (f.Shared || !validInlineImage(f)) {
		return errors.New("无效聊天图片")
	}
	requested, suppressed, autoReceive := false, false, false
	authRejected := false
	var denied error
	err := n.durable(func(p *persisted) []pending {
		s := &p.State
		if p.TransferTombstones[peer+":"+e.TransferID] != "" {
			suppressed = true
			return nil
		}
		if old := s.File(f.ID); old.ID != "" && old.SenderID != peer {
			denied = errors.New("文件身份冲突")
			return nil
		}
		if !conversationAllows(s, f.ConversationID, peer) {
			denied = errors.New("群文件权限无效")
			return nil
		}
		if f.Shared && !isPrint && transferIndex(s, e.TransferID) < 0 {
			i := pendingShareRequest(s, peer, f.ID, e.ShareRequestID)
			if i >= 0 || e.ShareRequestID != "" {
				catalog := s.File(f.ID)
				if i >= 0 && s.Transfers[i].Status == "requesting" && catalog.Shared && catalog.Hash == f.Hash && catalog.Size == f.Size && catalog.Name == f.Name {
					// Older senders do not echo the local request ID.
					e.ShareRequestID = s.Transfers[i].ID
					s.Transfers = append(s.Transfers[:i], s.Transfers[i+1:]...)
					autoReceive = true
				} else {
					if i >= 0 && s.Transfers[i].Status == "requesting" {
						updateTransferProgress(&s.Transfers[i], "failed", 0, 0, "分享已失效")
					}
					suppressed = true
					p.TransferTombstones[peer+":"+e.TransferID] = "rejected"
					return []pending{{PeerID: peer, Envelope: envelope{Type: "file_status", TransferID: e.TransferID, Status: "rejected"}}}
				}
			}
		}
		var printJob domain.PrintJob
		if isPrint {
			var printErr error
			storageRejected := false
			printJob, autoReceive, printErr = n.receivePrintOffer(peer, p, f, e)
			if printErr == nil && transferIndex(s, e.TransferID) < 0 && pendingPrintBytes(s)+f.Size > maxPendingPrintBytes {
				printErr = errors.New("本机打印接收缓存已达上限，请稍后重试")
			}
			if printErr == nil {
				if err := ensurePrintDirectory(printReceivePath(n.dir, printJob)); err != nil {
					printErr = errors.New("无法创建打印接收目录，请联系打印机共享人")
					storageRejected = true
				}
			}
			if printErr != nil {
				authRejected = errors.Is(printErr, errPrinterAuthorization)
				if storageRejected || printOfferRejected(printErr) {
					suppressed = true
					p.TransferTombstones[peer+":"+e.TransferID] = "rejected"
					return []pending{{PeerID: peer, Envelope: envelope{
						Type: "file_status", TransferID: e.TransferID, Status: "rejected", Error: printErr.Error(),
					}}}
				}
				denied = printErr
				return nil
			}
			if transferIndex(s, e.TransferID) < 0 && printJobIndex(s, printJob.ID) >= 0 {
				denied = errors.New("打印任务身份冲突")
				return nil
			}
			f.ConversationID = printJob.ConversationID
		}
		if !strings.HasPrefix(f.ConversationID, "group:") {
			f.ConversationID = directID(s.Self.ID, peer)
		}
		if p.MessageRecalls[messageRecallKey(peer, f.ConversationID, f.ID)] {
			suppressed = true
			return nil
		}
		if !strings.HasPrefix(f.ConversationID, "group:") {
			ensureDirect(s, s.Peer(peer))
		}
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
			if !isPrint && f.AutoReceiveVideo(s.Settings) {
				autoReceive = true
			}
			status := "offered"
			if f.InlineImage || isPrint || autoReceive {
				status = "waiting"
			}
			transfer := domain.Transfer{ID: e.TransferID, FileID: f.ID, ConversationID: f.ConversationID, PeerID: peer,
				Direction: "receive", Status: status, FromShare: f.Shared, ShareRequestID: e.ShareRequestID, Name: f.Name, Size: f.Size, UpdatedAt: time.Now()}
			kind := "file"
			if f.InlineImage {
				kind = "image"
			}
			if isPrint {
				kind = "print"
				transfer.PrintJobID = printJob.ID
				transfer.Path = printReceivePath(n.dir, printJob)
			}
			s.Transfers = append(s.Transfers, transfer)
			found := false
			for _, m := range s.Messages {
				if m.ID == f.ID || isPrint && m.ID == printJob.MessageID {
					found = true
					break
				}
			}
			if !found {
				message := domain.Message{ID: f.ID, ConversationID: f.ConversationID, SenderID: peer,
					Kind: kind, Text: f.Name, FileID: f.ID, Status: "delivered", CreatedAt: f.CreatedAt}
				if isPrint {
					message.ID = printJob.MessageID
					message.PrintJobID = printJob.ID
					message.Text = "打印 " + f.Name
					message.CreatedAt = printJob.CreatedAt
				}
				addMessageState(s, message)
				if i := convIndex(s, f.ConversationID); i >= 0 {
					if isPrint {
						s.Conversations[i].Preview = "打印 " + f.Name
					} else {
						s.Conversations[i].Preview = attachmentPreview(f)
					}
					s.Conversations[i].UpdatedAt = f.CreatedAt
				}
			}
			if isPrint {
				s.PrintJobs = append(s.PrintJobs, printJob)
			}
		}
		return nil
	})
	if err != nil || denied != nil {
		return errors.Join(err, denied)
	}
	if authRejected {
		n.sendPrinterCatalog(peer)
	}
	if !suppressed && (f.InlineImage || requested || autoReceive) {
		n.startReceive(e.TransferID, "")
	}
	return nil
}

func printOfferRejected(err error) bool {
	message := err.Error()
	return errors.Is(err, errPrinterAuthorization) || strings.Contains(message, "打印机已停止共享") ||
		strings.Contains(message, "共享打印机当前不可用") ||
		strings.Contains(message, "打印机不支持") ||
		strings.Contains(message, "打印接收缓存已达上限")
}

func pendingPrintBytes(s *domain.State) int64 {
	var total int64
	for _, transfer := range s.Transfers {
		if transfer.PrintJobID == "" || transfer.Direction != "receive" {
			continue
		}
		job := s.PrintJob(transfer.PrintJobID)
		if job.Terminal() || transfer.Status == "cancelled" || transfer.Status == "rejected" || job.ID == "" && terminalTransfer(transfer.Status) {
			continue
		}
		total += transfer.Size
	}
	return total
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
			updateTransferProgress(t, status, done, speed, problem)
		}
	})
}

// Compatibility transports report byte offsets without a rate.
func updateTransferProgress(t *domain.Transfer, status string, done int64, rate float64, problem string) {
	// The data socket and the peer's acknowledgement can report the same
	// offset. Keep the timestamp paired with the last byte advance.
	if status == "transferring" && t.Status == status && done == t.Done && rate == 0 && problem == t.Error {
		return
	}
	now := time.Now()
	if status != "transferring" {
		rate = 0
	} else if rate == 0 && t.Status == status && done > t.Done && !t.UpdatedAt.IsZero() {
		if elapsed := now.Sub(t.UpdatedAt); elapsed > 0 {
			rate = float64(done-t.Done) / elapsed.Seconds()
		}
	}
	t.Status, t.Done, t.Speed, t.Error, t.UpdatedAt = status, done, rate, problem, now
}

func (n *Node) transferContext(id string) (context.Context, func(), bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.transfers[id]; ok || n.closing {
		return nil, nil, false
	}
	_, recovering := n.data.ReceivePublications[id]
	if index := transferIndex(&n.data.State, id); !recovering && index >= 0 && n.data.fileMessageRecalled(n.data.State.Transfers[index]) {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(n.ctx)
	n.transfers[id] = cancel
	return ctx, func() {
		cancel()
		n.mu.Lock()
		delete(n.transfers, id)
		n.mu.Unlock()
		n.kickCompatibility()
	}, true
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
	n.mu.Lock()
	i := transferIndex(&n.data.State, id)
	_, recovery := n.data.ReceivePublications[id]
	if i >= 0 && !recovery && (n.data.State.Transfers[i].ShareProject != "" || n.data.State.Transfers[i].ShareRequest) {
		n.mu.Unlock()
		return
	}
	n.mu.Unlock()
	ctx, finish, ok := n.transferContext(id)
	if !ok {
		return
	}
	if !n.async(func() {
		defer func() {
			finish()
			n.removeFinishedGroupFolderStage(id)
		}()
		if err := n.download(ctx, id, destination); err != nil {
			if ctx.Err() != nil {
				return
			}
			if _, pending := n.receivePublication(id); pending {
				return
			}
			s := n.snapshot(false)
			if i := transferIndex(&s, id); i >= 0 {
				t := s.Transfers[i]
				if terminalTransfer(t.Status) {
					return
				}
				status := "failed"
				if !s.Peer(t.PeerID).Online {
					status = "waiting"
				}
				n.setTransfer(id, status, t.Done, 0, err.Error())
				if status == "failed" && t.PrintJobID != "" {
					n.failPrintTransfer(id, err.Error())
				}
				n.reliable(t.PeerID, envelope{Type: "file_status", TransferID: id, Status: status})
			}
		}
	}) {
		finish()
	}
}
func (n *Node) download(ctx context.Context, id, destination string) error {
	if publication, pending := n.receivePublication(id); pending {
		return n.recoverReceivePublication(ctx, id, publication)
	}
	s := n.snapshot(false)
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
	if terminalTransfer(t.Status) || !conversationAllows(&s, t.ConversationID, t.PeerID) {
		return context.Canceled
	}
	if t.LegacyTransport == legacyIPMSG {
		return n.downloadLegacy(ctx, t, s.File(t.FileID), destination)
	}
	if t.LegacyTransport == domain.TransportNeiwangtong {
		return n.downloadNeiwangtong(ctx, t, s.File(t.FileID), destination)
	}
	f := s.File(t.FileID)
	if !validFile(f) {
		return errors.New("文件信息不完整")
	}
	p := s.Peer(t.PeerID)
	if destination == "" {
		destination = t.Path
	}
	if destination == "" && f.InlineImage {
		destination = filepath.Join(n.dir, "chat-images", f.ID+imageSuffix(f.MIME))
	}
	if destination != "" {
		if info, e := os.Stat(destination); e == nil && info.Mode().IsRegular() && info.Size() == f.Size {
			if hash, e := fileHashContext(ctx, destination); e == nil && hash == f.Hash {
				if err := n.completeReceive(t, destination); err != nil {
					return err
				}
				n.startPrintIfReady(id)
				return nil
			}
		}
	}
	if !p.Online {
		n.setTransfer(id, "waiting", t.Done, 0, "")
		return nil
	}
	if destination == "" {
		var err error
		if t.ShareRequestID != "" && t.FromShare {
			destination, err = n.shareDestination(ctx, t, s.Settings.DownloadDir)
		} else {
			destination, err = receiveDestination(s.Settings.DownloadDir, f.Name, s.Settings.DuplicatePolicy)
		}
		if err != nil {
			return err
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
	if err := n.beginReceive(ctx, id, destination, offset); err != nil {
		return err
	}
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
	if f.InlineImage && inlineImageType(partial, f.Size) != f.MIME {
		_ = file.Truncate(0)
		return errors.New("发送内容不是受支持的聊天图片")
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return n.publishReceivedFile(ctx, id, partial, destination, f.Size, f.Hash)
}
func (n *Node) serveFile(conn *tls.Conn, e envelope, peer string) {
	s := n.snapshot(false)
	i := transferIndex(&s, e.TransferID)
	if i < 0 {
		return
	}
	t := s.Transfers[i]
	f := s.File(e.File.ID)
	if t.Direction != "send" || t.PeerID != peer || t.FileID != f.ID || f.SenderID != s.Self.ID || e.Offset < 0 || e.Offset > f.Size || t.Status == "cancelled" || t.Status == "rejected" || t.Revoked || t.FromShare && (!f.Shared || f.ShareProtected) {
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
	current := n.snapshot(false)
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
			now := n.snapshot(false)
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
	n.mu.Lock()
	connection := n.connections[peer]
	if n.closing || connection == nil {
		n.mu.Unlock()
		return
	}
	catalog := n.shareCatalog()
	if catalog.MemberCatalog != nil {
		catalog.MemberCatalog.Port = n.memberSharePort(connection.conn.LocalAddr().(*net.TCPAddr))
	}
	n.mu.Unlock()
	if err := connection.send(catalog); err != nil {
		_ = connection.conn.Close()
	}
}
func (n *Node) broadcastCatalog() {
	n.notifyBrowserCatalog()
	s := n.snapshot(false)
	for _, p := range s.Peers {
		if p.Online {
			n.sendCatalog(p.ID)
		}
	}
}
func (n *Node) offerShared(peer, id, requestID string) error {
	if requestID != "" && !validTransferID(requestID) {
		return errors.New("无效的共享请求")
	}
	granted := false
	err := n.durable(func(p *persisted) []pending {
		f := p.State.File(id)
		if !publicSharedFile(p, f) {
			if requestID != "" {
				return []pending{{PeerID: peer, Envelope: envelope{Type: "file_status", TransferID: requestID, Status: "rejected", Error: "分享已失效"}}}
			}
			return nil
		}
		granted = true
		if requestID != "" {
			for _, t := range p.State.Transfers {
				if t.Direction == "send" && t.PeerID == peer && t.FromShare && t.ShareRequestID == requestID {
					return nil
				}
			}
		}
		conv := ensureDirect(&p.State, p.State.Peer(peer))
		d := appendFileOffer(&p.State, peer, conv, f, "", true)
		d.Envelope.ShareRequestID = requestID
		if requestID != "" {
			i := transferIndex(&p.State, d.Envelope.TransferID)
			p.State.Transfers[i].ShareRequestID = requestID
		}
		return []pending{d}
	})
	if err == nil && !granted && requestID == "" {
		return errors.New("此分享已不可用")
	}
	return err
}

// receiveDestination chooses the first available name. Publication still checks
// the final path, because another transfer can finish while this one is running.
func receiveDestination(dir, name, policy string) (string, error) {
	return availableReceiveDestination(dir, name, policy, nil)
}

func availableReceiveDestination(dir, name, policy string, reserved map[string]bool) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for index := 0; index < 10000; index++ {
		candidate := name
		if index > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", base, index, ext)
		}
		path := filepath.Join(dir, candidate)
		if reserved[strings.ToLower(path)] {
			continue
		}
		_, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		if policy == "ask" {
			return "", errors.New("同名文件存在，请选择另存为")
		}
	}
	return "", errors.New("接收目录中的同名文件过多")
}
