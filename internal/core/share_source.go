package core

import (
	"errors"
	"mime"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"goim/internal/domain"
)

// Negative values distinguish root availability checks from catalog sweeps.
// Nonnegative values identify the directory a browser is about to display.
const (
	checkShareRoot = -2
	checkShareTree = -1
)

func (n *Node) ownedShareIDs() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var ids []string
	for _, f := range n.data.State.Files {
		if f.Shared && f.SenderID == n.data.State.Self.ID {
			ids = append(ids, f.ID)
		}
	}
	return ids
}

// Piggyback on discovery's existing heartbeat. Slow or disconnected volumes
// must not block the network loop or queue overlapping scans.
func (n *Node) scheduleShareCheck() {
	ids := n.ownedShareIDs()
	if len(ids) == 0 || !n.shareScanMu.TryLock() {
		return
	}
	if !n.async(func() {
		changed := n.refreshShareIDsLocked(ids, checkShareTree)
		n.shareScanMu.Unlock()
		if changed {
			n.broadcastCatalog()
		}
	}) {
		n.shareScanMu.Unlock()
	}
}
func (n *Node) refreshShareSources(ids []string, view int) {
	if len(ids) == 0 {
		return
	}
	n.shareScanMu.Lock()
	changed := n.refreshShareIDsLocked(ids, view)
	n.shareScanMu.Unlock()
	if changed {
		n.broadcastCatalog()
	}
}
func (n *Node) refreshShareIDsLocked(ids []string, view int) bool {
	changed := false
	for _, id := range ids {
		if n.ctx.Err() != nil {
			break
		}
		n.mu.Lock()
		f := n.data.State.File(id)
		own := f.Shared && f.SenderID == n.data.State.Self.ID
		old := n.data.BrowserShares[id]
		n.mu.Unlock()
		if !own {
			continue
		}
		next, dirty := n.inspectShareSource(f, old, view)
		if !dirty || n.ctx.Err() != nil {
			continue
		}
		visible := shareListingChanged(old, next)
		err := n.durable(func(p *persisted) []pending {
			current := p.BrowserShares[id]
			if !p.State.File(id).Shared {
				return nil
			}
			// Password changes may have committed while reading the filesystem.
			current.Modified, current.Entries, current.LastID, current.Unavailable = next.Modified, next.Entries, next.LastID, next.Unavailable
			if visible {
				current.Version++
				// Keep per-file counts bounded by the live manifest. The
				// share total remains cumulative for this project.
				current.pruneEntryDownloads()
			}
			if p.BrowserShares == nil {
				p.BrowserShares = make(map[string]browserShare)
			}
			p.BrowserShares[id] = current
			if current.Unavailable && !old.Unavailable {
				return n.revokeShareGrants(p, id, "分享不可用")
			}
			return nil
		})
		if err == nil && visible {
			changed = true
		}
	}
	return changed
}

// A directory timestamp may change solely because a dotfile was touched.
// Save that stamp, but only notify viewers when a visible entry changed.
func shareListingChanged(old, next browserShare) bool {
	if old.Unavailable != next.Unavailable || len(old.Entries) != len(next.Entries) {
		return true
	}
	for i := range next.Entries {
		if old.entryID(i) != next.entryID(i) {
			return true
		}
	}
	return false
}

func (n *Node) inspectShareSource(f domain.File, old browserShare, view int) (browserShare, bool) {
	next := old
	info, err := os.Lstat(f.SourcePath)
	if err != nil || f.ShareFolder != info.IsDir() || !f.ShareFolder && !info.Mode().IsRegular() {
		next.Unavailable = true
		next.Modified = time.Time{}
		next.LastID = max(next.LastID, len(next.Entries))
		next.Entries = nil
		return next, !old.Unavailable || !old.Modified.IsZero() || len(old.Entries) != 0
	}
	if !f.ShareFolder {
		if info.Size() != f.Size {
			next.Modified = time.Time{}
			next.Unavailable = true
		} else if old.Modified.IsZero() || !old.Modified.Equal(info.ModTime()) {
			digest, err := fileHashContext(n.ctx, f.SourcePath)
			next.Unavailable = err != nil || digest != f.Hash
			if err != nil {
				next.Modified = time.Time{}
			} else {
				next.Modified = info.ModTime()
			}
		}
		return next, next.Unavailable != old.Unavailable || !next.Modified.Equal(old.Modified)
	}
	if view == checkShareRoot && !old.Unavailable {
		return old, false
	}
	root, err := openSharedRoot(f.SourcePath)
	if err == nil {
		defer root.Close()
		if !old.Unavailable && info.ModTime().Equal(old.Modified) && !folderNeedsScan(root, old, view) {
			return old, false
		}
		next, err = scanFolder(n.ctx, root, old)
	}
	if err != nil {
		next = old
		next.Unavailable = true
		return next, !old.Unavailable
	}
	next.Unavailable = false
	return next, true
}

