package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"goim/internal/domain"
)

// URLs contain stable entry IDs. ID zero refers to the shared root.
type shareFolderEntry struct {
	ID       int `json:",omitempty"`
	Path     string
	Parent   int
	Folder   bool
	Size     int64
	Modified time.Time
}

// Entry IDs are stable across rescans. Older manifests used their slice
// position as the ID; the first rescan preserves all existing bookmarks.
func (r browserShare) entryID(i int) int {
	if id := r.Entries[i].ID; id != 0 {
		return id
	}
	return i + 1
}
func (r browserShare) entry(id int) (shareFolderEntry, bool) {
	for i, e := range r.Entries {
		if r.entryID(i) == id {
			return e, true
		}
	}
	return shareFolderEntry{}, false
}

func prepareFolder(ctx context.Context, source string, record browserShare) (domain.File, browserShare, error) {
	var file domain.File
	absolute, err := filepath.Abs(source)
	if err == nil {
		absolute, err = filepath.EvalSymlinks(absolute)
	}
	if err != nil {
		return file, record, err
	}
	if filepath.Dir(absolute) == absolute {
		return file, record, errors.New("请选择一个具体文件夹，不能共享整个磁盘")
	}
	root, err := openSharedRoot(absolute)
	if err != nil {
		return file, record, err
	}
	defer root.Close()
	record, err = scanFolder(ctx, root, record)
	file = domain.File{ID: newID(), Name: filepath.Base(absolute), SourcePath: absolute, Shared: true, ShareFolder: true, CreatedAt: time.Now()}
	return file, record, err
}

