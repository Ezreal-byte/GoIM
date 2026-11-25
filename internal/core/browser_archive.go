package core

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"goim/internal/domain"
)

//go:embed share_select.js
var shareSelectScript string

type archiveProject struct {
	file   domain.File
	record browserShare
}

func (n *Node) downloadShareArchive(w http.ResponseWriter, r *http.Request, token string, device bool) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "请从分享页面选择下载项目", http.StatusMethodNotAllowed)
		return
	}
	if shareOrigin.Check(r) != nil {
		http.Error(w, "来源无效", http.StatusForbidden)
		return
	}
	base := strings.TrimSuffix(r.URL.Path, "/archive")
	fail := func(status int, message string) {
		renderShare(w, r, status, sharePageData{Path: base, Error: message})
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if r.ParseForm() != nil || len(r.PostForm["item"]) == 0 {
		fail(http.StatusBadRequest, "请选择要下载的文件或文件夹。")
		return
	}
	select {
	case n.webSlots <- struct{}{}:
		defer func() { <-n.webSlots }()
	default:
		fail(http.StatusTooManyRequests, "下载繁忙，请稍后重试。")
		return
	}

	selected := make(map[string]bool)
	projectIDs := []string{""}
	if device {
		projectIDs = nil
	}
	for _, item := range r.PostForm["item"] {
		if device && !selected[item] {
			projectIDs = append(projectIDs, item)
		}
		selected[item] = true
	}
	// Authorize every project before sending ZIP headers. Device downloads
	// verify each project's own password since /s/ cookies are path-scoped.
	var projects []archiveProject
	var dir int
	for _, projectID := range projectIDs {
		f, record, ok := n.lookupArchiveProject(r, token, device, projectID)
		if !ok {
			fail(http.StatusForbidden, "分享已关闭或需要密码，请返回分享页面重新打开。")
			return
		}
		if device && record.Password.set() && !record.Password.matches(r.PostForm.Get("password-"+projectID)) {
			fail(http.StatusUnauthorized, "请检查「"+f.Name+"」的分享密码，再重新下载。")
			return
		}
		if !device {
			var err error
			dir, err = strconv.Atoi(r.PostForm.Get("dir"))
			if !f.ShareFolder || err != nil || dir < 0 {
				fail(http.StatusBadRequest, "文件夹无效。")
				return
			}
		}
		n.refreshShareSources([]string{f.ID}, checkShareTree)
		previous := record
		sourceID := f.ID
		f, record, ok = n.lookupArchiveProject(r, token, device, projectID)
		if !ok || f.ID != sourceID || !bytes.Equal(previous.Password.Hash, record.Password.Hash) {
			fail(http.StatusGone, "分享已变更，请刷新后重新选择。")
			return
		}
		if !device {
			found := 0
			for i, e := range record.Entries {
				if e.Parent == dir && selected[strconv.Itoa(record.entryID(i))] {
					found++
				}
			}
			if found != len(selected) {
				fail(http.StatusGone, "所选项目已变更，请刷新后重新选择。")
				return
			}
		}
		projects = append(projects, archiveProject{f, record})
	}

	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "GoIM-所选文件.zip"}))
	w.Header().Set("Content-Type", "application/zip")
	out := &archiveResponse{Writer: w}
	z := zip.NewWriter(out)
	names := make(archiveNames)
	var completed map[string][]int
	if n.shareDownloadStatsEnabled() {
		completed = make(map[string][]int)
	}
	var err error
	for _, p := range projects {
		active := func() bool { return r.Context().Err() == nil && n.archiveAccess(r, token, device, p.file.ID, p.record) }
		if err = p.writeArchive(r, z, names, selected, dir, device, active, completed); err != nil {
			break
		}
	}
	if err == nil {
		err = z.Close()
	}
	if err == nil && r.Context().Err() == nil {
		n.recordShareDownloads(completed)
	}
	if err != nil {
		// A missing central directory or a short HTTP response must never
		// look like a successful download of the complete selection.
		if out.started {
			panic(http.ErrAbortHandler)
		}
		w.Header().Del("Content-Disposition")
		fail(http.StatusGone, "下载期间分享已变更，请返回分享页面重试。")
	}
}

