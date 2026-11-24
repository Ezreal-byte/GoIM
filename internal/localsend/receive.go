package localsend

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"goim/internal/domain"
)

type incoming struct {
	id, ip, sender, cert string
	link                 *webLink
	files                map[string]*incomingFile
	order                []string
	text                 *string
	answer               chan []string
	ctx                  context.Context
	cancel               context.CancelFunc
	approved             bool
	directory            string
	start                time.Time
	progress             *progressWriter
	row                  domain.Transfer
}

type incomingFile struct {
	fileInfo
	token         string
	running, done bool
	path, problem string
}

func (in *incoming) offer() domain.LocalSendOffer {
	o := domain.LocalSendOffer{ID: in.id, From: in.sender}
	if in.text != nil {
		o.Text, o.IsText = *in.text, true
	}
	for _, id := range in.order {
		f := in.files[id]
		o.Files = append(o.Files, domain.LocalSendFile{ID: f.ID, Name: f.Name, Size: f.Size})
	}
	return o
}

func (s *Service) allowedReceive() bool {
	return s.options.AllowReceive == nil || s.options.AllowReceive()
}

func (s *Service) prepareUpload(w http.ResponseWriter, r *http.Request, link *webLink) {
	if !s.allowedReceive() {
		fail(w, http.StatusForbidden, "Receiver locked")
		return
	}
	if !s.checkPIN(w, r, link) {
		return
	}
	var payload prepare
	if !decode(w, r, &payload) {
		return
	}
	if len(payload.Files) == 0 || len(payload.Files) > maxItems {
		fail(w, 400, "Invalid file count")
		return
	}
	if !validInfo(payload.Info) {
		// Stock browser clients may advertise port zero since they do not listen.
		if len(payload.Info.Alias) > 256 || link == nil {
			fail(w, 400, "Invalid sender")
			return
		}
	}
	cert := certFingerprint(r)
	if cert != "" && payload.Info.Protocol == "https" && !strings.EqualFold(cert, payload.Info.Fingerprint) {
		fail(w, 400, "Invalid sender certificate")
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	ip := remoteIP(r)
	sender := strings.TrimSpace(payload.Info.Alias)
	if sender == "" {
		sender = ip
	}
	in := &incoming{id: "ls-" + newID(), ip: ip, sender: sender, cert: cert, link: link, ctx: ctx, cancel: cancel, answer: make(chan []string, 1), files: make(map[string]*incomingFile)}
	var total int64
	for id, f := range payload.Files {
		if len(id) > 128 || id == "" || id != f.ID || !validFile(f) || math.MaxInt64-total < f.Size {
			cancel()
			fail(w, 400, "Invalid file metadata")
			return
		}
		total += f.Size
		in.order = append(in.order, id)
		in.files[id] = &incomingFile{fileInfo: f}
		if len(payload.Files) == 1 && (f.Type == "text" || f.Type == "text/plain") && f.Size <= 16<<10 &&
			f.Preview != nil && len(*f.Preview) <= 16<<10 {
			in.text = f.Preview
		}
	}
	sort.Slice(in.order, func(i, j int) bool {
		a, b := in.files[in.order[i]], in.files[in.order[j]]
		if a.Name == b.Name {
			return a.ID < b.ID
		}
		return a.Name < b.Name
	})
	name := in.files[in.order[0]].Name
	if len(in.order) > 1 {
		name = fmt.Sprintf("%s 等 %d 个文件", name, len(in.order))
	}
	if in.text != nil {
		name = "文字消息"
	}
	in.row = domain.Transfer{ID: in.id, Name: name, Size: total, Direction: "receive", Status: "offered", LegacyTransport: domain.TransportLocalSend, LocalSendPeer: in.sender}
	s.mu.Lock()
	if s.closed || link != nil && s.link != link || s.incoming != nil {
		s.mu.Unlock()
		cancel()
		fail(w, http.StatusConflict, "Blocked by another session")
		return
	}
	s.incoming = in
	s.mu.Unlock()
	s.update(in.row)
	if link == nil && cert != "" && validInfo(payload.Info) && payload.Info.Protocol == "https" {
		s.remember(ip, payload.Info, true, false)
	}
	approval := time.NewTimer(approvalTime)
	defer approval.Stop()
	var ids []string
	select {
	case <-r.Context().Done():
		s.finishIncoming(in, "cancelled", "")
		return
	case <-ctx.Done():
		s.finishIncoming(in, "cancelled", "")
		fail(w, 403, "Cancelled")
		return
	case <-approval.C:
		s.finishIncoming(in, "rejected", "接收请求已超时")
		fail(w, 403, "Approval timed out")
		return
	case ids = <-in.answer:
	}
	if len(ids) == 0 || !s.allowedReceive() {
		s.finishIncoming(in, "rejected", "")
		fail(w, http.StatusForbidden, "Rejected")
		return
	}
	if in.text != nil {
		s.finishIncoming(in, "completed", "")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	dir := ""
	if s.options.DownloadDir != nil {
		dir = s.options.DownloadDir()
	}
	if dir == "" {
		s.finishIncoming(in, "failed", "接收目录未设置")
		fail(w, 500, "Receiver directory unavailable")
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		s.finishIncoming(in, "failed", err.Error())
		fail(w, 500, "Receiver directory unavailable")
		return
	}
	grant := uploadGrant{SessionID: in.id, Files: make(map[string]string)}
	s.mu.Lock()
	if s.incoming != in || ctx.Err() != nil || r.Context().Err() != nil {
		s.mu.Unlock()
		s.finishIncoming(in, "cancelled", "")
		return
	}
	in.approved = true
	in.directory = dir
	in.start = time.Now()
	start := in.start
	in.progress = newProgress(func(count int64) { s.progress(in.id, count, float64(count)/time.Since(start).Seconds()) })
	in.row.Size = 0
	for _, id := range ids {
		if f := in.files[id]; f != nil && f.token == "" {
			f.token = newID()
			grant.Files[id] = f.token
			in.row.Size += f.Size
		}
	}
	in.row.Status = "transferring"
	row := in.row
	s.mu.Unlock()
	s.update(row)
	reply(w, grant)
	if !s.spawn(func() { s.watchIncoming(in) }) {
		s.finishIncoming(in, "cancelled", "")
	}
}

func (s *Service) Answer(id string, ids []string, allow bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	in := s.incoming
	if in == nil || in.id != id || in.approved || in.ctx.Err() != nil {
		return errors.New("接收请求已结束")
	}
	if !allow {
		ids = nil
	} else if in.text != nil {
		ids = []string{in.order[0]}
	} else {
		if len(ids) == 0 {
			return errors.New("请至少选择一个文件")
		}
		for _, id := range ids {
			if in.files[id] == nil {
				return errors.New("接收清单已变化")
			}
		}
	}
	select {
	case in.answer <- ids:
		return nil
	default:
		return errors.New("接收请求已处理")
	}
}

func (s *Service) watchIncoming(in *incoming) {
	timer := time.NewTicker(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-in.ctx.Done():
			s.finishIncoming(in, "cancelled", "")
			return
		case <-timer.C:
			s.mu.Lock()
			busy := false
			for _, f := range in.files {
				busy = busy || f.running
			}
			expired := !busy && time.Since(in.start) > 2*time.Minute
			s.mu.Unlock()
			if expired {
				s.finishIncoming(in, "failed", "等待文件超时")
				return
			}
		}
	}
}

func (in *incoming) matches(r *http.Request, link *webLink) bool {
	return in.link == link && in.ip == remoteIP(r) && in.cert == certFingerprint(r)
}

func (s *Service) uploadFile(w http.ResponseWriter, r *http.Request, link *webLink) {
	q := r.URL.Query()
	s.mu.Lock()
	in := s.incoming
	if in == nil || !in.approved || in.id != q.Get("sessionId") || !in.matches(r, link) {
		s.mu.Unlock()
		fail(w, 403, "Invalid session")
		return
	}
	f := in.files[q.Get("fileId")]
	if f == nil || f.token == "" || subtle.ConstantTimeCompare([]byte(f.token), []byte(q.Get("token"))) != 1 {
		s.mu.Unlock()
		fail(w, 403, "Invalid file token")
		return
	}
	if f.running || f.done {
		s.mu.Unlock()
		fail(w, 409, "File already uploaded")
		return
	}
	f.running = true
	s.mu.Unlock()

	rc := http.NewResponseController(w)
	reader := &idleReader{r: r.Body, update: rc.SetReadDeadline}
	ctx, cancel := context.WithCancel(in.ctx)
	defer cancel()
	closeBody := context.AfterFunc(ctx, func() { _ = rc.SetReadDeadline(time.Now()) })
	defer closeBody()
	path, err := publishFile(ctx, in.directory, f.fileInfo, reader, in.progress)
	s.mu.Lock()
	f.running = false
	f.done = true
	f.path = path
	if err != nil {
		f.problem = err.Error()
	}
	in.start = time.Now()
	all := true
	for _, item := range in.files {
		if item.token != "" && !item.done {
			all = false
			break
		}
	}
	s.mu.Unlock()
	if all {
		s.finishIncoming(in, "completed", "")
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errHash) {
			status = http.StatusUnprocessableEntity
		}
		if errors.Is(err, errSize) || errors.Is(err, io.ErrUnexpectedEOF) {
			status = http.StatusBadRequest
		}
		fail(w, status, "Failed to save file")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) cancelRequest(w http.ResponseWriter, r *http.Request, link *webLink) {
	s.mu.Lock()
	in := s.incoming
	id := r.URL.Query().Get("sessionId")
	if in == nil || !in.matches(r, link) || (in.approved && id != in.id) || (!in.approved && id != "" && id != in.id) {
		s.mu.Unlock()
		fail(w, 403, "Invalid session")
		return
	}
	in.cancel()
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (s *Service) finishIncoming(in *incoming, status, problem string) {
	s.mu.Lock()
	if s.incoming != in {
		s.mu.Unlock()
		return
	}
	in.cancel()
	s.incoming = nil
	row := in.row
	row.Status, row.Error, row.Speed = status, problem, 0
	success, failure := 0, 0
	for _, fileID := range in.order {
		f := in.files[fileID]
		item := domain.LocalSendTransferItem{ID: f.ID, Name: f.Name, Size: f.Size, Status: status}
		if f.token == "" && in.approved {
			item.Status = "rejected"
		}
		if f.path != "" {
			item.Status = "completed"
			success++
			row.Done += f.Size
			if row.Path == "" {
				row.Path = f.path
			}
		}
		if f.problem != "" {
			item.Status = "failed"
			failure++
			if row.Error == "" {
				row.Error = f.problem
			}
		}
		row.LocalSendItems = append(row.LocalSendItems, item)
	}
	if success > 1 {
		row.Path = in.directory
		row.LocalSendDirectory = true
	}
	if status == "completed" {
		if in.text != nil {
			row.Done = row.Size
		}
		if failure > 0 {
			row.Status = "failed"
			if success > 0 {
				row.Status = "partial"
			}
		}
	}
	s.mu.Unlock()
	s.update(row)
}

func publishFile(ctx context.Context, dir string, f fileInfo, reader io.Reader, progress *progressWriter) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	name, err := filepath.Localize(f.Name)
	if err != nil {
		return "", err
	}
	if err = makeDirectories(root, filepath.Dir(name)); err != nil {
		return "", err
	}
	stage := filepath.Join(filepath.Dir(name), ".goim-localsend-"+newID()+".part")
	handle, err := root.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer root.Remove(stage)
	err = copyVerified(ctx, handle, reader, f, progress)
	if err == nil {
		err = handle.Sync()
	}
	if err = errors.Join(err, handle.Close()); err != nil {
		return "", err
	}
	if f.Metadata != nil {
		mod, _ := time.Parse(time.RFC3339Nano, f.Metadata.Modified)
		access, _ := time.Parse(time.RFC3339Nano, f.Metadata.Accessed)
		if !mod.IsZero() {
			if access.IsZero() {
				access = mod
			}
			_ = root.Chtimes(stage, access, mod)
		}
	}
	for i := 0; i < 10000; i++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		to := name
		if i > 0 {
			ext := filepath.Ext(name)
			to = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), i, ext)
		}
		if err := root.Link(stage, to); err == nil {
			return filepath.Join(dir, to), nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("同名文件过多")
}

func makeDirectories(root *os.Root, name string) error {
	if name == "." {
		return nil
	}
	if err := makeDirectories(root, filepath.Dir(name)); err != nil {
		return err
	}
	err := root.Mkdir(name, 0700)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("接收目录包含不可用的路径")
	}
	return nil
}

type idleReader struct {
	r      io.Reader
	update func(time.Time) error
	last   time.Time
}

func (r *idleReader) Read(p []byte) (int, error) {
	if time.Since(r.last) > 10*time.Second {
		r.last = time.Now()
		_ = r.update(r.last.Add(40 * time.Second))
	}
	return r.r.Read(p)
}
