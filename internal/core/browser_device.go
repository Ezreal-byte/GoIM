package core

import (
	"crypto/subtle"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/skip2/go-qrcode"
	"goim/internal/domain"
)

const webUploadLimit = 256 << 20
const webInboxLimit = 1 << 30

var shareOrigin = http.NewCrossOriginProtection()

//go:embed share_upload.js
var shareUploadScript string

type deviceShareEntry struct {
	sharePageItem
	Protected bool
	created   time.Time
}

func (n *Node) setDeviceShare(enabled bool) error {
	if enabled {
		n.mu.Lock()
		dir := n.data.State.Settings.DownloadDir
		n.mu.Unlock()
		if dir == "" {
			return errors.New("请先设置文件接收目录")
		}
		root, err := openWebInbox(dir)
		if err != nil {
			return fmt.Errorf("无法创建网页接收目录：%w", err)
		}
		root.Close()
		if err := n.startShareServer(); err != nil {
			return err
		}
	}
	return n.durable(func(p *persisted) []pending {
		if enabled && p.BrowserHomeToken == "" {
			p.BrowserHomeToken = randomToken()
		}
		if !enabled {
			p.BrowserHomeToken = ""
		}
		return nil
	})
}

func deviceSharePath(token string) string { return "/d/" + token }

// attachDeviceShare runs under n.mu and never copies the bearer token into the
// persisted public state or into a peer advertisement.
func (n *Node) attachDeviceShare(s *domain.State) {
	token := n.data.BrowserHomeToken
	if token == "" || n.webAddress == "" {
		n.webHomeLink = shareLink{}
		return
	}
	url := "http://" + n.webAddress + deviceSharePath(token)
	if n.webHomeLink.URL != url {
		n.webHomeLink.URL = url
		n.webHomeLink.QR, _ = qrcode.Encode(url, qrcode.Medium, 224)
	}
	s.DeviceShare = domain.DeviceShare{
		URL: url, ReceiveDir: filepath.Join(s.Settings.DownloadDir, domain.WebReceiveFolder),
		QR: append([]byte(nil), n.webHomeLink.QR...),
	}
}

func (n *Node) deviceHomePath() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.data.BrowserHomeToken != "" {
		return deviceSharePath(n.data.BrowserHomeToken)
	}
	return ""
}

func (n *Node) deviceSharePage(token string) (sharePageData, string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closing || subtle.ConstantTimeCompare([]byte(token), []byte(n.data.BrowserHomeToken)) != 1 {
		return sharePageData{}, "", false
	}
	s := &n.data.State
	device := s.Self.Name + "的电脑"
	page := sharePageData{Device: device, Title: device, Path: deviceSharePath(token)}
	for _, file := range s.Files {
		record, exists := n.data.BrowserShares[file.ID]
		if !exists || !file.Shared || file.SenderID != s.Self.ID || record.Token == "" {
			continue
		}
		href := "/s/" + record.Token
		item := webFileItem(file.Name, file.Size, file.CreatedAt, href)
		if file.ShareFolder {
			item.Folder, item.Style = true, "folder"
		}
		page.Catalog = append(page.Catalog, deviceShareEntry{item, record.Password.set(), file.CreatedAt})
	}
	slices.SortStableFunc(page.Catalog, func(a, b deviceShareEntry) int {
		return b.created.Compare(a.created)
	})
	return page, s.Settings.DownloadDir, true
}

func (n *Node) handleDeviceShare(w http.ResponseWriter, r *http.Request) {
	token, tail, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/d/"), "/")
	if len(token) != 32 || tail != "" && tail != "upload" {
		http.NotFound(w, r)
		return
	}
	page, downloadDir, ok := n.deviceSharePage(token)
	if !ok {
		renderShare(w, http.StatusNotFound, sharePageData{Error: "设备主页已关闭或链接无效。"})
		return
	}
	if tail == "" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "此操作不可用", http.StatusMethodNotAllowed)
			return
		}
		page.OpenUpload = r.URL.Query().Get("upload") == "1"
		page.Uploaded = r.URL.Query().Get("sent") == "1"
		renderShare(w, http.StatusOK, page)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "此操作不可用", http.StatusMethodNotAllowed)
		return
	}
	if err := shareOrigin.Check(r); err != nil {
		http.Error(w, "来源无效", http.StatusForbidden)
		return
	}
	page.OpenUpload = true
	fail := func(status int, message string) {
		page.UploadError = message
		renderShare(w, status, page)
	}
	select {
	case n.webUploads <- struct{}{}:
		defer func() { <-n.webUploads }()
	default:
		fail(http.StatusTooManyRequests, "设备正在接收其他文件，请稍后重试。")
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(10 * time.Minute))
	name, status, err := n.receiveWebFile(w, r, token, downloadDir)
	if err != nil {
		fail(status, err.Error())
		return
	}
	n.emitShareFeedback("网页已接收文件", name, false)
	http.Redirect(w, r, page.Path+"?sent=1", http.StatusSeeOther)
}