func (n *Node) lookupArchiveProject(r *http.Request, token string, device bool, projectID string) (domain.File, browserShare, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if device {
		if token != n.data.BrowserHomeToken {
			return domain.File{}, browserShare{}, false
		}
		f := n.data.State.File(projectID)
		record := n.data.BrowserShares[projectID]
		return f, record, f.Shared && f.SenderID == n.data.State.Self.ID && record.Token != "" && !record.Unavailable
	}
	f, record, ok := n.lookupShareLocked(token)
	return f, record, ok && !record.Unavailable && (!record.Password.set() || n.hasShareSession(r, record))
}

func (n *Node) archiveAccess(r *http.Request, token string, device bool, fileID string, snapshot browserShare) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	current, ok := n.data.BrowserShares[fileID]
	if !ok || current.Token != snapshot.Token || current.Version != snapshot.Version || current.Unavailable {
		return false
	}
	if device {
		return n.data.BrowserHomeToken == token
	}
	return !current.Password.set() || n.hasShareSession(r, current)
}

type archiveResponse struct {
	io.Writer
	started bool
}

func (w *archiveResponse) Write(p []byte) (int, error) {
	w.started = true
	return w.Writer.Write(p)
}

type archiveNames map[string]bool

func (names archiveNames) next(parent, base string) string {
	// A Unix filename can contain a literal backslash or colon. Keep each
	// source component a single component when the ZIP is opened on Windows.
	base = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, base)
	base = strings.TrimRight(base, " .")
	if !validFileName(base) {
		base = "_" + base
	}
	for i := 0; ; i++ {
		name := path.Join(parent, webUploadFilename(base, i))
		key := strings.ToLower(name)
		if !names[key] {
			names[key] = true
			return name
		}
	}
}

func (p archiveProject) writeArchive(r *http.Request, z *zip.Writer, names archiveNames, selected map[string]bool, dir int, whole bool, active func() bool, completed map[string][]int) error {
	if !active() {
		return os.ErrPermission
	}
	if !p.file.ShareFolder {
		file, err := os.Open(p.file.SourcePath)
		if err != nil {
			return err
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil || !stat.Mode().IsRegular() || stat.Size() != p.file.Size || !verifyWebFile(r, file, p.file) || !active() {
			return os.ErrInvalid
		}
		err = writeArchiveFile(z, names.next("", p.file.Name), stat.ModTime(), file, p.file.Size, active)
		if err == nil && completed != nil {
			completed[p.file.ID] = []int{0}
		}
		return err
	}
	root, err := openSharedRoot(p.file.SourcePath)
	if err != nil {
		return err
	}
	defer root.Close()
	// The manifest is in WalkDir order, so a selected directory's archive
	// path is available before its children, including empty directories.
	parents := make(map[int]string)
	if whole {
		parents[0] = names.next("", p.file.Name)
		if _, err = z.Create(parents[0] + "/"); err != nil {
			return err
		}
	}
	for i, e := range p.record.Entries {
		id := p.record.entryID(i)
		parent, included := parents[e.Parent]
		if !included && (whole || e.Parent != dir || !selected[strconv.Itoa(id)]) {
			continue
		}
		if !active() {
			return os.ErrPermission
		}
		name := names.next(parent, path.Base(e.Path))
		if e.Folder {
			parents[id] = name
			if _, err = z.Create(name + "/"); err != nil {
				return err
			}
			continue
		}
		file, err := openSharedFolderEntry(root, e)
		if err != nil {
			return err
		}
		err = writeArchiveFile(z, name, e.Modified, file, e.Size, active)
		file.Close()
		if err != nil {
			return err
		}
		if completed != nil {
			completed[p.file.ID] = append(completed[p.file.ID], id)
		}
	}
	return nil
}

func writeArchiveFile(z *zip.Writer, name string, modified time.Time, file *os.File, size int64, active func() bool) error {
	entry, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: modified})
	if err != nil {
		return err
	}
	// Recheck revocation and request cancellation during a large entry, even
	// when it is the only file in the ZIP. Never finalize a revoked archive.
	for size > 0 {
		if !active() {
			return os.ErrPermission
		}
		n, err := io.CopyN(entry, file, min(size, 4<<20))
		if err != nil {
			return err
		}
		size -= n
	}
	if !active() {
		return os.ErrPermission
	}
	return nil
}
