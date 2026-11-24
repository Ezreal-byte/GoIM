package localsend

import (
	"context"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"goim/internal/domain"
)

type Options struct {
	Context                     context.Context
	Config                      domain.LocalSendConfig
	IdentityDir, Alias, Network string
	DownloadDir                 func() string
	AllowReceive                func() bool
	Changed                     func()
	Persist                     func(domain.Transfer)
	NoDiscovery                 bool
}

type Service struct {
	scanning         atomic.Bool
	udp6             *net.UDPConn
	wake6            chan struct{}
	progressRevision uint64
	mu               sync.Mutex
	wg               sync.WaitGroup
	closed           bool
	ctx              context.Context
	stop             context.CancelFunc
	options          Options
	cfg              domain.LocalSendConfig
	alias, network   string
	port             int
	cert             tls.Certificate
	fingerprint      string
	server           *http.Server
	udp              *net.UDPConn
	problem          string
	peers            map[string]seenDevice
	endpoints        map[string]string
	workers          chan struct{}
	usePIN           map[string]pinState
	incoming         *incoming
	jobs             map[string]*outgoing
	transfers        map[string]domain.Transfer
	link             *webLink
	wake             chan struct{}
}

type pinState struct {
	failed int
	until  time.Time
}

func Start(options Options) (*Service, error) {
	if options.Context == nil {
		options.Context = context.Background()
	}
	cert, err := identity(options.IdentityDir)
	if err != nil {
		return nil, err
	}
	host := ""
	if options.NoDiscovery {
		host = "127.0.0.1"
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, portString(options.Config.Port)))
	if err != nil {
		return nil, err
	}
	ctx, stop := context.WithCancel(options.Context)
	s := &Service{
		ctx: ctx, stop: stop, options: options, cfg: options.Config, alias: cleanAlias(options.Alias), network: options.Network,
		port: listener.Addr().(*net.TCPAddr).Port, cert: cert, fingerprint: fingerprint(cert.Certificate[0]),
		peers: make(map[string]seenDevice), endpoints: make(map[string]string), workers: make(chan struct{}, 16),
		usePIN: make(map[string]pinState), jobs: make(map[string]*outgoing), transfers: make(map[string]domain.Transfer),
		wake: make(chan struct{}, 1), wake6: make(chan struct{}, 1),
	}
	s.server = s.httpServer(nil)
	if !s.cfg.HTTP {
		listener = tls.NewListener(listener, s.server.TLSConfig)
	}
	s.spawn(func() { _ = s.server.Serve(listener) })
	if !options.NoDiscovery {
		s.startDiscovery()
		s.startIPv6Discovery()
	}
	return s, nil
}

func saveIdentity(path string, der []byte, key *rsa.PrivateKey) (tls.Certificate, error) {
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	data := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})...)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return tls.Certificate{}, err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err = errors.Join(err, closeErr); err != nil {
		_ = os.Remove(path)
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(data, data)
}

func (s *Service) spawn(fn func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.wg.Go(fn)
	return true
}

func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.stop()
	s.mu.Unlock()
	s.StopLink()
	_ = s.server.Close()
	if s.udp != nil {
		_ = s.udp.Close()
	}
	if s.udp6 != nil {
		_ = s.udp6.Close()
	}
	s.wg.Wait()
}

func (s *Service) announceChange() {
	if s.options.Changed != nil {
		s.options.Changed()
	}
}

func (s *Service) UpdateProfile(alias, network string) {
	alias = cleanAlias(alias)
	s.mu.Lock()
	networkChanged := s.network != network
	changed := s.alias != alias || networkChanged
	if networkChanged {
		clear(s.peers)
	}
	s.alias, s.network = alias, network
	s.mu.Unlock()
	if networkChanged {
		s.StopLink()
		s.announceChange()
	}
	if changed {
		s.Refresh()
	}
}

func (s *Service) SetPIN(pin string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.PIN = pin
	clear(s.usePIN)
}

func (s *Service) checkPIN(w http.ResponseWriter, r *http.Request, link *webLink) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	pin, attempts := s.cfg.PIN, s.usePIN
	if link != nil {
		pin, attempts = link.pin, link.attempts
	}
	if pin == "" {
		return true
	}
	ip := remoteIP(r)
	now := time.Now()
	state := attempts[ip]
	if now.After(state.until) {
		state = pinState{until: now.Add(3 * time.Minute)}
	}
	if state.failed >= 3 {
		fail(w, http.StatusTooManyRequests, "Too many requests")
		return false
	}
	received := r.URL.Query().Get("pin")
	if subtle.ConstantTimeCompare([]byte(received), []byte(pin)) != 1 {
		if received != "" {
			state.failed++
			if len(attempts) >= 256 {
				for addr, older := range attempts {
					if now.After(older.until) {
						delete(attempts, addr)
					}
				}
			}
			if _, ok := attempts[ip]; ok || len(attempts) < 256 {
				attempts[ip] = state
			}
		}
		fail(w, http.StatusUnauthorized, "PIN required or invalid")
		return false
	}
	delete(attempts, ip)
	return true
}

