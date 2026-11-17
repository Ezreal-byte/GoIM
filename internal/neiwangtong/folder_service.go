package neiwangtong

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

var ErrFolderConnectTimeout = errors.New("内网通没有建立文件夹连接")

type directoryKey struct {
	ip   netip.Addr
	task string
}

type directoryReceive struct {
	ctx      context.Context
	offer    FolderOffer
	dest     string
	progress func(int64)
	result   chan error
	active   bool
}

type directorySend struct {
	peer      string
	directory Directory
	ctx       context.Context
	cancel    context.CancelFunc
	active    bool
}

type directoryTransport struct {
	listener *net.TCPListener
	mu       sync.Mutex
	wait     map[directoryKey]*directoryReceive
	sends    map[string]*directorySend
	slots    chan struct{}
}

func startDirectories(bind netip.Addr) (*directoryTransport, error) {
	if !bind.IsValid() {
		bind = netip.IPv4Unspecified()
	}
	listener, err := net.ListenTCP("tcp4", net.TCPAddrFromAddrPort(netip.AddrPortFrom(bind, 0)))
	if err != nil {
		return nil, err
	}
	d := &directoryTransport{listener: listener, wait: make(map[directoryKey]*directoryReceive),
		sends: make(map[string]*directorySend), slots: make(chan struct{}, 16)}
	go d.listen()
	return d, nil
}

func (d *directoryTransport) port() uint16 {
	return uint16(d.listener.Addr().(*net.TCPAddr).Port)
}

func (d *directoryTransport) listen() {
	for {
		conn, err := d.listener.AcceptTCP()
		if err != nil {
			return
		}
		select {
		case d.slots <- struct{}{}:
			go func() {
				defer func() { _ = conn.Close(); <-d.slots }()
				d.receive(conn)
			}()
		default:
			_ = conn.Close()
		}
	}
}

func (d *directoryTransport) receive(conn *net.TCPConn) {
	init, err := requireFolderFrame(conn, 1, 684)
	if err != nil {
		return
	}
	task := strings.ToLower(readFolderString(init[folderTaskStart:folderTaskEnd], binary.BigEndian))
	key := directoryKey{conn.RemoteAddr().(*net.TCPAddr).AddrPort().Addr().Unmap(), task}
	d.mu.Lock()
	in := d.wait[key]
	if in == nil || in.active || in.ctx.Err() != nil {
		d.mu.Unlock()
		return
	}
	in.active = true
	d.mu.Unlock()
	in.result <- receiveDirectory(in.ctx, conn, init, in.offer, in.dest, in.progress)
}

func (s *Service) ReceiveDirectory(ctx context.Context, peerID string, offer FolderOffer, destination string, progress func(int64)) error {
	if s.directories == nil || !validFolderOffer(offer) {
		return errors.New("内网通文件夹接收未就绪")
	}
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	offer.TaskID = strings.ToLower(offer.TaskID)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	d := s.directories
	in := &directoryReceive{ctx: ctx, offer: offer, dest: destination, progress: progress, result: make(chan error, 1)}
	key := directoryKey{control.Addr(), offer.TaskID}
	d.mu.Lock()
	if len(d.wait) >= cap(d.slots) || d.wait[key] != nil {
		d.mu.Unlock()
		return errors.New("文件夹接收任务已在等待或达到上限")
	}
	d.wait[key] = in
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.wait, key)
		d.mu.Unlock()
	}()
	if err := s.AcceptFolderOffer(peerID, offer.TaskID); err != nil {
		return err
	}
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()
	select {
	case err := <-in.result:
		return err
	case <-timeout.C:
		d.mu.Lock()
		active := in.active
		if !active {
			delete(d.wait, key)
		}
		d.mu.Unlock()
		if !active {
			return ErrFolderConnectTimeout
		}
	case <-ctx.Done():
	}
	// If cancellation races a disk write, wait for the socket worker to exit
	// before the caller removes its staging directory.
	d.mu.Lock()
	active := in.active
	if !active {
		delete(d.wait, key)
	}
	d.mu.Unlock()
	if !active {
		return ctx.Err()
	}
	return <-in.result
}

