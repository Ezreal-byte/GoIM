package localsend

import (
	"context"
	"crypto/tls"
	_ "embed"
	"errors"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"goim/internal/domain"
)

//go:embed link.html
var linkHTML string

var linkPage = template.Must(template.New("localsend").Parse(linkHTML))

type webLink struct {
	view     domain.LocalSendLinkState
	server   *http.Server
	pin      string
	files    map[string]source
	manifest map[string]fileInfo
	size     int64
	attempts map[string]pinState
	pending  *webRequest
	approved map[string]*webRequest
	ctx      context.Context
	cancel   context.CancelFunc
}

type webRequest struct {
	mu         sync.Mutex
	id, ip     string
	answer     chan bool
	validUntil time.Time
	done       map[string]bool
	running    map[string]bool
	bytes      int64
}

func (s *Service) linkIP() string {
	if s.options.NoDiscovery {
		return "127.0.0.1"
	}
	ifaces := s.interfaces()
	if route, err := net.DialTimeout("udp4", "192.0.2.1:9", 250*time.Millisecond); err == nil {
		preferred := route.LocalAddr().(*net.UDPAddr).IP
		_ = route.Close()
		for _, iface := range ifaces {
			a, _ := iface.Addrs()
			for _, address := range a {
				if ip, ok := address.(*net.IPNet); ok && ip.IP.Equal(preferred) {
					return preferred.String()
				}
			}
		}
	}
	fallback := ""
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ip, ok := addr.(*net.IPNet); ok && !ip.IP.IsLoopback() {
				if ip.IP.To4() != nil {
					return ip.IP.String()
				}
				if fallback == "" && ip.IP.IsGlobalUnicast() {
					fallback = ip.IP.String()
				}
			}
		}
	}
	return fallback
}

func (s *Service) StartLink(paths []string, pin string, upload, https bool) error {
	if !s.allowedReceive() {
		return errors.New("请先解锁 GoIM")
	}
	ip := s.linkIP()
	if ip == "" {
		return errors.New("找不到局域网地址")
	}
	var files []source
	var err error
	if !upload {
		files, err = sources(paths, "")
		if err != nil {
			return err
		}
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		return err
	}
	scheme := "http"
	if https {
		scheme = "https"
	}
	linkURL := (&url.URL{Scheme: scheme, Host: listener.Addr().String(), Path: "/"}).String()
	qr, err := qrcode.Encode(linkURL, qrcode.Medium, 200)
	if err != nil {
		_ = listener.Close()
		return err
	}
	if !s.allowedReceive() {
		_ = listener.Close()
		return errors.New("请先解锁 GoIM")
	}
	s.StopLink()
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
	link := &webLink{view: domain.LocalSendLinkState{URL: linkURL, QR: qr, Upload: upload},
		pin: pin, files: make(map[string]source), manifest: make(map[string]fileInfo), size: totalSources(files), attempts: make(map[string]pinState),
		approved: make(map[string]*webRequest), ctx: ctx, cancel: cancel}
	for _, f := range files {
		link.files[f.ID], link.manifest[f.ID] = f, f.fileInfo
	}
	link.server = s.httpServer(link)
	if https {
		listener = tls.NewListener(listener, link.server.TLSConfig)
	}
	s.mu.Lock()
	if s.closed || s.link != nil {
		s.mu.Unlock()
		cancel()
		_ = listener.Close()
		return context.Canceled
	}
	s.link = link
	s.wg.Go(func() { _ = link.server.Serve(listener) })
	s.wg.Go(func() { <-ctx.Done(); s.stopLink(link) })
	s.mu.Unlock()
	s.announceChange()
	return nil
}

func (s *Service) StopLink() {
	s.mu.Lock()
	link := s.link
	s.mu.Unlock()
	if link != nil {
		s.stopLink(link)
	}
}

func (s *Service) stopLink(link *webLink) {
	s.mu.Lock()
	if s.link != link {
		s.mu.Unlock()
		return
	}
	s.link = nil
	link.cancel()
	in := s.incoming
	if in != nil && in.link == link {
		in.cancel()
	}
	s.mu.Unlock()
	_ = link.server.Close()
	s.announceChange()
}

func (s *Service) AnswerLink(id string, allow bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.link == nil || s.link.pending == nil || s.link.pending.id != id {
		return errors.New("链接请求已结束")
	}
	select {
	case s.link.pending.answer <- allow:
		return nil
	default:
		return errors.New("链接请求已处理")
	}
}

func (s *Service) webRoutes(mux *http.ServeMux, link *webLink) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'")
		_ = linkPage.Execute(w, link.view)
	})
	if !link.view.Upload {
		mux.HandleFunc("POST "+prefix+"prepare-download", func(w http.ResponseWriter, r *http.Request) { s.prepareDownload(w, r, link) })
		mux.HandleFunc("GET "+prefix+"download", func(w http.ResponseWriter, r *http.Request) { s.webDownload(w, r, link) })
	}
}

