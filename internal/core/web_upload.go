package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/domain"
)

const webApprovalWindow = 10 * time.Minute
const maxPendingWebUploads = 32

type uploadProblem struct {
	code    int
	message string
}

func (p uploadProblem) Error() string { return p.message }

var errUploadGone = uploadProblem{http.StatusGone, "上传请求已失效，请重新选择文件。"}
var errUploadApproval = uploadProblem{http.StatusForbidden, "请先在 GoIM 的传入列表中同意接收。"}
var errUploadInput = uploadProblem{http.StatusBadRequest, "文件名或大小无效。"}
var errUploadStorage = uploadProblem{http.StatusServiceUnavailable, "无法写入设备的接收目录。"}
var errUploadBusy = uploadProblem{http.StatusTooManyRequests, "当前上传请求较多，请稍后再试。"}

func webUploadError(w http.ResponseWriter, err error) {
	var problem uploadProblem
	if !errors.As(err, &problem) {
		problem = errUploadStorage
	}
	http.Error(w, problem.message, problem.code)
}

func (n *Node) setWebApproval(required bool) error {
	var issue error
	err := n.durable(func(p *persisted) []pending {
		if p.BrowserHomeToken == "" {
			issue = errUploadGone
			return nil
		}
		if required {
			n.restoreWebApproval(&p.State)
		} else if !time.Now().Before(n.data.WebAutoUntil) {
			n.restoreWebApproval(&p.State)
			n.data.WebAutoUntil = time.Now().Add(webApprovalWindow)
		}
		return nil
	})
	return errors.Join(err, issue)
}
func (n *Node) restoreWebApproval(s *domain.State) {
	n.data.WebAutoUntil = time.Time{}
	for i := range s.Transfers {
		t := &s.Transfers[i]
		if t.WebSender != "" && t.WebAuto && t.Status == "waiting" {
			t.WebAuto = false
			updateTransferProgress(t, "offered", 0, 0, "")
		}
	}
}
func (n *Node) closeWebUploads(s *domain.State) {
	for i := range s.Transfers {
		t := &s.Transfers[i]
		if t.WebSender == "" || terminalTransfer(t.Status) || t.Status == "failed" {
			continue
		}
		if cancel := n.transfers[t.ID]; cancel != nil {
			cancel()
		}
		updateTransferProgress(t, "cancelled", t.Done, 0, "设备主页已关闭")
	}
}
func expiredWebRequest(t domain.Transfer, now time.Time) bool {
	return t.WebSender != "" && (t.Status == "offered" || t.Status == "waiting") && !now.Before(t.UpdatedAt.Add(webApprovalWindow))
}
func (n *Node) expireWebUploads(now time.Time) {
	n.mu.Lock()
	if n.data.BrowserHomeToken == "" {
		n.mu.Unlock()
		return
	}
	needed := !n.data.WebAutoUntil.IsZero() && !now.Before(n.data.WebAutoUntil)
	if !needed {
		for _, t := range n.data.State.Transfers {
			if expiredWebRequest(t, now) {
				needed = true
				break
			}
		}
	}
	n.mu.Unlock()
	if !needed {
		return
	}
	n.change(func(s *domain.State) {
		for i := range s.Transfers {
			t := &s.Transfers[i]
			if expiredWebRequest(*t, now) {
				updateTransferProgress(t, "failed", 0, 0, "上传请求已过期")
			}
		}
		if !n.data.WebAutoUntil.IsZero() && !now.Before(n.data.WebAutoUntil) {
			n.restoreWebApproval(s)
		}
	})
}
func (n *Node) isWebUpload(id string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	i := transferIndex(&n.data.State, id)
	return i >= 0 && n.data.State.Transfers[i].WebSender != ""
}
func (n *Node) approveWebUpload(id string) error {
	var issue error
	err := n.change(func(s *domain.State) {
		i := transferIndex(s, id)
		if i < 0 || s.Transfers[i].Status != "offered" || expiredWebRequest(s.Transfers[i], time.Now()) {
			issue = errUploadGone
			return
		}
		t := &s.Transfers[i]
		t.WebAuto = false
		updateTransferProgress(t, "waiting", 0, 0, "")
	})
	return errors.Join(err, issue)
}

