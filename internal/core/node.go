package core

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
	"goim/internal/domain"
	ipmsgproto "goim/internal/ipmsg"
)

type Options struct {
	DataDir          string
	Listen           string
	Name             string
	DisableDiscovery bool
	DisableIPMSG     bool
	Version          string
	DownloadDir      string
}
type Node struct {
	mu            sync.Mutex
	groupMu       sync.Mutex
	flushMu       sync.Mutex
	data          persisted
	db            *bolt.DB
	key           ed25519.PrivateKey
	tls           *tls.Config
	dir           string
	ctx           context.Context
	cancel        context.CancelFunc
	listener      net.Listener
	localIP       string
	connections   map[string]*peerConn
	dialing       map[string]bool
	commands      chan domain.Command
	updates       chan struct{}
	wg            sync.WaitGroup
	workers       sync.WaitGroup
	closing       bool
	transfers     map[string]context.CancelFunc
	discovery     bool
	webServer     *http.Server
	webAddress    string
	webKey        [32]byte
	webSlots      chan struct{}
	webLinks      map[string]shareLink
	webHomeLink   shareLink
	webUploads    chan struct{}
	webUploadMu   sync.Mutex
	shareFeedback domain.ShareFeedback
	unlockAfter   time.Time
	lockResult    domain.LockResult
	legacy        *ipmsgproto.Service
}

func New(opts Options) (*Node, error) {
	if opts.DataDir == "" {
		root, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		opts.DataDir = filepath.Join(root, "GoIM")
	}
	if opts.Listen == "" {
		opts.Listen = "0.0.0.0:47842"
	}
	db, data, err := openStore(opts.DataDir)
	if err != nil {
		return nil, err
	}
	key, err := loadIdentity(opts.DataDir)
	if err != nil {
		db.Close()
		return nil, err
	}
	tlsConfig, err := tlsIdentity(key)
	if err != nil {
		db.Close()
		return nil, err
	}
	listener, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		db.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{data: data, db: db, key: key, tls: tlsConfig, dir: opts.DataDir, ctx: ctx, cancel: cancel, listener: listener, connections: make(map[string]*peerConn), dialing: make(map[string]bool), commands: make(chan domain.Command, 256), updates: make(chan struct{}, 1), transfers: make(map[string]context.CancelFunc), discovery: !opts.DisableDiscovery}
	newSettings := n.data.State.Settings.Theme == ""
	if newSettings {
		downloads := filepath.Join(opts.DataDir, "downloads")
		if home, e := os.UserHomeDir(); e == nil {
			downloads = filepath.Join(home, "Downloads", "GoIM")
		}
		n.data.State.Settings = domain.DefaultSettings(downloads)
	} else {
		n.data.State.Settings.EnsureDefaultShortcuts()
	}
	n.data.State.Settings.HasLockPassword = n.data.LockCredential.set()
	n.data.State.Locked = n.data.State.Settings.HasLockPassword && (n.data.State.Locked || n.data.State.Settings.LockOnStartup)
	if opts.Name != "" {
		n.data.State.Settings.Nickname = opts.Name
	}
	if opts.DownloadDir != "" {
		n.data.State.Settings.DownloadDir = opts.DownloadDir
	}
	if newSettings && opts.Name == "" && n.data.State.Settings.Nickname == "我" {
		if host, e := os.Hostname(); e == nil {
			n.data.State.Settings.Nickname = host
		}
	}
	n.data.State.Self = domain.Peer{ID: keyID(key.Public().(ed25519.PublicKey)), Name: n.data.State.Settings.Nickname, Avatar: n.data.State.Settings.Avatar, PublicKey: append([]byte(nil), key.Public().(ed25519.PublicKey)...), Address: listener.Addr().String(), Online: true}
	n.data.State.Self.Signature = n.data.State.Settings.Signature
	n.data.State.Self.Presence = n.data.State.Settings.Presence
	n.data.State.Self.Details = n.data.State.Settings.ProfileDetails
	if fields := strings.Fields(opts.Version); len(fields) > 0 {
		n.data.State.Self.Version = fields[0]
	}
	if bound, ok := listener.Addr().(*net.TCPAddr); ok {
		n.localIP = browserIP(bound, n.data.State.Settings.Network).String()
	}
	n.data.State.ListenAddress = listener.Addr().String()
	n.data.State.Version = opts.Version
	for i := range n.data.State.Peers {
		n.data.State.Peers[i].Online = false
	}
	for i := range n.data.State.Transfers {
		t := &n.data.State.Transfers[i]
		if t.Status == "transferring" || t.Status == "verifying" {
			t.Status = "waiting"
			t.Speed = 0
		}
	}
	if err = saveStore(n.db, &n.data); err != nil {
		listener.Close()
		cancel()
		db.Close()
		return nil, err
	}
	if len(n.data.BrowserShares) != 0 || n.data.BrowserHomeToken != "" {
		if err := n.startShareServer(); err != nil {
			n.report(err)
		}
	}
	if !opts.DisableIPMSG {
		n.startIPMSG(opts.Version)
	}
	n.wg.Add(3)
	go n.acceptLoop()
	go n.commandLoop()
	go n.discoveryLoop()
	for _, t := range n.snapshot(false).Transfers {
		if t.Direction == "receive" && t.Status == "waiting" && t.Path != "" {
			n.startReceive(t.ID, "")
		}
	}
	return n, nil
}

