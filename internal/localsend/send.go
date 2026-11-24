package localsend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"goim/internal/domain"
)

type source struct {
	fileInfo
	path string
	text []byte
}

func fileType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".avif", ".heic", ".heif", ".tiff":
		return "image"
	case ".mp4", ".webm", ".mkv", ".avi", ".mov", ".m4v":
		return "video"
	case ".pdf":
		return "pdf"
	case ".txt":
		return "text"
	case ".apk":
		return "apk"
	default:
		return "other"
	}
}

func sources(paths []string, text string) ([]source, error) {
	if text != "" {
		if len(paths) != 0 || len(text) > maxText {
			return nil, errors.New("一次只能发送文字或文件，文字最长 128 KiB")
		}
		f := source{fileInfo: fileInfo{ID: newID(), Name: "message.txt", Size: int64(len(text)), Type: "text"}, text: []byte(text)}
		if len(text) <= 16<<10 {
			f.Preview = &text
		}
		return []source{f}, nil
	}
	var list []source
	for _, sourcePath := range paths {
		root, err := filepath.Abs(sourcePath)
		if err != nil {
			return nil, err
		}
		base := filepath.Base(root)
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return errors.New("LocalSend 仅支持普通文件和文件夹")
			}
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			name := filepath.ToSlash(filepath.Join(base, rel))
			if !validName(name) {
				return fmt.Errorf("文件名不兼容：%s", name)
			}
			stat, e := d.Info()
			if e != nil {
				return e
			}
			if len(list) >= maxItems {
				return errors.New("单次最多发送 5000 个文件")
			}
			list = append(list, source{fileInfo: fileInfo{ID: newID(), Name: name, Size: stat.Size(), Type: fileType(name),
				Metadata: &fileMetadata{Modified: stat.ModTime().UTC().Format(time.RFC3339Nano)}}, path: path})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(list) == 0 {
		return nil, errors.New("请选择文件。空文件夹不会出现在 LocalSend 传输中。")
	}
	if total := totalSources(list); total < 0 {
		return nil, errors.New("文件总大小超出限制")
	}
	return list, nil
}

func totalSources(files []source) int64 {
	var size int64
	for _, f := range files {
		if f.Size > math.MaxInt64-size {
			return -1
		}
		size += f.Size
	}
	return size
}

func (f source) open() (io.ReadCloser, error) {
	if f.path == "" {
		return io.NopCloser(bytes.NewReader(f.text)), nil
	}
	raw, err := os.Open(f.path)
	if err != nil {
		return nil, err
	}
	fi, err := raw.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != f.Size || fi.ModTime().UTC().Format(time.RFC3339Nano) != f.Metadata.Modified {
		_ = raw.Close()
		return nil, errors.New("源文件已修改，请重新选择")
	}
	return raw, nil
}

type outgoing struct {
	device    seenDevice
	remaining []source
	allowHTTP bool
	stop      context.CancelFunc
}

func (s *Service) Send(ids, paths []string, text, pin string, allowHTTP bool) error {
	if len(ids) == 0 || len(ids) > 32 {
		return errors.New("请选择 1 到 32 台接收设备")
	}
	files, err := sources(paths, text)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return context.Canceled
	}
	devices := make([]seenDevice, 0, len(ids))
	unique := make(map[string]bool)
	for _, id := range ids {
		if unique[id] {
			continue
		}
		unique[id] = true
		device, ok := s.peers[id]
		if !ok || device.Changed {
			s.mu.Unlock()
			return errors.New("设备已离线或证书已改变，请重新确认设备")
		}
		if device.Plain && !allowHTTP {
			s.mu.Unlock()
			return errors.New("所选设备使用 HTTP，请确认允许明文传输")
		}
		devices = append(devices, device)
	}
	if len(s.jobs)+len(devices) > 128 {
		for key, job := range s.jobs {
			if job.stop == nil {
				delete(s.jobs, key)
			}
		}
	}
	if len(s.jobs)+len(devices) > 128 {
		s.mu.Unlock()
		return errors.New("LocalSend 发送任务过多")
	}
	for _, device := range devices {
		job := &outgoing{device: device, remaining: files, allowHTTP: allowHTTP}
		s.launchLocked("ls-"+newID(), job, pin)
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) launchLocked(id string, job *outgoing, pin string) {
	ctx, stop := context.WithCancel(s.ctx)
	job.stop = stop
	s.jobs[id] = job
	s.wg.Go(func() {
		defer stop()
		s.runSend(ctx, id, job, pin)
	})
}