func webSender(r *http.Request) string { host, _, _ := net.SplitHostPort(r.RemoteAddr); return host }

func (n *Node) offerWebUpload(token, sender, name string, size int64, legacy bool) (domain.Transfer, error) {
	if !validFileName(name) || strings.ContainsFunc(name, func(r rune) bool { return r < 32 || r == 127 }) || strings.HasPrefix(name, ".") || sender == "" || size < 0 {
		return domain.Transfer{}, errUploadInput
	}
	if size > webUploadLimit {
		return domain.Transfer{}, uploadProblem{413, "单个文件最大 256 MB。"}
	}
	n.webUploadMu.Lock()
	defer n.webUploadMu.Unlock()
	n.mu.Lock()
	valid := n.data.BrowserHomeToken == token && token != ""
	pendingCount, fromSender := 0, 0
	for _, t := range n.data.State.Transfers {
		if t.WebSender != "" && !terminalTransfer(t.Status) && t.Status != "failed" {
			pendingCount++
			if t.WebSender == sender {
				fromSender++
			}
		}
	}
	n.mu.Unlock()
	if !valid {
		return domain.Transfer{}, errUploadGone
	}
	if pendingCount >= maxPendingWebUploads || fromSender >= 8 {
		return domain.Transfer{}, errUploadBusy
	}
	var task domain.Transfer
	var issue error
	err := n.durable(func(p *persisted) []pending {
		if p.BrowserHomeToken != token {
			issue = errUploadGone
			return nil
		}
		auto := time.Now().Before(n.data.WebAutoUntil)
		if legacy && !auto {
			issue = errUploadApproval
			return nil
		}
		task = domain.Transfer{ID: newID(), WebSender: sender, Name: name, Size: size, WebInbox: filepath.Join(p.State.Settings.DownloadDir, domain.WebReceiveFolder), WebAuto: auto, Direction: "receive", Status: "offered", UpdatedAt: time.Now()}
		if auto {
			task.Status = "waiting"
		}
		p.State.Transfers = append(p.State.Transfers, task)
		return nil
	})
	if err == nil && issue == nil && !task.WebAuto {
		n.emitShareFeedback("网页上传等待接收", name, false)
	}
	return task, errors.Join(err, issue)
}

type webTicket struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func writeWebTicket(w http.ResponseWriter, t domain.Transfer) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(webTicket{t.ID, t.Status})
}
func (n *Node) handleUploadTicket(w http.ResponseWriter, r *http.Request, token, id string) {
	w.Header().Set("Cache-Control", "no-store")
	if err := shareOrigin.Check(r); err != nil {
		http.Error(w, "来源无效", 403)
		return
	}
	sender := webSender(r)
	n.mu.Lock()
	valid := !n.closing && n.data.BrowserHomeToken == token
	var task domain.Transfer
	if valid && validTransferID(id) {
		if i := transferIndex(&n.data.State, id); i >= 0 {
			task = n.data.State.Transfers[i]
		}
	}
	n.mu.Unlock()
	if !valid {
		http.NotFound(w, r)
		return
	}
	if id == "request" {
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			w.WriteHeader(405)
			return
		}
		var req struct {
			Name string
			Size *int64
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
		if decoder.Decode(&req) != nil || req.Size == nil || decoder.Decode(&struct{}{}) != io.EOF {
			webUploadError(w, errUploadInput)
			return
		}
		task, err := n.offerWebUpload(token, sender, req.Name, *req.Size, false)
		if err != nil {
			webUploadError(w, err)
			return
		}
		writeWebTicket(w, task)
		return
	}
	if task.ID == "" || task.WebSender != sender {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeWebTicket(w, task)
	case http.MethodPut:
		if task.Status == "offered" {
			webUploadError(w, errUploadApproval)
			return
		}
		if task.Status != "waiting" {
			webUploadError(w, errUploadGone)
			return
		}
		if r.ContentLength != task.Size {
			webUploadError(w, errUploadInput)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, task.Size)
		name, err := n.saveWebUpload(w, r, token, task, r.Body, nil)
		if err != nil {
			webUploadError(w, err)
			return
		}
		n.emitShareFeedback("网页已接收文件", name, false)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(405)
	}
}