func folderNeedsScan(root *os.Root, r browserShare, view int) bool {
	if view == checkShareTree {
		// In-place saves can leave all parent timestamps untouched. Check the
		// bounded manifest's metadata; a content hash is not needed for polling.
		for _, e := range r.Entries {
			fi, err := root.Lstat(filepath.FromSlash(e.Path))
			if err != nil || e.Folder != fi.IsDir() || !e.Modified.Equal(fi.ModTime()) ||
				!e.Folder && (!fi.Mode().IsRegular() || e.Size != fi.Size()) {
				return true
			}
		}
		return false
	}
	path := "."
	if view != 0 {
		e, ok := r.entry(view)
		if !ok || !e.Folder {
			return false
		}
		path = filepath.FromSlash(e.Path)
	}
	// Read the requested directory even on volumes with coarse directory
	// timestamps. Only rebuild the full manifest if its visible contents differ.
	stat, err := root.Lstat(path)
	if err != nil || !stat.IsDir() {
		return true
	}
	list, err := root.Open(path)
	if err != nil {
		return true
	}
	children, err := list.ReadDir(-1)
	list.Close()
	if err != nil {
		return true
	}
	expected := make(map[string]shareFolderEntry)
	for _, e := range r.Entries {
		if e.Parent == view {
			expected[filepath.Base(e.Path)] = e
		}
	}
	for _, d := range children {
		if strings.HasPrefix(d.Name(), ".") || !d.IsDir() && !d.Type().IsRegular() {
			continue
		}
		e, ok := expected[d.Name()]
		if !ok || e.Folder != d.IsDir() {
			return true
		}
		fi, err := root.Lstat(filepath.Join(path, d.Name()))
		if err != nil || !e.Modified.Equal(fi.ModTime()) || !e.Folder && e.Size != fi.Size() {
			return true
		}
		delete(expected, d.Name())
	}
	return len(expected) != 0
}

// Replacing a source retains the browser link and password. Give it a fresh
// file identity and member key so queued native transfers can never fetch the
// replacement under a previously accepted file name or hash.
func (n *Node) replaceShareSource(id, source string) error {
	n.mu.Lock()
	old := n.data.State.File(id)
	record := n.data.BrowserShares[id]
	own := old.Shared && old.SenderID == n.data.State.Self.ID
	n.mu.Unlock()
	if !own {
		return errors.New("分享已失效")
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	absolute, err = canonicalSharePath(absolute)
	if err != nil {
		return err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return err
	}
	next := browserShare{LastID: max(record.LastID, len(record.Entries)), MemberKey: newID(), Modified: info.ModTime()}
	var f domain.File
	if info.IsDir() {
		f, next, err = prepareFolder(n.ctx, absolute, next)
	} else if info.Mode().IsRegular() {
		f = domain.File{ID: newID(), Name: filepath.Base(absolute), SourcePath: absolute, Size: info.Size(), CreatedAt: time.Now(), Shared: true, MIME: mime.TypeByExtension(strings.ToLower(filepath.Ext(absolute)))}
		f.Hash, err = fileHashContext(n.ctx, absolute)
		if err == nil && !validFile(f) {
			err = errors.New("文件名不适合跨平台传输")
		}
	} else {
		return errors.New("请选择文件或文件夹")
	}
	if err != nil {
		return err
	}
	after, err := os.Lstat(absolute)
	if err != nil {
		return err
	}
	if !os.SameFile(info, after) || !after.ModTime().Equal(info.ModTime()) || !f.ShareFolder && after.Size() != f.Size {
		return errors.New("共享内容已变化，请重新选择")
	}
	if err = n.startShareServer(); err != nil {
		return err
	}
	// Hashing a large replacement must not block other projects' listings.
	n.shareScanMu.Lock()
	n.mu.Lock()
	current := n.data.BrowserShares[id]
	n.mu.Unlock()
	if current.LastID != record.LastID {
		n.shareScanMu.Unlock()
		return errors.New("共享内容已变化，请重新选择")
	}
	checked, _ := n.inspectShareSource(old, current, checkShareTree)
	if !checked.Unavailable {
		n.shareScanMu.Unlock()
		return errors.New("共享源仍然可用")
	}
	var problem error
	err = n.durable(func(p *persisted) []pending {
		i := fileIndex(&p.State, id)
		if i < 0 || !p.State.Files[i].Shared || p.State.Files[i].SourcePath != old.SourcePath {
			problem = errors.New("分享已失效")
			return nil
		}
		for _, other := range p.State.Files {
			if other.ID != id && other.Shared && other.SenderID == p.State.Self.ID && (other.SourcePath == absolute || runtime.GOOS == "windows" && strings.EqualFold(other.SourcePath, absolute)) {
				problem = errors.New("该路径已在共享中")
				return nil
			}
		}
		current := p.BrowserShares[id]
		next.Token, next.Password = current.Token, current.Password
		if next.Token == "" {
			next.Token = randomToken()
		}
		next.Version = current.Version + 1
		f.SenderID = p.State.Self.ID
		p.State.Files[i].Shared = false
		delete(p.BrowserShares, id)
		if p.BrowserShares == nil {
			p.BrowserShares = make(map[string]browserShare)
		}
		p.BrowserShares[f.ID] = next
		p.State.Files = append(p.State.Files, f)
		return n.revokeShareGrants(p, id, "共享源已更换")
	})
	n.shareScanMu.Unlock()
	if err != nil || problem != nil {
		return errors.Join(err, problem)
	}
	n.broadcastCatalog()
	return nil
}