func scanFolder(ctx context.Context, root *os.Root, old browserShare) (browserShare, error) {
	next := old
	next.Entries = nil
	next.LastID = max(old.LastID, len(old.Entries))
	prior := make(map[string]shareFolderEntry, len(old.Entries))
	for i, e := range old.Entries {
		e.ID = old.entryID(i)
		prior[e.Path] = e
	}
	dirs := map[string]int{".": 0}
	err := fs.WalkDir(root.FS(), ".", func(name string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if name != "." && strings.HasPrefix(de.Name(), ".") {
			if de.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !de.IsDir() && !de.Type().IsRegular() {
			return nil
		}
		info, err := de.Info()
		if err != nil {
			return err
		}
		if name == "." {
			next.Modified = info.ModTime()
			return nil
		}
		if len(next.Entries) >= 4000 || strings.Count(name, "/") >= 32 {
			return errors.New("文件夹内容过多，请缩小分享范围后重试")
		}
		e := shareFolderEntry{Path: name, Parent: dirs[path.Dir(name)], Folder: info.IsDir(), Modified: info.ModTime()}
		if !e.Folder {
			if !info.Mode().IsRegular() {
				return nil
			}
			e.Size = info.Size()
		}
		p, exists := prior[name]
		if exists && p.Folder == e.Folder && (e.Folder || p.Size == e.Size && p.Modified.Equal(e.Modified)) {
			e.ID = p.ID
		} else {
			next.LastID++
			e.ID = next.LastID
		}
		if e.Folder {
			dirs[name] = e.ID
		}
		next.Entries = append(next.Entries, e)
		return nil
	})
	return next, err
}

func (n *Node) shareFolder(source string, record browserShare) error {
	f, record, err := prepareFolder(n.ctx, source, record)
	if err != nil {
		return err
	}
	absolute := f.SourcePath
	duplicate := false
	err = n.durable(func(p *persisted) []pending {
		s := &p.State
		for _, f := range s.Files {
			if f.Shared && f.SenderID == s.Self.ID &&
				(f.SourcePath == absolute || runtime.GOOS == "windows" && strings.EqualFold(f.SourcePath, absolute)) {
				duplicate = true
				return nil
			}
		}
		f.SenderID = s.Self.ID
		if p.BrowserShares == nil {
			p.BrowserShares = make(map[string]browserShare)
		}
		p.BrowserShares[f.ID] = record
		s.Files = append(s.Files, f)
		return nil
	})
	if err == nil && duplicate {
		err = errors.New("这个文件夹已在共享中，请从项目菜单管理链接或密码")
	}
	return err
}

func folderPageURL(base string, id int) string {
	if id == 0 {
		return base
	}
	return base + "?dir=" + strconv.Itoa(id)
}

func (n *Node) renderFolderShare(w http.ResponseWriter, r *http.Request, file domain.File, record browserShare, page sharePageData) {
	id := 0
	page.Current = "文件列表"
	page.Crumbs = []shareCrumb{{Name: file.Name, URL: page.Path}}
	dir := "."
	if value := r.URL.Query().Get("dir"); value != "" {
		id, _ = strconv.Atoi(value)
		entry, ok := record.entry(id)
		if !ok || !entry.Folder {
			page.Error = "这个文件夹不在分享范围内。"
			renderShare(w, r, http.StatusNotFound, page)
			return
		}
		dir, page.Current = entry.Path, path.Base(entry.Path)
		page.ParentURL = folderPageURL(page.Path, entry.Parent)
		var ancestors []shareCrumb
		for parent := id; parent > 0; {
			a, ok := record.entry(parent)
			if !ok {
				http.NotFound(w, r)
				return
			}
			parent = a.Parent
			ancestors = append(ancestors, shareCrumb{path.Base(a.Path), folderPageURL(page.Path, parent)})
		}
		slices.Reverse(ancestors)
		page.Crumbs = append(page.Crumbs, ancestors...)
	}
	page.Crumbs[len(page.Crumbs)-1].URL = ""
	root, err := openSharedRoot(file.SourcePath)
	if err == nil {
		defer root.Close()
		var stat fs.FileInfo
		stat, err = root.Lstat(filepath.FromSlash(dir))
		if err == nil && !stat.IsDir() {
			err = fs.ErrNotExist
		}
	}
	if err != nil {
		page.Error = "分享的文件夹已移走，或暂时无法访问。"
		renderShare(w, r, http.StatusGone, page)
		return
	}
	var folders, files int
	for index, entry := range record.Entries {
		if entry.Parent != id {
			continue
		}
		name := path.Base(entry.Path)
		if entry.Folder {
			folders++
			page.Items = append(page.Items, sharePageItem{Name: name, Folder: true, Style: "folder", URL: folderPageURL(page.Path, record.entryID(index))})
		} else {
			files++
			url := page.Path + "/download?file=" + strconv.Itoa(record.entryID(index))
			page.Items = append(page.Items, webFileItem(name, "", entry.Size, entry.Modified, url))
		}
	}
	slices.SortStableFunc(page.Items, func(a, b sharePageItem) int {
		if a.Folder != b.Folder {
			if a.Folder {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	switch {
	case folders == 0:
		page.Count = fmt.Sprintf("%d 个文件", files)
	case files == 0:
		page.Count = fmt.Sprintf("%d 个文件夹", folders)
	default:
		page.Count = fmt.Sprintf("%d 个文件夹 · %d 个文件", folders, files)
	}
	page.Ready = true
	renderShare(w, r, http.StatusOK, page)
}

func (n *Node) downloadFolderFile(w http.ResponseWriter, r *http.Request, file domain.File, record browserShare, base string) {
	id, _ := strconv.Atoi(r.URL.Query().Get("file"))
	entry, ok := record.entry(id)
	if !ok || entry.Folder {
		renderShare(w, r, http.StatusNotFound, sharePageData{Path: base, Error: "这个文件不在分享范围内。"})
		return
	}
	root, err := openSharedRoot(file.SourcePath)
	if err == nil {
		defer root.Close()
		var input *os.File
		input, err = openSharedFolderEntry(root, entry)
		if err == nil {
			defer input.Close()
			source, current, active := n.lookupShare(record.Token)
			if !active || source.ID != file.ID || current.Unavailable || current.Password.set() && !n.hasShareSession(r, current) {
				http.NotFound(w, r)
				return
			}
			name := path.Base(entry.Path)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
			http.ServeContent(w, r, name, entry.Modified, input)
			return
		}
	}
	renderShare(w, r, http.StatusGone, sharePageData{Path: base, Error: "文件已移动或修改，请返回共享目录查看最新内容。"})
}

func openSharedFolderEntry(root *os.Root, entry shareFolderEntry) (*os.File, error) {
	for parent := path.Dir(entry.Path); parent != "."; parent = path.Dir(parent) {
		dir, err := root.Lstat(filepath.FromSlash(parent))
		if err != nil || !dir.IsDir() {
			return nil, fs.ErrNotExist
		}
	}
	name := filepath.FromSlash(entry.Path)
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() {
		return nil, fs.ErrNotExist
	}
	input, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	after, err := input.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) ||
		after.Size() != entry.Size || !after.ModTime().Equal(entry.Modified) {
		input.Close()
		return nil, fs.ErrNotExist
	}
	return input, nil
}

func openSharedRoot(source string) (*os.Root, error) {
	// Sources are canonicalized when chosen. Reject a subsequently redirected
	// ancestor as well as a link in place of the shared directory itself.
	canonical, err := filepath.EvalSymlinks(source)
	if err != nil || canonical != source && !(runtime.GOOS == "windows" && strings.EqualFold(canonical, source)) {
		return nil, fs.ErrNotExist
	}
	before, err := os.Lstat(source)
	if err != nil || !before.IsDir() {
		return nil, fs.ErrNotExist
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		root.Close()
		return nil, fs.ErrNotExist
	}
	return root, nil
}
