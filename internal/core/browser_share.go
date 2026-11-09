package core

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"goim/internal/brand"
	"goim/internal/domain"
)

type browserShare struct {
	Token    string
	Password credential
}

type shareLink struct {
	URL string
	QR  []byte
}

//go:embed share_page.html
var sharePageHTML string
var shareLogo = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(brand.PNG("goim-64")))
var sharePage = template.Must(template.New("share").Funcs(template.FuncMap{
	"logo": func() template.URL { return shareLogo },
}).Parse(sharePageHTML))

func randomToken() string {
	var data [24]byte
	rand.Read(data[:])
	return base64.RawURLEncoding.EncodeToString(data[:])
}

func (n *Node) shareFile(path, password string) error {
	var link browserShare
	if password != "" {
		next, err := newCredential(password)
		if err != nil {
			return err
		}
		link.Password = next
	}
	if err := n.startShareServer(); err != nil {
		return err
	}
	link.Token = randomToken()
	return n.sendFileWithLink("", path, true, "", link)
}

func (n *Node) updateSharePassword(id, password string) error {
	var next credential
	if password != "" {
		var err error
		next, err = newCredential(password)
		if err != nil {
			return err
		}
	}
	if err := n.startShareServer(); err != nil {
		return err
	}
	token := randomToken()
	changed := false
	err := n.durable(func(p *persisted) []pending {
		s := &p.State
		i := fileIndex(s, id)
		if i < 0 || !s.Files[i].Shared || s.Files[i].SenderID != s.Self.ID {
			return nil
		}
		record := p.BrowserShares[id]
		if record.Token == "" {
			record.Token = token
		}
		record.Password = next
		if p.BrowserShares == nil {
			p.BrowserShares = make(map[string]browserShare)
		}
		p.BrowserShares[id] = record
		changed = true
		s.Notice, s.Error = "分享密码已更新", ""
		if !next.set() {
			return nil
		}
		return n.revokeShareGrants(p, id, "分享已设置密码")
	})
	if err != nil {
		return err
	}
	if !changed {
		return errors.New("分享已失效")
	}
	n.broadcastCatalog()
	return nil
}

// attachShareLinks runs under n.mu. The bearer URLs never enter persisted
// public state or peer envelopes. QR images are generated once per active URL.
func (n *Node) attachShareLinks(s *domain.State, urls bool) {
	if len(n.data.BrowserShares) == 0 {
		return
	}
	if n.webLinks == nil {
		n.webLinks = make(map[string]shareLink)
	}
	for id := range n.webLinks {
		if _, exists := n.data.BrowserShares[id]; !exists {
			delete(n.webLinks, id)
		}
	}
	for i := range s.Files {
		f := &s.Files[i]
		record, ok := n.data.BrowserShares[f.ID]
		if !ok || !f.Shared || f.SenderID != s.Self.ID {
			continue
		}
		f.ShareProtected = record.Password.set()
		if !urls || n.webAddress == "" {
			continue
		}
		url := "http://" + n.webAddress + "/s/" + record.Token
		link := n.webLinks[f.ID]
		if link.URL != url {
			link.URL = url
			link.QR, _ = qrcode.Encode(url, qrcode.Medium, 224)
			n.webLinks[f.ID] = link
		}
		f.ShareURL, f.ShareQR = url, append([]byte(nil), link.QR...)
	}
}

// revokeShareGrants runs inside the same durable mutation as access changes.
func (n *Node) revokeShareGrants(p *persisted, id, reason string) []pending {
	var out []pending
	for i := range p.State.Transfers {
		t := &p.State.Transfers[i]
		if t.FileID != id || !t.FromShare || t.Direction != "send" || terminalTransfer(t.Status) {
			continue
		}
		t.Revoked, t.Status, t.Speed, t.Error = true, "cancelled", 0, reason
		if cancel := n.transfers[t.ID]; cancel != nil {
			cancel()
		}
		p.TransferTombstones[t.PeerID+":"+t.ID] = "cancelled"
		for key, d := range p.Outbox {
			if d.Envelope.Type == "file_offer" && d.Envelope.TransferID == t.ID {
				delete(p.Outbox, key)
			}
		}
		out = append(out, pending{PeerID: t.PeerID, Envelope: envelope{Type: "file_status", TransferID: t.ID, Status: "cancelled"}})
	}
	return out
}