func (s *Service) prepareDownload(w http.ResponseWriter, r *http.Request, link *webLink) {
	if !s.allowedReceive() {
		fail(w, 403, "Sharer locked")
		return
	}
	if !s.checkPIN(w, r, link) {
		return
	}
	now := time.Now()
	requestID := r.URL.Query().Get("sessionId")
	ip := remoteIP(r)
	s.mu.Lock()
	if s.link != link {
		s.mu.Unlock()
		fail(w, 403, "Link closed")
		return
	}
	for id, p := range link.approved {
		if now.After(p.validUntil) {
			delete(link.approved, id)
		}
	}
	if requestID != "" {
		old := link.approved[requestID]
		s.mu.Unlock()
		if old != nil && old.ip == ip {
			s.webGrant(w, link, old.id)
			return
		}
		fail(w, 403, "Invalid session")
		return
	}
	if link.pending != nil || len(link.approved) >= 32 {
		s.mu.Unlock()
		fail(w, 429, "Too many requests")
		return
	}
	p := &webRequest{id: "ls-" + newID(), ip: ip, answer: make(chan bool, 1), done: make(map[string]bool), running: make(map[string]bool)}
	link.pending = p
	s.mu.Unlock()
	s.announceChange()
	timer := time.NewTimer(approvalTime)
	defer timer.Stop()
	var approved bool
	select {
	case approved = <-p.answer:
	case <-timer.C:
	case <-r.Context().Done():
	case <-link.ctx.Done():
	}
	approved = approved && s.allowedReceive() && r.Context().Err() == nil
	s.mu.Lock()
	if link.pending == p {
		link.pending = nil
	}
	approved = approved && s.link == link
	if approved {
		p.validUntil = now.Add(30 * time.Minute)
		link.approved[p.id] = p
	}
	s.mu.Unlock()
	s.announceChange()
	if !approved {
		fail(w, 403, "Rejected")
		return
	}
	s.webGrant(w, link, p.id)
}

func (s *Service) webGrant(w http.ResponseWriter, link *webLink, id string) {
	i := s.selfInfo()
	i.Download = true
	i.Port, i.Protocol = 0, ""
	w.Header().Set("Cache-Control", "no-store")
	reply(w, downloadGrant{Info: i, SessionID: id, Files: link.manifest})
}

func (s *Service) webDownload(w http.ResponseWriter, r *http.Request, link *webLink) {
	if !s.allowedReceive() {
		fail(w, 403, "Sharer locked")
		return
	}
	query := r.URL.Query()
	id := query.Get("fileId")
	s.mu.Lock()
	p := link.approved[query.Get("sessionId")]
	f, found := link.files[id]
	if s.link != link || p == nil || p.ip != remoteIP(r) || time.Now().After(p.validUntil) || !found {
		s.mu.Unlock()
		fail(w, 403, "Invalid session or file")
		return
	}
	s.mu.Unlock()
	p.mu.Lock()
	if p.running[id] {
		p.mu.Unlock()
		fail(w, 409, "Download already in progress")
		return
	}
	p.running[id] = true
	before := p.bytes
	already := p.done[id]
	row := domain.Transfer{ID: p.id, Name: transferNameFromLink(link, f), Size: link.size, Done: before, Direction: "send",
		Status: "transferring", LegacyTransport: domain.TransportLocalSend, LocalSendPeer: p.ip}
	if !already {
		s.update(row)
	}
	p.mu.Unlock()
	raw, err := f.open()
	var written int64
	if err != nil {
		fail(w, 500, "File unavailable")
	} else {
		defer raw.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(f.Name)}))
		w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
		rc := http.NewResponseController(w)
		start := time.Now()
		progress := newProgress(func(n int64) {
			_ = rc.SetWriteDeadline(time.Now().Add(time.Minute))
			if !already {
				s.progress(p.id, min(before+n, link.size), float64(n)/time.Since(start).Seconds())
			}
		})
		_ = rc.SetWriteDeadline(time.Now().Add(time.Minute))
		written, err = io.Copy(w, io.TeeReader(raw, progress))
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.running, id)
	if err == nil && written == f.Size && !p.done[id] {
		p.done[id] = true
		p.bytes += written
	}
	row.Done = p.bytes
	all := len(p.done) == len(link.files)
	active := len(p.running)
	if !already {
		switch {
		case all:
			row.Status = "completed"
		case err != nil && active == 0:
			row.Status, row.Error = "failed", "浏览器下载未完成"
			if row.Done > 0 {
				row.Status = "partial"
			}
		case active == 0:
			row.Status = "partial"
		}
		s.update(row)
	}
}

func transferNameFromLink(link *webLink, f source) string {
	if len(link.files) == 1 {
		return f.Name
	}
	return "局域网链接分享"
}