func (s *Service) Retry(id, pin string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return context.Canceled
	}
	job := s.jobs[id]
	if job == nil {
		return errors.New("请重新选择文件并发起发送")
	}
	if job.stop != nil || len(job.remaining) == 0 {
		return errors.New("该任务没有可重试的文件")
	}
	peer, ok := s.peers[job.device.ID]
	if !ok || peer.Changed {
		return errors.New("设备已离线或证书已改变，请重新确认设备")
	}
	job.device = peer
	s.launchLocked(id, job, pin)
	return nil
}

func (s *Service) Cancel(id string) bool {
	s.mu.Lock()
	if job := s.jobs[id]; job != nil && job.stop != nil {
		job.stop()
		s.mu.Unlock()
		return true
	}
	if in := s.incoming; in != nil && in.id == id {
		in.cancel()
		s.mu.Unlock()
		return true
	}
	s.mu.Unlock()
	return false
}

func (s *Service) runSend(ctx context.Context, id string, job *outgoing, pin string) {
	device := job.device
	files := job.remaining
	remaining := files
	row := domain.Transfer{ID: id, Name: transferName(files), Size: totalSources(files), Direction: "send", Status: "offered",
		LegacyTransport: domain.TransportLocalSend, LocalSendPeer: device.Name}
	for _, f := range files {
		row.LocalSendItems = append(row.LocalSendItems, domain.LocalSendTransferItem{ID: f.ID, Name: f.Name, Size: f.Size, Status: "waiting"})
	}
	s.update(row)
	sessionID := ""
	scheme := "https"
	if device.Plain {
		scheme = "http"
	}
	client := s.client(device.fingerprint, nil, approvalTime+10*time.Second)
	defer client.CloseIdleConnections()
	defer func() {
		if ctx.Err() != nil && row.Status != "completed" {
			row.Status, row.Error = "cancelled", ""
			cancelCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			query := url.Values{}
			if sessionID != "" {
				query.Set("sessionId", sessionID)
			}
			if res, err := request(cancelCtx, client, http.MethodPost, targetURL(scheme, device.Address, "cancel", query), nil, "", 0); err == nil {
				_ = res.Body.Close()
			}
		}
		row.Speed = 0
		row.LocalSendCanRetry = len(remaining) > 0
		for i := range row.LocalSendItems {
			if row.LocalSendItems[i].Status == "waiting" {
				row.LocalSendItems[i].Status = row.Status
			}
		}
		s.update(row)
		s.mu.Lock()
		job.stop = nil
		job.remaining = remaining
		if len(remaining) == 0 {
			delete(s.jobs, id)
		}
		s.mu.Unlock()
	}()

	if !s.originAllowed(endpointHost(device.Address)) {
		row.Status, row.Error = "failed", "设备已不在所选局域网接口上"
		return
	}
	payload := prepare{Info: s.selfInfo(), Files: make(map[string]fileInfo, len(files))}
	for _, f := range files {
		payload.Files[f.ID] = f.fileInfo
	}
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > maxMetadata {
		row.Status, row.Error = "failed", "文件清单过大"
		return
	}
	query := url.Values{}
	if pin != "" {
		query.Set("pin", pin)
	}
	res, err := request(ctx, client, http.MethodPost, targetURL(scheme, device.Address, "prepare-upload", query), bytes.NewReader(raw), "application/json", int64(len(raw)))
	if err != nil {
		row.Status, row.Error = "failed", networkError(err)
		return
	}
	switch res.StatusCode {
	case http.StatusNoContent:
		_ = res.Body.Close()
		if len(files) == 1 && files[0].Preview != nil {
			row.Status, row.Done = "completed", row.Size
			remaining = nil
		} else {
			row.Status, row.Error = "rejected", "对方未选择接收文件"
		}
		return
	case http.StatusUnauthorized:
		_ = res.Body.Close()
		row.Status, row.Error = "failed", transferError(http.StatusUnauthorized).Error()
		row.LocalSendNeedsPIN = true
		return
	case http.StatusForbidden:
		_ = res.Body.Close()
		row.Status, row.Error = "rejected", transferError(http.StatusForbidden).Error()
		return
	}
	var grant uploadGrant
	if err := readResponse(res, &grant); err != nil {
		row.Status, row.Error = "failed", networkError(err)
		return
	}
	if grant.SessionID == "" || len(grant.Files) == 0 {
		row.Status, row.Error = "rejected", "对方未选择接收文件"
		return
	}
	sessionID = grant.SessionID
	row.Status = "transferring"
	s.update(row)
	var failed []source
	start := time.Now()
	accepted, completed := 0, 0
	for index, f := range files {
		token, ok := grant.Files[f.ID]
		if !ok || token == "" {
			row.LocalSendItems[index].Status = "rejected"
			failed = append(failed, f)
			continue
		}
		accepted++
		if err := s.sendFile(ctx, client, device.Address, scheme, grant.SessionID, token, f, id, row.Done, start); err != nil {
			row.Error = networkError(err)
			row.LocalSendItems[index].Status = "failed"
			failed = append(failed, f)
			continue
		}
		row.Done += f.Size
		row.LocalSendItems[index].Status = "completed"
		completed++
	}
	remaining = failed
	switch {
	case len(failed) == 0:
		row.Status = "completed"
	case completed > 0:
		row.Status = "partial"
	case accepted > 0 && row.Error != "":
		row.Status = "failed"
	default:
		row.Status = "rejected"
	}
}