func (n *Node) Address() string          { return n.listener.Addr().String() }
func (n *Node) DataDir() string          { return n.dir }
func (n *Node) Updates() <-chan struct{} { return n.updates }
func (n *Node) Snapshot() domain.State {
	return n.snapshot(true)
}
func (n *Node) snapshot(urls bool) domain.State {
	n.mu.Lock()
	defer n.mu.Unlock()
	// Snapshots are refreshed at event boundaries, never from the painter.
	b, _ := json.Marshal(n.data.State)
	var s domain.State
	_ = json.Unmarshal(b, &s)
	s.LockResult = n.lockResult
	s.LocalIP = n.localIP
	s.ShareFeedback = n.shareFeedback
	n.attachShareLinks(&s, urls)
	return s
}
func (n *Node) Submit(c domain.Command) {
	select {
	case n.commands <- c:
	case <-n.ctx.Done():
	default:
		n.report(errors.New("操作队列已满，请稍后重试"))
	}
}
func (n *Node) change(fn func(*domain.State)) error {
	return n.durable(func(p *persisted) []pending { fn(&p.State); return nil })
}
func (n *Node) signal() {
	select {
	case n.updates <- struct{}{}:
	default:
	}
}
func (n *Node) report(err error) {
	if err != nil {
		n.change(func(s *domain.State) { s.Error = err.Error() })
	}
}
func (n *Node) notice(t string) { n.change(func(s *domain.State) { s.Notice = t; s.Error = "" }) }
func (n *Node) async(fn func()) bool {
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		return false
	}
	n.workers.Add(1)
	n.mu.Unlock()
	go func() { defer n.workers.Done(); fn() }()
	return true
}
func (n *Node) Close() error {
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		return nil
	}
	n.closing = true
	n.cancel()
	n.listener.Close()
	web := n.webServer
	for _, c := range n.connections {
		c.conn.Close()
	}
	for _, cancel := range n.transfers {
		cancel()
	}
	legacy := n.legacy
	n.mu.Unlock()
	if legacy != nil {
		legacy.Close()
	}
	if web != nil {
		_ = web.Close()
	}
	n.wg.Wait()
	n.workers.Wait()
	n.mu.Lock()
	defer n.mu.Unlock()
	for i := range n.data.State.Peers {
		n.data.State.Peers[i].Online = false
	}
	err := saveStore(n.db, &n.data)
	e := n.db.Close()
	if err != nil {
		return err
	}
	return e
}
func (n *Node) commandLoop() {
	defer n.wg.Done()
	for {
		select {
		case c := <-n.commands:
			if err := n.execute(c); err != nil {
				n.report(err)
			}
		case <-n.ctx.Done():
			return
		}
	}
}
func (n *Node) acceptLoop() {
	defer n.wg.Done()
	for {
		c, err := n.listener.Accept()
		if err != nil {
			return
		}
		if !n.async(func() { n.establish(tls.Server(c, n.tls), false, "") }) {
			c.Close()
		}
	}
}
func (n *Node) Connect(address string) {
	address = strings.TrimSpace(address)
	if _, port, err := net.SplitHostPort(address); err == nil {
		if port == strconv.Itoa(ipmsgproto.DefaultPort) {
			n.async(func() {
				if err := n.connectLegacy(address); err != nil {
					n.report(err)
				}
			})
			return
		}
		n.dialTo(address, "", false)
		return
	}
	legacyAddress := ""
	if ip, err := netip.ParseAddr(address); err == nil && ip.Unmap().Is4() {
		n.mu.Lock()
		legacy := n.legacy
		n.mu.Unlock()
		if legacy != nil {
			legacyAddress = netip.AddrPortFrom(ip.Unmap(), ipmsgproto.DefaultPort).String()
		}
	}
	n.dialToAddress(address, "", false, legacyAddress)
}
func (n *Node) dialTo(address, expected string, quiet bool) {
	n.dialToAddress(address, expected, quiet, "")
}

