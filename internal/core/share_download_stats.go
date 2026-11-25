package core

import (
	"io"
	"net/http"
	"time"
)

func (n *Node) shareDownloadStatsEnabled() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.data.State.Settings.ShareDownloadStats
}

func (n *Node) recordShareDownloads(completed map[string][]int) {
	if len(completed) == 0 || !n.shareDownloadStatsEnabled() {
		return
	}
	// The bytes have already been sent. A store error is reported by durable
	// and cannot change the result of the download.
	_ = n.durable(func(p *persisted) []pending {
		for fileID, entries := range completed {
			p.countShareDownloads(fileID, entries)
		}
		return nil
	})
}

func (p *persisted) countShareDownloads(fileID string, entries []int) {
	if !p.State.Settings.ShareDownloadStats || len(entries) == 0 {
		return
	}
	file := p.State.File(fileID)
	if !file.Shared || file.SenderID != p.State.Self.ID {
		return
	}
	record := p.BrowserShares[fileID]
	if record.Unavailable {
		return
	}
	if file.ShareFolder {
		downloaded := make(map[int]struct{}, len(entries))
		for _, id := range entries {
			downloaded[id] = struct{}{}
		}
		for i, entry := range record.Entries {
			id := record.entryID(i)
			if _, ok := downloaded[id]; !ok || entry.Folder {
				continue
			}
			if record.EntryDownloads == nil {
				record.EntryDownloads = make(map[int]uint64)
			}
			record.EntryDownloads[id]++
			record.Downloads++
			delete(downloaded, id)
			if len(downloaded) == 0 {
				break
			}
		}
	} else {
		record.Downloads++
	}
	if p.BrowserShares == nil {
		p.BrowserShares = make(map[string]browserShare)
	}
	p.BrowserShares[fileID] = record
}

func (r *browserShare) pruneEntryDownloads() {
	if len(r.EntryDownloads) == 0 {
		return
	}
	counts := make(map[int]uint64)
	for i, entry := range r.Entries {
		id := r.entryID(i)
		if count := r.EntryDownloads[id]; !entry.Folder && count != 0 {
			counts[id] = count
		}
	}
	r.EntryDownloads = counts
}

func (n *Node) serveShareDownload(w http.ResponseWriter, r *http.Request, fileID string, entryID int, name string, modified time.Time, size int64, source io.ReadSeeker) {
	if r.Method != http.MethodGet || !n.shareDownloadStatsEnabled() {
		http.ServeContent(w, r, name, modified, source)
		return
	}
	out := &shareDownloadResponse{ResponseWriter: w}
	http.ServeContent(out, r, name, modified, source)
	// A probe, error page, single range covering only part of a file or
	// multipart range response isn't evidence that the full file was sent.
	full := out.status == http.StatusOK ||
		out.status == http.StatusPartialContent && out.Header().Get("Content-Range") != ""
	if full && !out.failed && out.bytes == size && r.Context().Err() == nil {
		n.recordShareDownloads(map[string][]int{fileID: {entryID}})
	}
}

type shareDownloadResponse struct {
	http.ResponseWriter
	status int
	bytes  int64
	failed bool
}

func (w *shareDownloadResponse) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *shareDownloadResponse) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	w.failed = w.failed || err != nil || n != len(p)
	return n, err
}