func (s *Service) View() (domain.LocalSendStatus, []domain.Transfer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	view := domain.LocalSendStatus{ProgressRevision: s.progressRevision, Active: !s.closed, Port: s.port, HasPIN: s.cfg.PIN != "", Problem: s.problem, Alias: s.alias}
	for _, p := range s.peers {
		view.Devices = append(view.Devices, p.LocalSendDevice)
	}
	sort.Slice(view.Devices, func(i, j int) bool {
		a, b := view.Devices[i], view.Devices[j]
		if a.Name == b.Name {
			return a.ID < b.ID
		}
		return a.Name < b.Name
	})
	if s.incoming != nil && !s.incoming.approved {
		view.Offer = s.incoming.offer()
	}
	if s.link != nil {
		view.Link = s.link.view
		view.Link.QR = slices.Clone(view.Link.QR)
		if pending := s.link.pending; pending != nil {
			view.Link.RequestID, view.Link.RequestFrom = pending.id, pending.ip
		}
	}
	rows := make([]domain.Transfer, 0, len(s.transfers))
	for _, row := range s.transfers {
		row.LocalSendItems = slices.Clone(row.LocalSendItems)
		rows = append(rows, row)
	}
	return view, rows
}

func (s *Service) update(row domain.Transfer) {
	row.LocalSendItems = slices.Clone(row.LocalSendItems)
	row.UpdatedAt = time.Now()
	s.mu.Lock()
	s.transfers[row.ID] = row
	s.mu.Unlock()
	if s.options.Persist != nil {
		s.options.Persist(row)
	}
	s.announceChange()
	if terminal(row.Status) {
		s.mu.Lock()
		delete(s.transfers, row.ID)
		s.mu.Unlock()
	}
}

func (s *Service) progress(id string, count int64, speed float64) {
	s.mu.Lock()
	row, ok := s.transfers[id]
	if ok && row.Status == "transferring" {
		row.Done, row.Speed = count, speed
		s.progressRevision++
		s.transfers[id] = row
	} else {
		ok = false
	}
	s.mu.Unlock()
	if ok {
		s.announceChange()
	}
}

func terminal(status string) bool {
	switch status {
	case "completed", "cancelled", "rejected", "failed", "partial":
		return true
	default:
		return false
	}
}

func (s *Service) httpServer(link *webLink) *http.Server {
	mux := http.NewServeMux()
	if link == nil {
		mux.HandleFunc("GET "+prefix+"info", s.serveInfo)
		mux.HandleFunc("POST "+prefix+"register", s.register)
	}
	if link == nil || link.view.Upload {
		mux.HandleFunc("POST "+prefix+"prepare-upload", func(w http.ResponseWriter, r *http.Request) { s.prepareUpload(w, r, link) })
		mux.HandleFunc("POST "+prefix+"upload", func(w http.ResponseWriter, r *http.Request) { s.uploadFile(w, r, link) })
		mux.HandleFunc("POST "+prefix+"cancel", func(w http.ResponseWriter, r *http.Request) { s.cancelRequest(w, r, link) })
	}
	if link != nil {
		s.webRoutes(mux, link)
	}
	return &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    24 << 10,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{s.cert}, ClientAuth: tls.RequestClientCert},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				fail(w, http.StatusServiceUnavailable, "Server stopped")
				return
			}
			s.wg.Add(1)
			s.mu.Unlock()
			defer s.wg.Done()
			if !s.originAllowed(remoteIP(r)) {
				fail(w, http.StatusForbidden, "LAN only")
				return
			}
			mux.ServeHTTP(w, r)
		}),
	}
}

func cleanAlias(alias string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "GoIM"
	}
	if len(alias) > 256 {
		for i := range alias {
			if i >= 252 {
				return alias[:i]
			}
		}
	}
	return alias
}

func (s *Service) PauseReceive() {
	s.StopLink()
	s.mu.Lock()
	in := s.incoming
	s.mu.Unlock()
	if in != nil {
		s.Cancel(in.id)
	}
}
