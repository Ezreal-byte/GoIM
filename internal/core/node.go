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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	bolt "go.etcd.io/bbolt"
	"goim/internal/domain"
	ipmsgproto "goim/internal/ipmsg"
	"goim/internal/localsend"
	"goim/internal/neiwangtong"
	"goim/internal/printing"
	"goim/internal/systemauth"
)

type Options struct {
	DataDir              string
	Listen               string
	Name                 string
	DisableDiscovery     bool
	DisableIPMSG         bool
	DisableCompatibility bool
	Version              string
	DownloadDir          string
	PrintBackend         printing.Backend
}
type Node struct {
	connectionFeedback    domain.ConnectionFeedback
	dataResetPending      bool
	dataLock              *os.File
	mu                    sync.Mutex
	groupMu               sync.Mutex
	flushing              map[string]*peerDelivery
	nativeAvatarFetches   map[string]*nativeAvatarDownload
	data                  persisted
	db                    *bolt.DB
	key                   ed25519.PrivateKey
	tls                   *tls.Config
	dir                   string
	ctx                   context.Context
	cancel                context.CancelFunc
	listener              net.Listener
	localIP               string
	connections           map[string]*peerConn
	incoming              map[*tls.Conn]struct{}
	dialing               map[string]bool
	commands              chan domain.Command
	updates               chan struct{}
	wg                    sync.WaitGroup
	workers               sync.WaitGroup
	closing               bool
	shareSessions         map[string]domain.ShareSession
	shareWorker           bool
	chatDraftsSending     map[string]bool // protected by mu
	transfers             map[string]context.CancelFunc
	discovery             bool
	discoveryWake         chan struct{}
	manualDiscovery       chan struct{}
	contactRefresh        domain.ContactRefresh
	dnsDirectory          *dnsDirectory
	relayRoutes           map[string]map[string]bool
	webServer             *http.Server
	memberWebServers      map[string]*http.Server // additional local interfaces used by direct peers
	webAddress            string
	webKey                [32]byte
	webSlots              chan struct{}
	webCatalogVersion     uint64
	webCatalogChange      chan struct{}
	shareScanMu           sync.Mutex
	webLinks              map[string]shareLink
	webHomeLink           shareLink
	webUploads            chan struct{}
	webUploadMu           sync.Mutex
	shareFeedback         domain.ShareFeedback
	unlockAfter           time.Time
	lockResult            domain.LockResult
	legacy                *ipmsgproto.Service
	legacyLive            *atomic.Bool
	neiwangtong           *neiwangtong.Service
	neiwangtongLive       *atomic.Bool
	compatMu              sync.Mutex
	compatPeers           map[compatibilityEndpoint]domain.Peer
	ipmsgStatus           domain.CompatibilityStatus
	neiwangtongStatus     domain.CompatibilityStatus
	ipmsgOverride         bool
	compatibilityDisabled bool
	compatScheduled       bool
	version               string
	printBackend          printing.Backend
	printPollDelay        time.Duration
	printPollLimit        time.Duration
	localPrinters         []domain.LocalPrinter
	printersLoading       bool
	printError            string
	printCancelMu         sync.Mutex // Serialize spooler cancellation against terminal status updates.
	printPolls            map[string]bool
	printSubmitTokens     chan struct{}
	printAuthWaiters      map[string]printerAuthWaiter
	printAuthActive       map[string]bool
	printAuthResults      map[string]domain.PrinterAuthResult
	printAuthAfter        map[string]time.Time
	voice                 *voiceSession

	localSend                      atomic.Pointer[localsend.Service]
	localSendMu                    sync.Mutex
	localSendPIN, localSendProblem string
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
	dataLock, err := lockDataDirectory(opts.DataDir)
	if err != nil {
		return nil, err
	}
	retainedLock := false
	defer func() {
		if !retainedLock {
			_ = dataLock.Close()
		}
	}()
	if err := completePendingDataReset(opts.DataDir); err != nil {
		return nil, err
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
	printBackend := opts.PrintBackend
	if printBackend == nil {
		printBackend = printing.System{}
	}
	n := &Node{
		dataLock: dataLock,
		data:     data, db: db, key: key, tls: tlsConfig, dir: opts.DataDir, ctx: ctx, cancel: cancel,
		listener: listener, connections: make(map[string]*peerConn), dialing: make(map[string]bool),
		incoming: make(map[*tls.Conn]struct{}),
		commands: make(chan domain.Command, 256), updates: make(chan struct{}, 1),
		transfers: make(map[string]context.CancelFunc), discovery: !opts.DisableDiscovery,
		manualDiscovery: make(chan struct{}, 1), discoveryWake: make(chan struct{}, 1), relayRoutes: make(map[string]map[string]bool),
		printBackend: printBackend, printPollDelay: 900 * time.Millisecond,
		printPollLimit: 30 * time.Minute, printPolls: make(map[string]bool),
		printSubmitTokens: make(chan struct{}, 2),
	}
	n.hydrateGroupAvatars(&n.data.State)
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
	settings := &n.data.State.Settings
	if !systemauth.Supported() {
		settings.SystemUnlock = false
	}
	settings.InterfaceFontSize = domain.NormalizeInterfaceFontSize(settings.InterfaceFontSize)
	settings.EnsureVoiceDefaults()
	if settings.IPMSG.Port == 0 {
		settings.IPMSG = domain.CompatibilityConfig{Enabled: true, Port: domain.IPMSGPort}
	}
	if settings.Neiwangtong.Port == 0 {
		settings.Neiwangtong = domain.CompatibilityConfig{Enabled: true, Port: domain.NeiwangtongDiscoveryPort}
	}
	settings.CompatibilityConfigured = true
	settings.CompatibilityDefaultsVersion = 1
	removeUnverifiedBridges(&n.data)
	for _, peer := range n.data.State.Peers {
		migrateContactPreferences(&n.data, peer.ID, peer.CompatibilityAliasOf)
	}
	retireUnboundIPMSGDeliveries(&n.data)
	n.ipmsgOverride, n.compatibilityDisabled, n.version = opts.DisableIPMSG, opts.DisableCompatibility, opts.Version
	n.hideUnusedDirectConversations()
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
	n.data.State.Self = domain.Peer{ID: keyID(key.Public().(ed25519.PublicKey)), Name: n.data.State.Settings.Nickname, Avatar: n.data.State.Settings.Avatar, PublicKey: append([]byte(nil), key.Public().(ed25519.PublicKey)...), Address: listener.Addr().String(), Online: true, VoiceSupported: true}
	n.data.State.Self.PrintSupported = true
	n.data.State.Self.Signature = n.data.State.Settings.Signature
	n.data.State.Self.Presence = n.data.State.Settings.Presence
	n.data.State.Self.Details = n.data.State.Settings.ProfileDetails
	if fields := strings.Fields(opts.Version); len(fields) > 0 {
		n.data.State.Self.Version = fields[0]
	}
	if bound, ok := listener.Addr().(*net.TCPAddr); ok {
		n.localIP = browserIP(bound, n.data.State.Settings.Network).String()
	}
	// Unattended browser uploads are always disabled on startup.
	n.data.WebAutoUntil = time.Time{}
	n.data.State.ListenAddress = listener.Addr().String()
	n.data.State.Version = opts.Version
	for i := range n.data.State.Peers {
		n.data.State.Peers[i].Online = false
	}
	migratePartials := n.data.PartialCleanup == nil
	if migratePartials {
		n.data.PartialCleanup = make(map[string]bool)
	}
	for i := range n.data.State.Transfers {
		t := &n.data.State.Transfers[i]
		if t.LegacyTransport == domain.TransportLocalSend {
			t.LocalSendCanRetry, t.LocalSendNeedsPIN = false, false
			if t.Status == "offered" || t.Status == "transferring" || t.Status == "waiting" {
				t.Status, t.Error, t.Speed = "failed", "应用退出，传输已中断", 0
			}
			continue
		}
		if t.WebSender != "" {
			recoverWebUpload(t)
			continue
		}
		if t.ShareProject != "" {
			if _, ok := n.data.ReceivePublications[t.ID]; !ok {
				resetShareDownload(t)
			}
			continue
		}
		if migratePartials {
			n.data.queuePartialCleanup(*t)
		}
		if t.Status == "transferring" || t.Status == "verifying" {
			t.Status = "waiting"
			t.Speed = 0
		}
	}
	n.discardIdleReceivePartialsLocked()
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
	n.initializeCompatibility()
	if settings.LocalSend.Enabled {
		_ = n.applyLocalSend(settings.LocalSend)
	}
	n.wg.Add(3)
	go n.acceptLoop()
	go n.commandLoop()
	go n.discoveryLoop()
	for _, t := range n.snapshot(false).Transfers {
		if _, pending := n.receivePublication(t.ID); pending {
			n.startReceive(t.ID, "")
			continue
		}
		if t.FolderStage != "" {
			// The folder invitation is usable again once its sender
			// reconnects. Peer replay starts the saved download.
			continue
		}
		if t.Direction == "receive" && t.Status == "waiting" && t.Path != "" {
			n.startReceive(t.ID, "")
		}
	}
	n.resumePrintJobs()
	n.scheduleShareCheck()
	retainedLock = true
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
	for i := range s.Peers {
		peer := &s.Peers[i]
		id := peer.ID
		if peer.CompatibilityAliasOf != "" {
			id = peer.CompatibilityAliasOf
		}
		peer.Remark = n.data.PeerRemarks[id]
		peer.ContactHidden = n.data.ContactVisibility[id] == contactHidden
		peer.ContactDeleted = n.data.ContactVisibility[id] == contactDeleted
	}
	s.ContactRefresh = n.contactRefresh
	n.addLocalSendSnapshot(&s)
	s.DiscoveryDisabled = !n.discovery
	s.LockResult = n.lockResult
	s.ConnectionFeedback = n.connectionFeedback
	if n.dnsActiveLocked() {
		s.DNSDiscoveryStatus = n.dnsDirectory.status
	}
	s.DataResetPending = n.dataResetPending
	s.LocalIP = n.localIP
	s.LocalPrinters = append([]domain.LocalPrinter(nil), n.localPrinters...)
	s.PrintersLoading = n.printersLoading
	s.PrintError = n.printError
	s.PrinterAuth = make(map[string]domain.PrinterAuthResult, len(n.printAuthResults))
	for id, result := range n.printAuthResults {
		s.PrinterAuth[id] = result
	}
	s.PrinterAuthorized = make(map[string]string)
	for id, access := range n.data.PrintAccess {
		share := s.Printer(id)
		if access.OwnerID == share.OwnerID && access.Revision == share.AuthRevision && access.Token != "" {
			s.PrinterAuthorized[id] = access.Revision
		}
	}
	s.ShareFeedback = n.shareFeedback
	s.IPMSGStatus = n.ipmsgStatus
	if s.IPMSGStatus.Pending != nil {
		pending := *s.IPMSGStatus.Pending
		s.IPMSGStatus.Pending = &pending
	}
	s.NeiwangtongStatus = n.neiwangtongStatus
	if s.NeiwangtongStatus.Pending != nil {
		pending := *s.NeiwangtongStatus.Pending
		s.NeiwangtongStatus.Pending = &pending
	}
	n.hydrateGroupAvatars(&s)
	n.attachShareLinks(&s, urls)
	if urls {
		n.attachMemberProjects(&s)
	}
	if n.voice != nil {
		s.VoiceCall = n.voice.call
	}
	return s
}
func (n *Node) Submit(c domain.Command) {
	var err error
	select {
	case n.commands <- c:
		return
	case <-n.ctx.Done():
		err = n.ctx.Err()
	default:
		err = errors.New("操作队列已满，请稍后重试")
		n.report(err)
	}
	if c.OnResult != nil {
		c.OnResult(err)
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
	n.compatMu.Lock()
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		n.compatMu.Unlock()
		return nil
	}
	n.closing = true
	n.cancel()
	n.listener.Close()
	web := n.webServer
	memberServers := n.memberWebServers
	for _, c := range n.connections {
		c.conn.Close()
	}
	for _, cancel := range n.transfers {
		cancel()
	}
	n.closeVoiceLocked()
	legacy := n.legacy
	if n.legacyLive != nil {
		n.legacyLive.Store(false)
	}
	n.legacy = nil
	nwt := n.neiwangtong
	if n.neiwangtongLive != nil {
		n.neiwangtongLive.Store(false)
	}
	n.neiwangtong = nil
	n.mu.Unlock()
	n.compatMu.Unlock()
	if legacy != nil {
		legacy.Close()
	}
	if nwt != nil {
		_ = nwt.Close()
	}
	if web != nil {
		_ = web.Close()
	}
	for _, server := range memberServers {
		_ = server.Close()
	}
	n.stopLocalSend()
	n.wg.Wait()
	n.workers.Wait()
	n.mu.Lock()
	defer n.mu.Unlock()
	for i := range n.data.State.Peers {
		n.data.State.Peers[i].Online = false
	}
	err := saveStore(n.db, &n.data)
	e := n.db.Close()
	if e == nil {
		e = completePendingDataReset(n.dir)
	}
	if n.dataLock != nil {
		if lockErr := n.dataLock.Close(); e == nil {
			e = lockErr
		}
	}
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
			if c.Kind == domain.SendChatDraft {
				if !n.async(func() { n.completeCommand(c, n.execute(c)) }) {
					n.completeCommand(c, context.Canceled)
				}
				continue
			}
			n.completeCommand(c, n.execute(c))
		case <-n.ctx.Done():
			return
		}
	}
}
func (n *Node) completeCommand(c domain.Command, err error) {
	if err != nil {
		n.report(err)
	}
	if c.OnResult != nil {
		c.OnResult(err)
	}
}