func openWebInbox(downloadDir string) (*os.Root, error) {
	if downloadDir == "" {
		return nil, os.ErrNotExist
	}
	dir := filepath.Join(downloadDir, domain.WebReceiveFolder)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return openSharedRoot(dir)
}

func (n *Node) receiveWebFile(w http.ResponseWriter, r *http.Request, token, downloadDir string) (string, int, error) {
	const maxBody = webUploadLimit + (64 << 10)
	tooLarge := errors.New("文件超过 256 MB，无法上传。")
	invalid := errors.New("请选择一个有效的文件后重试。")
	unavailable := errors.New("无法写入设备的接收目录，请联系设备使用者。")
	if r.ContentLength > maxBody {
		return "", http.StatusRequestEntityTooLarge, tooLarge
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	reader, err := r.MultipartReader()
	if err != nil {
		return "", http.StatusBadRequest, invalid
	}
	part, err := reader.NextPart()
	if err != nil {
		return "", http.StatusBadRequest, invalid
	}
	_, parameters, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	name := parameters["filename"]
	if err != nil || part.FormName() != "file" || !validFileName(name) || strings.HasPrefix(name, ".") {
		return "", http.StatusBadRequest, invalid
	}
	root, err := openWebInbox(downloadDir)
	if err != nil {
		return "", http.StatusServiceUnavailable, unavailable
	}
	defer root.Close()
	temp := ".goim-upload-" + randomToken()
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", http.StatusServiceUnavailable, unavailable
	}
	defer root.Remove(temp)
	defer file.Close()
	size, err := io.Copy(file, io.LimitReader(part, webUploadLimit+1))
	var sizeError *http.MaxBytesError
	if size > webUploadLimit || errors.As(err, &sizeError) {
		return "", http.StatusRequestEntityTooLarge, tooLarge
	}
	if err != nil {
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			return "", http.StatusServiceUnavailable, unavailable
		}
		return "", http.StatusBadRequest, errors.New("上传中断，请重新选择文件。")
	}
	if _, err := reader.NextPart(); err != io.EOF {
		if errors.As(err, &sizeError) {
			return "", http.StatusRequestEntityTooLarge, tooLarge
		}
		return "", http.StatusBadRequest, invalid
	}
	if err := file.Sync(); err != nil {
		return "", http.StatusServiceUnavailable, unavailable
	}
	if err := file.Close(); err != nil {
		return "", http.StatusServiceUnavailable, unavailable
	}
	n.webUploadMu.Lock()
	defer n.webUploadMu.Unlock()
	room, err := webInboxHasRoom(root, size)
	if err != nil {
		return "", http.StatusServiceUnavailable, unavailable
	}
	if !room {
		return "", http.StatusInsufficientStorage, errors.New("设备的网页接收目录已满，请联系设备使用者。")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closing || n.data.BrowserHomeToken != token || n.data.State.Settings.DownloadDir != downloadDir || r.Context().Err() != nil {
		return "", http.StatusGone, errors.New("设备主页或接收位置已变化，请重新打开链接。")
	}
	for index := range 1000 {
		candidate := webUploadFilename(name, index)
		err := root.Link(temp, candidate)
		switch {
		case err == nil:
			return candidate, http.StatusOK, nil
		case !errors.Is(err, os.ErrExist):
			return "", http.StatusServiceUnavailable, unavailable
		}
	}
	return "", http.StatusConflict, errors.New("同名文件过多，请更改文件名后重试。")
}

func webInboxHasRoom(root *os.Root, size int64) (bool, error) {
	directory, err := root.Open(".")
	if err != nil {
		return false, err
	}
	defer directory.Close()
	for {
		entries, err := directory.ReadDir(128)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".goim-upload-") || !entry.Type().IsRegular() {
				continue
			}
			info, err := entry.Info()
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
			if err == nil {
				size += info.Size()
				if size > webInboxLimit {
					return false, nil
				}
			}
		}
		if err == io.EOF {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
}

func webUploadFilename(name string, index int) string {
	if index == 0 {
		return name
	}
	ext := filepath.Ext(name)
	if len(ext) > 20 {
		ext = ""
	}
	base := strings.TrimSuffix(name, ext)
	suffix := fmt.Sprintf(" (%d)%s", index+1, ext)
	for len(base)+len(suffix) > 255 {
		_, width := utf8.DecodeLastRuneInString(base)
		base = base[:len(base)-width]
	}
	return base + suffix
}

func serveShareUploadScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "此操作不可用", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = io.WriteString(w, shareUploadScript)
}