// Record the intended destination before linking. On a crash the private
// stage and final hard link identify the same file, including after a rename collision.
func recoverWebUpload(t *domain.Transfer) {
	if t.ShareStage != "" && validTransferID(t.ID) && filepath.Base(t.WebInbox) == domain.WebReceiveFolder {
		if root, err := openRealDirectory(t.WebInbox); err == nil {
			stage := ".goim-upload-" + t.ID
			if t.ShareStage == stage {
				if info, e := root.Lstat(stage); e == nil && info.Mode().IsRegular() && filepath.Dir(t.Path) == t.WebInbox {
					if dst, e := root.Lstat(filepath.Base(t.Path)); e == nil && os.SameFile(info, dst) {
						t.Status, t.Size, t.Done, t.Error = "completed", info.Size(), info.Size(), ""
					}
				}
				_ = root.Remove(stage)
			}
			root.Close()
		}
	}
	t.ShareStage = ""
	if !terminalTransfer(t.Status) && t.Status != "failed" {
		updateTransferProgress(t, "failed", t.Done, 0, "网页上传已中断，请从网页重新上传")
	}
}

func (n *Node) claimWebUpload(token string, task domain.Transfer) error {
	stage := ".goim-upload-" + task.ID
	var issue error
	err := n.durable(func(p *persisted) []pending {
		i := transferIndex(&p.State, task.ID)
		if i < 0 || token != p.BrowserHomeToken || filepath.Join(p.State.Settings.DownloadDir, domain.WebReceiveFolder) != task.WebInbox {
			issue = errUploadGone
			return nil
		}
		t := &p.State.Transfers[i]
		if t.Status == "offered" {
			issue = errUploadApproval
			return nil
		}
		if t.Status != "waiting" || expiredWebRequest(*t, time.Now()) {
			issue = errUploadGone
			return nil
		}
		if t.WebAuto && !time.Now().Before(n.data.WebAutoUntil) {
			n.restoreWebApproval(&p.State)
			issue = errUploadApproval
			return nil
		}
		t.ShareStage = stage
		updateTransferProgress(t, "transferring", 0, 0, "")
		return nil
	})
	if err != nil || issue != nil {
		return errors.Join(issue, err)
	}
	return nil
}