func browserIP(bound *net.TCPAddr, preferred string) net.IP {
	if !bound.IP.IsUnspecified() {
		return bound.IP
	}
	ifaces, _ := net.Interfaces()
	var candidate net.IP
	var routed net.IP
	// A UDP connect selects the OS route and local address without transmitting.
	if route, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 9}); err == nil {
		routed = route.LocalAddr().(*net.UDPAddr).IP
		_ = route.Close()
	}
	var defaultRoute net.IP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			v, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := v.IP.To4()
			if ip == nil || !ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast() {
				continue
			}
			if iface.Name == preferred {
				return ip
			}
			if ip.Equal(routed) {
				defaultRoute = ip
			}
			if candidate == nil || !candidate.IsPrivate() && ip.IsPrivate() {
				candidate = ip
			}
		}
	}
	if defaultRoute != nil {
		return defaultRoute
	}
	if candidate != nil {
		return candidate
	}
	return net.IPv4(127, 0, 0, 1)
}

func (n *Node) startShareServer() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closing {
		return errors.New("应用正在关闭")
	}
	if n.webServer != nil {
		return nil
	}
	bound := n.listener.Addr().(*net.TCPAddr)
	ip := browserIP(bound, n.data.State.Settings.Network)
	port := n.data.BrowserPort
	if port == "" {
		port = "0"
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(ip.String(), port))
	if err != nil && port != "0" {
		listener, err = net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
	}
	if err != nil {
		return fmt.Errorf("无法启动局域网分享: %w", err)
	}
	n.webAddress = listener.Addr().String()
	_, n.data.BrowserPort, _ = net.SplitHostPort(n.webAddress)
	rand.Read(n.webKey[:])
	n.webSlots = make(chan struct{}, 8)
	server := &http.Server{
		Handler:           n.shareHandler(ip),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 12 * time.Second,
		IdleTimeout: 20 * time.Second, MaxHeaderBytes: 12 << 10,
	}
	n.webServer = server
	n.wg.Add(1)
	go func() { defer n.wg.Done(); _ = server.Serve(listener) }()
	return nil
}

func (n *Node) shareHandler(local net.IP) http.Handler {
	var networks []*net.IPNet
	if addresses, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addresses {
			if network, ok := a.(*net.IPNet); ok && network.IP.Equal(local) {
				networks = append(networks, network)
			}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ip := net.ParseIP(host)
		allowed := local.IsLoopback() && ip.IsLoopback() || !local.IsLoopback() && ip.IsPrivate()
		for _, subnet := range networks {
			allowed = allowed || subnet.Contains(ip)
		}
		if !allowed {
			http.NotFound(w, r)
			return
		}
		n.handleShare(w, r)
	})
}

func (n *Node) lookupShare(token string) (domain.File, browserShare, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for id, record := range n.data.BrowserShares {
		if record.Token != token {
			continue
		}
		s := &n.data.State
		i := fileIndex(s, id)
		if i >= 0 && s.Files[i].Shared && s.Files[i].SenderID == s.Self.ID {
			return s.Files[i], record, true
		}
		break
	}
	return domain.File{}, browserShare{}, false
}

