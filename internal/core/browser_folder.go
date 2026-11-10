package core

import (
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

// The URL contains indices into this manifest, never local paths. Index zero
// refers to the share's root; stored entries are numbered starting at one.
type shareFolderEntry struct {
	Path     string
	Parent   int
	Folder   bool
	Size     int64
	Modified time.Time
}

func (n *Node) shareFolder(source string, record browserShare) error {
	absolute, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return err
	}
	if filepath.Dir(absolute) == absolute {
		return errors.New("请选择一个具体文件夹，不能共享整个磁盘")
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return err
	}
	defer root.Close()

	dirs := map[string]int{".": 0}
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := n.ctx.Err(); err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return nil
		}
		if len(record.Entries) >= 4000 || strings.Count(name, "/") >= 32 {
			return errors.New("文件夹内容过多，请缩小分享范围后重试")
		}
		item := shareFolderEntry{Path: name, Parent: dirs[path.Dir(name)], Folder: entry.IsDir()}
		if item.Folder {
			dirs[name] = len(record.Entries) + 1
		} else {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			item.Size, item.Modified = info.Size(), info.ModTime()
		}
		record.Entries = append(record.Entries, item)
		return nil
	})
	if err != nil {
		return err
	}
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
		f := domain.File{ID: newID(), Name: filepath.Base(absolute), SourcePath: absolute,
			SenderID: s.Self.ID, Shared: true, ShareFolder: true, CreatedAt: time.Now()}
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

func shareEntryID(value string, count int) (int, bool) {
	id, err := strconv.Atoi(value)
	return id, err == nil && id > 0 && id <= count
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
		var ok bool
		id, ok = shareEntryID(value, len(record.Entries))
		if !ok || !record.Entries[id-1].Folder {
			page.Error = "这个文件夹不在分享范围内。"
			renderShare(w, http.StatusNotFound, page)
			return
		}
		entry := record.Entries[id-1]
		dir, page.Current = entry.Path, path.Base(entry.Path)
		page.ParentURL = folderPageURL(page.Path, entry.Parent)
		var ancestors []shareCrumb
		for parent := id; parent > 0; parent = record.Entries[parent-1].Parent {
			a := record.Entries[parent-1]
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
		renderShare(w, http.StatusGone, page)
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
			page.Items = append(page.Items, sharePageItem{Name: name, Folder: true, Style: "folder", URL: folderPageURL(page.Path, index+1)})
		} else {
			files++
			url := page.Path + "/download?file=" + strconv.Itoa(index+1)
			page.Items = append(page.Items, webFileItem(name, entry.Size, entry.Modified, url))
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
	renderShare(w, http.StatusOK, page)
}

func (n *Node) downloadFolderFile(w http.ResponseWriter, r *http.Request, file domain.File, record browserShare, base string) {
	id, ok := shareEntryID(r.URL.Query().Get("file"), len(record.Entries))
	if !ok || record.Entries[id-1].Folder {
		renderShare(w, http.StatusNotFound, sharePageData{Path: base, Error: "这个文件不在分享范围内。"})
		return
	}
	entry := record.Entries[id-1]
	root, err := openSharedRoot(file.SourcePath)
	if err == nil {
		defer root.Close()
		var input *os.File
		input, err = openSharedFolderEntry(root, entry)
		if err == nil {
			defer input.Close()
			_, current, active := n.lookupShare(record.Token)
			if !active || current.Password.set() && !n.hasShareSession(r, current) {
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
	renderShare(w, http.StatusGone, sharePageData{Path: base, Error: "文件已移动或修改，请让分享人重新分享。"})
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