func networkError(err error) string {
	var e *url.Error
	if errors.As(err, &e) {
		return e.Err.Error()
	}
	return err.Error()
}

func endpointHost(endpoint string) string {
	host, _, _ := net.SplitHostPort(endpoint)
	return host
}

func (s *Service) sendFile(ctx context.Context, client *http.Client, address, scheme, session, token string, f source, id string, offset int64, start time.Time) error {
	raw, err := f.open()
	if err != nil {
		return err
	}
	defer raw.Close()
	query := url.Values{"sessionId": {session}, "fileId": {f.ID}, "token": {token}}
	progress := newProgress(func(n int64) { s.progress(id, offset+n, float64(offset+n)/time.Since(start).Seconds()) })
	var body io.Reader
	if f.Size != 0 {
		body = io.TeeReader(raw, progress)
	}
	res, err := request(ctx, client, http.MethodPost, targetURL(scheme, address, "upload", query), body, "application/octet-stream", f.Size)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		return transferError(res.StatusCode)
	}
	return nil
}

func transferName(files []source) string {
	if len(files) == 1 {
		if files[0].Preview != nil {
			return "文字消息"
		}
		return files[0].Name
	}
	return fmt.Sprintf("%s 等 %d 个文件", files[0].Name, len(files))
}

type progressWriter struct {
	mu      sync.Mutex
	total   int64
	last    time.Time
	publish func(int64)
}

func newProgress(publish func(int64)) *progressWriter {
	return &progressWriter{last: time.Now(), publish: publish}
}
func (p *progressWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total += int64(len(b))
	if time.Since(p.last) >= 250*time.Millisecond {
		p.last = time.Now()
		p.publish(p.total)
	}
	return len(b), nil
}

func copyVerified(ctx context.Context, dest io.Writer, src io.Reader, f fileInfo, p *progressWriter) error {
	hash := sha256.New()
	reader := &io.LimitedReader{R: src, N: f.Size}
	n, err := io.Copy(io.MultiWriter(dest, hash, p), reader)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if n != f.Size {
		return io.ErrUnexpectedEOF
	}
	var extra [1]byte
	if n, err := io.ReadFull(src, extra[:]); n > 0 || err != io.EOF {
		return errSize
	}
	if f.SHA != "" && !strings.EqualFold(f.SHA, hex.EncodeToString(hash.Sum(nil))) {
		return errHash
	}
	return nil
}

var errSize = errors.New("接收数据长度和文件清单不一致")
var errHash = errors.New("接收文件的 SHA-256 校验失败")