func (n *Node) acceptLoop() {
	defer n.wg.Done()
	for {
		c, err := n.listener.Accept()
		if err != nil {
			return
		}
		conn := tls.Server(c, n.tls)
		n.mu.Lock()
		admitted := !n.closing && len(n.incoming) < maxPendingPeerConnections
		if admitted {
			n.incoming[conn] = struct{}{}
		}
		n.mu.Unlock()
		if !admitted {
			conn.Close()
			continue
		}
		if !n.async(func() {
			defer func() { n.mu.Lock(); delete(n.incoming, conn); n.mu.Unlock() }()
			n.establish(conn, false, "")
		}) {
			conn.Close()
			n.mu.Lock()
			delete(n.incoming, conn)
			n.mu.Unlock()
		}
	}
}
func (n *Node) Connect(address string) {
	if err := n.connectVia(domain.TransportGoIM, address); err != nil {
		n.report(err)
	}
}

func (n *Node) dialTo(address, expected string, quiet bool) {
	n.dialToContext(n.ctx, address, expected, quiet)
}

func (n *Node) dialToContext(ctx context.Context, address, expected string, quiet bool) {
	address = strings.TrimSpace(address)
	if _, _, e := net.SplitHostPort(address); e != nil {
		address = net.JoinHostPort(address, "47842")
	}
	n.mu.Lock()
	if n.dialing[address] || n.closing || ctx.Err() != nil {
		n.mu.Unlock()
		return
	}
	n.dialing[address] = true
	n.mu.Unlock()
	n.async(func() {
		defer func() { n.mu.Lock(); delete(n.dialing, address); n.mu.Unlock() }()
		if err := n.connectGoIMContext(ctx, address, expected); err != nil && !quiet {
			n.report(fmt.Errorf("连接 %s 失败: %w", address, err))
		}
	})
}

func (n *Node) connectGoIM(address, expected string) error {
	return n.connectGoIMContext(n.ctx, address, expected)
}

func (n *Node) connectGoIMContext(parent context.Context, address, expected string) error {
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	d := net.Dialer{}
	ip, err := n.selectedIPFor(address)
	if err != nil {
		return err
	}
	if ip != nil {
		d.LocalAddr = &net.TCPAddr{IP: ip}
	}
	raw, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	ready := make(chan bool, 1)
	if !n.async(func() { n.establishWithResult(ctx, tls.Client(raw, n.tls.Clone()), true, expected, ready) }) {
		raw.Close()
		return context.Canceled
	}
	select {
	case ok := <-ready:
		if ok {
			return nil
		}
		return errors.New("未完成 GoIM TLS 握手")
	case <-ctx.Done():
		_ = raw.Close()
		return ctx.Err()
	}
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