func (n *Node) saveWebUpload(w http.ResponseWriter, r *http.Request, token string, task domain.Transfer, src io.Reader, checkEnd func() error) (string, error) {
	select {
	case n.webUploads <- struct{}{}:
		defer func() { <-n.webUploads }()
	default:
		return "", errUploadBusy
	}
	ctx, finish, ok := n.transferContext(task.ID)
	if !ok {
		return "", errUploadGone
	}
	defer finish()
	ctx, cancel := context.WithCancel(ctx)
	stopRequest := context.AfterFunc(r.Context(), cancel)
	defer func() { stopRequest(); cancel() }()
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		return "", errUploadGone
	}
	n.workers.Add(1)
	n.mu.Unlock()
	defer n.workers.Done()
	if err := n.claimWebUpload(token, task); err != nil {
		return "", err
	}
	stage := ".goim-upload-" + task.ID
	failed := true
	defer func() {
		if failed {
			n.change(func(s *domain.State) {
				if i := transferIndex(s, task.ID); i >= 0 && s.Transfers[i].Status == "transferring" {
					t := &s.Transfers[i]
					updateTransferProgress(t, "failed", t.Done, 0, "网页上传中断或接收失败")
				}
			})
		}
	}()
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(10 * time.Minute))
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = controller.SetReadDeadline(time.Now()); close(stopped) })
	defer func() {
		if !stop() {
			<-stopped
		}
	}()
	root, err := openWebInbox(filepath.Dir(task.WebInbox))
	if err != nil {
		return "", errUploadStorage
	}
	defer root.Close()
	file, err := root.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", errUploadStorage
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = root.Remove(stage)
		}
	}()
	defer file.Close()
	progress := shareProgress{n: n, ctx: ctx, id: task.ID}
	limit := task.Size
	if checkEnd != nil {
		limit = webUploadLimit
	}
	size, err := io.Copy(io.MultiWriter(file, &progress), io.LimitReader(receiveReader{ctx, src}, limit+1))
	if size > limit {
		return "", uploadProblem{413, "文件超过允许的大小。"}
	}
	if ctx.Err() != nil {
		return "", errUploadGone
	}
	if err != nil || checkEnd == nil && size != task.Size {
		return "", errUploadInput
	}
	if checkEnd != nil {
		if err := checkEnd(); err != nil {
			return "", err
		}
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return "", errUploadStorage
	}
	// A linked stage is retained if the database commit fails. Startup can
	// then verify the inode before recording the upload as complete.
	name, linked, err := n.publishWebUpload(ctx, root, token, task, stage, size)
	keepStage = linked
	failed = !linked
	if err == nil && root.Remove(stage) == nil {
		n.change(func(s *domain.State) {
			if i := transferIndex(s, task.ID); i >= 0 {
				s.Transfers[i].ShareStage = ""
			}
		})
	}
	return name, err
}

func (n *Node) publishWebUpload(ctx context.Context, root *os.Root, token string, task domain.Transfer, stage string, size int64) (string, bool, error) {
	n.webUploadMu.Lock()
	defer n.webUploadMu.Unlock()
	pinned, err := root.Stat(".")
	current, pathErr := os.Lstat(task.WebInbox)
	if err != nil || pathErr != nil || !os.SameFile(pinned, current) {
		return "", false, errUploadGone
	}
	room, err := webInboxHasRoom(root, size)
	if err != nil {
		return "", false, errUploadStorage
	}
	if !room {
		return "", false, uploadProblem{507, "设备的网页接收目录已满。"}
	}
	for index := range 1000 {
		name := webUploadFilename(task.Name, index)
		if _, err := root.Lstat(name); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", false, errUploadStorage
		}
		dest := filepath.Join(task.WebInbox, name)
		err = n.change(func(s *domain.State) {
			if i := transferIndex(s, task.ID); i >= 0 {
				s.Transfers[i].Path = dest
			}
		})
		if err != nil {
			return "", false, err
		}
		var linkErr error
		linked := false
		err = n.durable(func(p *persisted) []pending {
			i := transferIndex(&p.State, task.ID)
			if i < 0 || p.State.Transfers[i].Status != "transferring" || p.BrowserHomeToken != token || filepath.Join(p.State.Settings.DownloadDir, domain.WebReceiveFolder) != task.WebInbox || ctx.Err() != nil {
				linkErr = errUploadGone
				return nil
			}
			if linkErr = root.Link(stage, name); linkErr == nil {
				linked = true
				t := &p.State.Transfers[i]
				t.Size = size
				t.Name = name
				updateTransferProgress(t, "completed", size, 0, "")
			}
			return nil
		})
		if err != nil {
			return "", linked, err
		}
		if errors.Is(linkErr, os.ErrExist) {
			continue
		}
		if linkErr != nil {
			return "", false, linkErr
		}
		return name, true, nil
	}
	return "", false, uploadProblem{409, "同名文件过多，请更改文件名后重试。"}
}