func (s *Service) SendDirectory(peerID, taskID string, directory Directory) error {
	if s.directories == nil || !validNativeID(taskID) || len(directory.items) == 0 {
		return errors.New("内网通文件夹发送未就绪")
	}
	if _, err := s.directoryRemote(peerID); err != nil {
		return err
	}
	taskID = strings.ToLower(taskID)
	d := s.directories
	d.mu.Lock()
	if saved := d.sends[taskID]; saved != nil {
		same := saved.peer == peerID && saved.directory.Stamp == directory.Stamp && saved.directory.Path == directory.Path
		d.mu.Unlock()
		if !same {
			return errors.New("内网通文件夹任务编号已被占用")
		}
		return s.SendFolderOffer(peerID, directory.offer(taskID))
	}
	if len(d.sends) >= 1024 {
		d.mu.Unlock()
		return errors.New("内网通待接收文件夹数量已达上限")
	}
	ctx, cancel := context.WithCancel(s.ctx)
	up := &directorySend{peer: peerID, directory: directory, ctx: ctx, cancel: cancel}
	d.sends[taskID] = up
	d.mu.Unlock()
	err := s.SendFolderOffer(peerID, directory.offer(taskID))
	if err != nil && !errors.Is(err, ErrDeliveryUncertain) {
		d.finishSend(taskID, up)
	}
	return err
}

func (s *Service) directoryRemote(peerID string) (netip.AddrPort, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.peers[peerID]
	if p == nil || !p.peer.Online {
		return netip.AddrPort{}, ErrNotConnected
	}
	if !validUnicast(p.folderTCP) {
		return netip.AddrPort{}, errors.New("对方未提供内网通文件夹端口")
	}
	return p.folderTCP, nil
}

func (d *directoryTransport) finishSend(taskID string, sender *directorySend) {
	d.mu.Lock()
	if d.sends[taskID] == sender {
		delete(d.sends, taskID)
	}
	d.mu.Unlock()
	sender.cancel()
}

func (s *Service) onDirectoryOperation(peerID string, kind uint32, data []byte) {
	root := "X_OPERATE_RECV_FOLDER"
	if kind == CmdOperateSendFolder {
		root = "X_OPERATE_SEND_FOLDER"
	}
	var op struct {
		Task string `xml:"TASK_ID"`
		Code uint32 `xml:"OP"`
	}
	if err := decodeNativeXML(data, root, &op); err != nil || !validNativeID(op.Task) {
		return
	}
	task := strings.ToLower(op.Task)
	if kind == CmdOperateSendFolder {
		if op.Code == 1 && s.cfg.Callbacks.FolderCancelled != nil {
			s.cfg.Callbacks.FolderCancelled(peerID, task)
		}
		return
	}
	d := s.directories
	if d == nil || (op.Code != 2 && op.Code != 4) {
		return
	}
	d.mu.Lock()
	up := d.sends[task]
	if up == nil || up.peer != peerID {
		d.mu.Unlock()
		return
	}
	switch op.Code {
	case 2:
		if up.active {
			d.mu.Unlock()
			return
		}
		up.active = true
		d.mu.Unlock()
		go s.uploadDirectory(task, up)
	case 4:
		err := ErrFileRejected
		if up.active {
			err = ErrFileCancelled
		}
		delete(d.sends, task)
		d.mu.Unlock()
		up.cancel()
		s.reportDirectory(peerID, task, 0, up.directory.Size, false, err)
	}
}

func (s *Service) uploadDirectory(task string, up *directorySend) {
	defer s.directories.finishSend(task, up)
	target, err := s.directoryRemote(up.peer)
	var done int64
	if err == nil {
		var conn net.Conn
		conn, err = dialTransfer(up.ctx, s.cfg.BindIP, target)
		if err == nil {
			defer conn.Close()
			var last time.Time
			err = sendDirectory(up.ctx, conn, task, up.directory, func(bytes int64) {
				done = bytes
				if time.Since(last) >= 150*time.Millisecond {
					last = time.Now()
					s.reportDirectory(up.peer, task, done, up.directory.Size, false, nil)
				}
			})
		}
	}
	if up.ctx.Err() != nil {
		return
	}
	if err != nil {
		err = fmt.Errorf("内网通文件夹传输失败: %w", err)
	}
	s.reportDirectory(up.peer, task, done, up.directory.Size, err == nil, err)
}

func (s *Service) reportDirectory(peer, task string, done, total int64, completed bool, err error) {
	if cb := s.cfg.Callbacks.FolderProgress; cb != nil {
		cb(peer, task, done, total, completed, err)
	}
}

func (s *Service) CancelDirectorySend(peerID, task string) error {
	if d := s.directories; d != nil {
		d.mu.Lock()
		if up := d.sends[task]; up != nil && up.peer == peerID {
			delete(d.sends, task)
			up.cancel()
		}
		d.mu.Unlock()
	}
	return s.folderTaskCommand(peerID, task, CmdOperateSendFolder, "X_OPERATE_SEND_FOLDER", "<OP>1</OP>")
}

func (s *Service) StopDirectoryReceive(peerID, task string) error {
	return s.folderTaskCommand(peerID, task, CmdOperateRecvFolder, "X_OPERATE_RECV_FOLDER", "<OP>4</OP>")
}
