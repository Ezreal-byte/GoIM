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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/skip2/go-qrcode"
	"goim/internal/domain"
)

const webUploadLimit = 256 << 20
const webInboxLimit = 1 << 30
const deviceHomeCookie = "goim_device_home"

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
	err := n.durable(func(p *persisted) []pending {
		if enabled && p.BrowserHomeToken == "" {
			p.BrowserHomeToken = randomToken()
		}
		if !enabled {
			p.BrowserHomeToken = ""
			n.data.WebAutoUntil = time.Time{}
			n.closeWebUploads(&p.State)
		}
		return nil
	})
	if err == nil {
		n.notifyBrowserCatalog()
	}
	return err
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
		AutoAcceptUntil: n.data.WebAutoUntil, URL: url, ReceiveDir: filepath.Join(s.Settings.DownloadDir, domain.WebReceiveFolder),
		QR: append([]byte(nil), n.webHomeLink.QR...),
	}
}

func (n *Node) deviceHomePath(r *http.Request) string {
	cookie, err := r.Cookie(deviceHomeCookie)
	if err != nil || len(cookie.Value) != 32 {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(n.data.BrowserHomeToken)) == 1 {
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
	page := sharePageData{Device: device, Title: device, Path: deviceSharePath(token), LiveVersion: strconv.FormatUint(n.webCatalogVersion, 10)}
	for _, file := range s.Files {
		record, exists := n.data.BrowserShares[file.ID]
		if !exists || record.Unavailable || !file.Shared || file.SenderID != s.Self.ID || record.Token == "" {
			continue
		}
		href := "/s/" + record.Token
		item := webFileItem(file.Name, file.MIME, file.Size, file.CreatedAt, href)
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
	if len(token) == 32 && tail == "events" {
		n.watchShare(w, r, token, true)
		return
	}
	if len(token) == 32 && strings.HasPrefix(tail, "upload/") {
		n.handleUploadTicket(w, r, token, strings.TrimPrefix(tail, "upload/"))
		return
	}
	if len(token) != 32 || tail != "" && tail != "upload" {
		http.NotFound(w, r)
		return
	}
	n.mu.Lock()
	validToken := subtle.ConstantTimeCompare([]byte(n.data.BrowserHomeToken), []byte(token)) == 1
	n.mu.Unlock()
	if validToken {
		n.refreshShareSources(n.ownedShareIDs(), checkShareRoot)
	}
	page, _, ok := n.deviceSharePage(token)
	if !ok {
		renderShare(w, r, http.StatusNotFound, sharePageData{Error: "设备主页已关闭或链接无效。"})
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
		http.SetCookie(w, &http.Cookie{Name: deviceHomeCookie, Value: token, Path: "/s/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		renderShare(w, r, http.StatusOK, page)
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
		renderShare(w, r, status, page)
	}
	n.mu.Lock()
	auto := time.Now().Before(n.data.WebAutoUntil)
	n.mu.Unlock()
	if !auto {
		fail(http.StatusForbidden, "请通过上传弹窗发送接收请求，并等待 GoIM 同意。")
		return
	}
	name, status, err := n.receiveWebFile(w, r, token)

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
	// Users may select a redirected Downloads folder or a Windows drive alias.
	// Only the inbox beneath that configured location must be a real directory.
	return openRealDirectory(dir)
}

// Older, single-file forms can upload directly during the explicit automatic window.
func (n *Node) receiveWebFile(w http.ResponseWriter, r *http.Request, token string) (string, int, error) {
	r.Body = http.MaxBytesReader(w, r.Body, webUploadLimit+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		return "", 400, errUploadInput
	}
	part, err := reader.NextPart()
	if err != nil {
		return "", 400, errUploadInput
	}
	_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil || part.FormName() != "file" {
		return "", 400, errUploadInput
	}
	t, err := n.offerWebUpload(token, webSender(r), params["filename"], 0, true)
	var name string
	if err == nil {
		name, err = n.saveWebUpload(w, r, token, t, part, func() error {
			if _, err := reader.NextPart(); err != io.EOF {
				return errUploadInput
			}
			return nil
		})
		if err != nil {
			n.setTransfer(t.ID, "failed", 0, 0, "网页上传失败")
		}
	}
	if err != nil {
		var problem uploadProblem
		if errors.As(err, &problem) {
			return "", problem.code, problem
		}
		return "", 503, errUploadStorage
	}
	return name, 200, nil
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

func serveShareScript(w http.ResponseWriter, r *http.Request, script string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "此操作不可用", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = io.WriteString(w, script)
}
