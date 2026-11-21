package core

import (
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

//go:embed share_live.js
var shareLiveScript string

func projectVersion(id string, r browserShare) string {
	return id + "-" + strconv.FormatUint(r.Version, 10)
}

func (n *Node) notifyBrowserCatalog() {
	n.mu.Lock()
	n.webCatalogVersion++
	if n.webCatalogChange != nil {
		close(n.webCatalogChange)
		n.webCatalogChange = nil
	}
	n.mu.Unlock()
}

// The change channel and catalog stamp are read together so an update between
// rendering the page and opening its event stream cannot be missed.
func (n *Node) watchShare(w http.ResponseWriter, req *http.Request, token string, device bool) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "", http.StatusServiceUnavailable)
		return
	}
	want := req.URL.Query().Get("v")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	for {
		n.mu.Lock()
		version := ""
		if device {
			if token == n.data.BrowserHomeToken {
				version = strconv.FormatUint(n.webCatalogVersion, 10)
			}
		} else if f, r, ok := n.lookupShareLocked(token); ok && !r.Unavailable && (!r.Password.set() || n.hasShareSession(req, r)) {
			version = projectVersion(f.ID, r)
		}
		if n.webCatalogChange == nil {
			n.webCatalogChange = make(chan struct{})
		}
		change := n.webCatalogChange
		n.mu.Unlock()
		if version == "" || version != want {
			io.WriteString(w, "event: change\ndata: reload\n\n")
			return
		}
		select {
		case <-change:
		case <-req.Context().Done():
			return
		case <-n.ctx.Done():
			return
		}
	}
}