func (n *Node) authDigest(record browserShare, expiry string) string {
	mac := hmac.New(sha256.New, n.webKey[:])
	fmt.Fprintf(mac, "%s:%s:", record.Token, expiry)
	mac.Write(record.Password.Hash)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (n *Node) hasShareSession(r *http.Request, record browserShare) bool {
	c, err := r.Cookie("goim_share")
	if err != nil {
		return false
	}
	expiry, digest, ok := strings.Cut(c.Value, ".")
	ts, err := strconv.ParseInt(expiry, 10, 64)
	return ok && err == nil && time.Now().Unix() < ts &&
		hmac.Equal([]byte(digest), []byte(n.authDigest(record, expiry)))
}

type sharePageData struct {
	Name, Size, Path, Error string
	Sender, Kind, Date      string
	Locked                  bool
}

func shareFileSize(size int64) string {
	for _, unit := range []struct {
		bytes int64
		label string
	}{{1 << 30, "GB"}, {1 << 20, "MB"}, {1 << 10, "KB"}} {
		if size >= unit.bytes {
			return fmt.Sprintf("%.1f %s", float64(size)/float64(unit.bytes), unit.label)
		}
	}
	return fmt.Sprintf("%d 字节", size)
}

func renderShare(w http.ResponseWriter, status int, data sharePageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = sharePage.Execute(w, data)
}

func (n *Node) handleShare(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	// Keep cross-origin requests free of the bearer URL without making a
	// browser's same-origin form POST serialize its Origin as "null".
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Host != n.webAddress || !strings.HasPrefix(r.URL.Path, "/s/") {
		http.NotFound(w, r)
		return
	}
	token, tail, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/s/"), "/")
	if len(token) != 32 || (tail != "" && tail != "download") {
		http.NotFound(w, r)
		return
	}
	f, record, ok := n.lookupShare(token)
	if !ok {
		renderShare(w, http.StatusNotFound, sharePageData{Error: "此分享已撤回或链接无效。"})
		return
	}
	page := sharePageData{Path: "/s/" + token, Locked: record.Password.set()}
	if r.Method == http.MethodPost && tail == "" && page.Locked {
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "来源无效", http.StatusForbidden)
			return
		}
		select {
		case n.webSlots <- struct{}{}:
			defer func() { <-n.webSlots }()
		default:
			http.Error(w, "请稍后重试", http.StatusTooManyRequests)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		if r.ParseForm() != nil || !record.Password.matches(r.PostForm.Get("password")) {
			page.Error = "密码不正确，请重试。"
			renderShare(w, http.StatusUnauthorized, page)
			return
		}
		exp := time.Now().Add(30 * time.Minute)
		expiry := strconv.FormatInt(exp.Unix(), 10)
		http.SetCookie(w, &http.Cookie{Name: "goim_share", Value: expiry + "." + n.authDigest(record, expiry),
			Path: page.Path, Expires: exp, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, page.Path, http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "此操作不可用", http.StatusMethodNotAllowed)
		return
	}
	if page.Locked && !n.hasShareSession(r, record) {
		status := http.StatusOK
		if tail == "download" {
			status = http.StatusUnauthorized
		}
		renderShare(w, status, page)
		return
	}
	page.Locked, page.Name, page.Size = false, f.Name, shareFileSize(f.Size)
	page.Kind = strings.ToUpper(strings.TrimPrefix(filepath.Ext(f.Name), "."))
	if page.Kind == "" || len(page.Kind) > 5 {
		page.Kind = "文件"
	}
	if !f.CreatedAt.IsZero() {
		page.Date = f.CreatedAt.Local().Format("2006-01-02")
	}
	if tail == "" {
		n.mu.Lock()
		page.Sender = n.data.State.Self.Name
		n.mu.Unlock()
		renderShare(w, http.StatusOK, page)
		return
	}
	select {
	case n.webSlots <- struct{}{}:
		defer func() { <-n.webSlots }()
	default:
		http.Error(w, "下载繁忙，请稍后重试", http.StatusTooManyRequests)
		return
	}
	source, err := os.Open(f.SourcePath)
	if err != nil {
		renderShare(w, http.StatusGone, sharePageData{Error: "分享的源文件已移走。"})
		return
	}
	defer source.Close()
	stat, err := source.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != f.Size || !verifyWebFile(r, source, f) {
		renderShare(w, http.StatusGone, sharePageData{Error: "源文件已变化，请让发送方重新分享。"})
		return
	}
	_, current, ok := n.lookupShare(token)
	if !ok || current.Password.set() && !n.hasShareSession(r, current) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
	w.Header().Set("ETag", `"`+f.Hash+`"`)
	http.ServeContent(w, r, f.Name, stat.ModTime(), source)
}

func verifyWebFile(r *http.Request, source *os.File, f domain.File) bool {
	hash := sha256.New()
	var buf [64 << 10]byte
	for {
		if r.Context().Err() != nil {
			return false
		}
		count, err := source.Read(buf[:])
		hash.Write(buf[:count])
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
	}
	_, err := source.Seek(0, io.SeekStart)
	return err == nil && hex.EncodeToString(hash.Sum(nil)) == f.Hash
}