func (n *Node) dialToAddress(address, expected string, quiet bool, legacyAddress string) {
	address = strings.TrimSpace(address)
	if _, _, e := net.SplitHostPort(address); e != nil {
		address = net.JoinHostPort(address, "47842")
	}
	n.mu.Lock()
	if n.dialing[address] || n.closing {
		n.mu.Unlock()
		return
	}
	n.dialing[address] = true
	n.mu.Unlock()
	n.async(func() {
		defer func() { n.mu.Lock(); delete(n.dialing, address); n.mu.Unlock() }()
		d := net.Dialer{Timeout: 4 * time.Second}
		if ip := n.selectedIP(); ip != nil {
			d.LocalAddr = &net.TCPAddr{IP: ip}
		}
		raw, err := d.DialContext(n.ctx, "tcp", address)
		if err != nil {
			if legacyAddress != "" && n.ctx.Err() == nil {
				if legacyErr := n.connectLegacy(legacyAddress); legacyErr == nil {
					return
				} else {
					n.report(legacyErr)
					return
				}
			}
			if !quiet {
				n.report(fmt.Errorf("连接 %s 失败: %w", address, err))
			}
			return
		}
		n.establish(tls.Client(raw, n.tls.Clone()), true, expected)
	})
}
func peerIndex(s *domain.State, id string) int {
	for i := range s.Peers {
		if s.Peers[i].ID == id {
			return i
		}
	}
	return -1
}
func convIndex(s *domain.State, id string) int {
	for i := range s.Conversations {
		if s.Conversations[i].ID == id {
			return i
		}
	}
	return -1
}
func groupIndex(s *domain.State, id string) int {
	for i := range s.Groups {
		if s.Groups[i].ID == id {
			return i
		}
	}
	return -1
}
func fileIndex(s *domain.State, id string) int {
	for i := range s.Files {
		if s.Files[i].ID == id {
			return i
		}
	}
	return -1
}
func transferIndex(s *domain.State, id string) int {
	for i := range s.Transfers {
		if s.Transfers[i].ID == id {
			return i
		}
	}
	return -1
}
func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
func without(ids []string, id string) []string {
	out := make([]string, 0, len(ids))
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}
func directID(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return "direct:" + a + ":" + b
}
func ensureDirect(s *domain.State, p domain.Peer) string {
	id := directID(s.Self.ID, p.ID)
	if convIndex(s, id) < 0 {
		s.Conversations = append(s.Conversations, domain.Conversation{ID: id, Title: p.Name, Kind: "direct", PeerID: p.ID, UpdatedAt: time.Now()})
	}
	return id
}
func ensureGroup(s *domain.State, g domain.Group) {
	id := "group:" + g.ID
	i := convIndex(s, id)
	if i < 0 {
		s.Conversations = append(s.Conversations, domain.Conversation{ID: id, Title: g.Name, Kind: "group", GroupID: g.ID, UpdatedAt: time.Now()})
	} else {
		s.Conversations[i].Title = g.Name
	}
}
