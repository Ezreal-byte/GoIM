package neiwangtong

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type OutgoingFile struct {
	TaskID    uint64 // application-level offer identifier
	FileID    uint64 // TCP selector advertised in X_SEND_FILE
	Path      string
	Size      int64
	Recipient netip.Addr
	Info      os.FileInfo
}

const maxFileSelector = math.MaxUint32

const (
	nativeReverseFileCommand = 100
	nativeReverseFileFrame   = 560
)

var ErrFileRejected = errors.New("对方拒绝了文件")

func validFileSize(size int64) bool { return size >= 0 && size <= math.MaxUint32-100 }

type TransferServer struct {
	listener      net.Listener
	port          int
	bindIP        netip.Addr
	reverse       net.Listener
	reversePort   int
	closing       atomic.Bool
	avatarMu      sync.RWMutex
	avatarPNG     []byte
	filesMu       sync.RWMutex
	files         map[uint64]OutgoingFile
	onProgress    func(taskID uint64, done, total int64)
	onReverse     func(fileID uint64, remote netip.Addr)
	reverseWaitMu sync.Mutex
	reverseWait   map[uint64]chan reverseSignal
	reverseMu     sync.Mutex
	reverseActive map[uint64]net.Conn
	avatarAllowed func(netip.Addr) bool
	active        sync.Map
	slots         chan struct{}
}

type reverseSignal struct {
	remote netip.Addr
	err    error
}

func StartTransferServer(bind netip.Addr, port int, onProgress func(taskID uint64, done, total int64)) (*TransferServer, error) {
	return startTransferServer(bind, port, onProgress, nil, nil)
}

func startTransferServer(bind netip.Addr, port int, onProgress func(taskID uint64, done, total int64), avatarAllowed func(netip.Addr) bool, onReverse func(fileID uint64, remote netip.Addr)) (*TransferServer, error) {
	bindStr := fmt.Sprintf(":%d", port)
	if bind.IsValid() && bind.Is4() {
		bindStr = fmt.Sprintf("%s:%d", bind.String(), port)
	}
	l, err := net.Listen("tcp4", bindStr)
	if err != nil {
		return nil, err
	}
	reverseBind := ":0"
	if bind.IsValid() && bind.Is4() {
		reverseBind = fmt.Sprintf("%s:0", bind.String())
	}
	reverse, err := net.Listen("tcp4", reverseBind)
	if err != nil {
		_ = l.Close()
		return nil, err
	}
	s := &TransferServer{
		listener:      l,
		port:          l.Addr().(*net.TCPAddr).Port,
		bindIP:        bind,
		reverse:       reverse,
		reversePort:   reverse.Addr().(*net.TCPAddr).Port,
		files:         make(map[uint64]OutgoingFile),
		onProgress:    onProgress,
		onReverse:     onReverse,
		reverseWait:   make(map[uint64]chan reverseSignal),
		reverseActive: make(map[uint64]net.Conn),
		avatarAllowed: avatarAllowed,
		slots:         make(chan struct{}, 16),
	}
	go s.acceptLoop()
	go s.acceptReverseLoop()
	return s, nil
}

func (s *TransferServer) Port() int {
	return s.port
}

func (s *TransferServer) ReversePort() int {
	return s.reversePort
}

func (s *TransferServer) Close() error {
	s.closing.Store(true)
	s.active.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
	listenerErr := s.listener.Close()
	reverseErr := s.reverse.Close()
	if listenerErr != nil {
		return listenerErr
	}
	return reverseErr
}

func (s *TransferServer) SetAvatar(png []byte) {
	s.avatarMu.Lock()
	defer s.avatarMu.Unlock()
	s.avatarPNG = append([]byte(nil), png...)
}

func (s *TransferServer) RegisterFile(taskID uint64, path string, size int64) {
	_ = s.registerFile(taskID, taskID, path, size, netip.Addr{})
}

func (s *TransferServer) registerFile(fileID, taskID uint64, path string, size int64, recipient netip.Addr) error {
	if fileID == 0 || fileID > maxFileSelector || taskID == 0 || !validFileSize(size) {
		return errors.New("invalid native file offer")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("源文件已变化")
	}
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	if len(s.files) >= 1024 {
		return errors.New("内网通待接收文件数量已达上限")
	}
	s.files[fileID] = OutgoingFile{TaskID: taskID, FileID: fileID, Path: path, Size: size, Recipient: recipient, Info: info}
	return nil
}

func (s *TransferServer) UnregisterFile(fileID uint64) {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	delete(s.files, fileID)
}

func (s *TransferServer) expectReverse(fileID uint64) <-chan reverseSignal {
	s.reverseWaitMu.Lock()
	defer s.reverseWaitMu.Unlock()
	if existing := s.reverseWait[fileID]; existing != nil {
		return existing
	}
	ready := make(chan reverseSignal, 1)
	s.reverseWait[fileID] = ready
	return ready
}

func (s *TransferServer) clearReverse(fileID uint64) {
	s.reverseWaitMu.Lock()
	delete(s.reverseWait, fileID)
	s.reverseWaitMu.Unlock()
}

func (s *TransferServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closing.Load() {
				return
			}
			continue
		}
		select {
		case s.slots <- struct{}{}:
			s.active.Store(conn, struct{}{})
			go func() {
				defer func() { s.active.Delete(conn); <-s.slots }()
				if s.closing.Load() {
					_ = conn.Close()
					return
				}
				s.handleConnection(conn)
			}()
		default:
			_ = conn.Close()
		}
	}
}

func (s *TransferServer) acceptReverseLoop() {
	for {
		conn, err := s.reverse.Accept()
		if err != nil {
			if s.closing.Load() {
				return
			}
			continue
		}
		select {
		case s.slots <- struct{}{}:
			s.active.Store(conn, struct{}{})
			go func() {
				defer func() { s.active.Delete(conn); <-s.slots }()
				defer conn.Close()
				if s.closing.Load() {
					return
				}
				s.handleReverseConnection(conn)
			}()
		default:
			_ = conn.Close()
		}
	}
}

func (s *TransferServer) handleConnection(conn net.Conn) {
	defer conn.Close()
	for {
		frame, err := readNativeTCPFrame(conn, 1024)
		if err != nil {
			return
		}
		selector, err := nativeFrameSelector(frame)
		if err != nil {
			return
		}
		cmd := binary.BigEndian.Uint32(frame[8:12])

		// Native 3.4 multiplexes requests on a pooled TCP connection. In
		// particular, it may fetch an avatar and then request a file without
		// reconnecting.
		switch cmd {
		case 1:
			if selector != avatarSelector {
				return
			}
			if addr, ok := conn.RemoteAddr().(*net.TCPAddr); !ok || s.avatarAllowed != nil && !s.avatarAllowed(addr.AddrPort().Addr().Unmap()) {
				return
			}
			s.serveAvatar(conn)
		case 3:
			s.serveFile(conn, selector)
			return
		default:
			return
		}
	}
}

func (s *TransferServer) handleReverseConnection(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	frame, err := readNativeTCPFrame(conn, 4096)
	if err != nil || binary.BigEndian.Uint32(frame[8:12]) != nativeReverseFileCommand {
		return
	}
	selector, err := nativeFrameSelector(frame)
	if err != nil {
		return
	}
	remoteAddr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return
	}
	remote := remoteAddr.AddrPort().Addr().Unmap()
	s.reverseMu.Lock()
	if previous := s.reverseActive[selector]; previous != nil && previous != conn {
		_ = previous.Close()
	}
	s.reverseActive[selector] = conn
	s.reverseMu.Unlock()
	defer func() {
		s.reverseMu.Lock()
		if s.reverseActive[selector] == conn {
			delete(s.reverseActive, selector)
		}
		s.reverseMu.Unlock()
	}()
	if s.onReverse != nil {
		s.onReverse(selector, remote)
	}
	s.reverseWaitMu.Lock()
	ready := s.reverseWait[selector]
	s.reverseWaitMu.Unlock()
	if ready != nil {
		select {
		case ready <- reverseSignal{remote: remote}:
		default:
		}
	}
	// The native sender keeps this bootstrap connection open until the
	// separate cmd=3 transfer finishes. Closing it immediately after cmd=100
	// makes the original client cancel the pending transfer.
	_ = conn.SetDeadline(time.Time{})
	_, _ = io.Copy(io.Discard, conn)
}

func (s *TransferServer) finishReverse(fileID uint64) {
	s.reverseMu.Lock()
	conn := s.reverseActive[fileID]
	delete(s.reverseActive, fileID)
	s.reverseMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func readNativeTCPFrame(conn net.Conn, maxLength uint32) ([]byte, error) {
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	var head [12]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(head[0:4])
	version := binary.BigEndian.Uint32(head[4:8])
	if version != 1 || length < 12 || length > maxLength {
		return nil, errors.New("invalid native TCP frame")
	}
	frame := make([]byte, length)
	copy(frame[:12], head[:])
	if _, err := io.ReadFull(conn, frame[12:]); err != nil {
		return nil, err
	}
	return frame, nil
}

func nativeFrameSelector(frame []byte) (uint64, error) {
	if len(frame) < 60 {
		return 0, errors.New("native TCP frame has no selector")
	}
	return uint64(binary.BigEndian.Uint32(frame[56:60])), nil
}

func (s *TransferServer) serveAvatar(conn net.Conn) {
	s.avatarMu.RLock()
	png := s.avatarPNG
	s.avatarMu.RUnlock()
	if len(png) == 0 {
		return
	}

	// 1. Send Command 2 confirmation (108 bytes)
	confirm := make([]byte, 108)
	binary.BigEndian.PutUint32(confirm[0:4], 108)
	binary.BigEndian.PutUint32(confirm[4:8], 1)
	binary.BigEndian.PutUint32(confirm[8:12], 2) // cmd 2
	binary.BigEndian.PutUint32(confirm[56:60], avatarSelector)
	if _, err := conn.Write(confirm); err != nil {
		return
	}

	// 2. Send Command 4 image data (100-byte header + png)
	frameLen := 100 + len(png)
	frame := make([]byte, frameLen)
	binary.BigEndian.PutUint32(frame[0:4], uint32(frameLen))
	binary.BigEndian.PutUint32(frame[4:8], 1)
	binary.BigEndian.PutUint32(frame[8:12], 4) // cmd 4
	binary.BigEndian.PutUint32(frame[56:60], avatarSelector)
	copy(frame[100:], png)
	_, _ = conn.Write(frame)
}

func (s *TransferServer) serveFile(conn net.Conn, taskID uint64) {
	s.filesMu.RLock()
	file, ok := s.files[taskID]
	s.filesMu.RUnlock()
	if !ok {
		return
	}
	if addr, ok := conn.RemoteAddr().(*net.TCPAddr); !ok || file.Recipient.IsValid() && file.Recipient != addr.AddrPort().Addr().Unmap() {
		return
	}

	f, err := os.Open(file.Path)
	if err != nil {
		return
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(current, file.Info) ||
		current.Size() != file.Size || !current.ModTime().Equal(file.Info.ModTime()) {
		return
	}

	totalLen := 100 + file.Size
	// Send 100-byte Command 4 header
	header := make([]byte, 100)
	binary.BigEndian.PutUint32(header[0:4], uint32(totalLen))
	binary.BigEndian.PutUint32(header[4:8], 1)
	binary.BigEndian.PutUint32(header[8:12], 4) // cmd 4
	binary.BigEndian.PutUint32(header[56:60], uint32(taskID))
	if _, err := conn.Write(header); err != nil {
		return
	}

	// Stream file content
	var done int64
	buf := make([]byte, 32*1024)
	for done < file.Size {
		s.filesMu.RLock()
		_, allowed := s.files[taskID]
		s.filesMu.RUnlock()
		if !allowed || s.closing.Load() {
			return
		}
		_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
		n, rerr := f.Read(buf[:min(int64(len(buf)), file.Size-done)])
		if n > 0 {
			if _, werr := conn.Write(buf[:n]); werr != nil {
				return
			}
			done += int64(n)
			if s.onProgress != nil {
				s.onProgress(file.TaskID, done, file.Size)
			}
		}
		if rerr != nil {
			return
		}
	}
	if file.Size == 0 && s.onProgress != nil {
		s.onProgress(file.TaskID, 0, 0)
	}
}

// OpenReverseFileChannel sends the bootstrap frame that native Neiwangtong
// expects before it opens the ordinary file-transfer connection. The returned
// connection must stay open until the corresponding transfer is complete.
func OpenReverseFileChannel(ctx context.Context, target netip.AddrPort, fileID uint64) (net.Conn, error) {
	return openReverseFileChannel(ctx, netip.Addr{}, target, fileID)
}

func openReverseFileChannel(ctx context.Context, bind netip.Addr, target netip.AddrPort, fileID uint64) (net.Conn, error) {
	if fileID == 0 || fileID > maxFileSelector || !validUnicast(target) {
		return nil, errors.New("invalid native reverse file request")
	}
	conn, err := dialTransfer(ctx, bind, target)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, nativeReverseFileFrame)
	binary.BigEndian.PutUint32(frame[0:4], nativeReverseFileFrame)
	binary.BigEndian.PutUint32(frame[4:8], 1)
	binary.BigEndian.PutUint32(frame[8:12], nativeReverseFileCommand)
	binary.BigEndian.PutUint32(frame[56:60], uint32(fileID))
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write(frame); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// dialTransfer uses the same local address as the control session. The sender
// checks that the download comes from the peer it offered the file to.
func dialTransfer(ctx context.Context, bind netip.Addr, target netip.AddrPort) (net.Conn, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	if bind.Is4() && !bind.IsUnspecified() {
		d.LocalAddr = net.TCPAddrFromAddrPort(netip.AddrPortFrom(bind, 0))
	}
	return d.DialContext(ctx, "tcp4", target.String())
}

func downloadFile(ctx context.Context, bind netip.Addr, target netip.AddrPort, fileID uint64, totalSize int64, destPath string, onProgress func(done, total int64)) error {
	if fileID == 0 || fileID > maxFileSelector || !validFileSize(totalSize) || !validUnicast(target) {
		return errors.New("invalid native file transfer")
	}
	conn, err := dialTransfer(ctx, bind, target)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(12 * time.Second))

	// Send Command 3 (108 bytes)
	if _, err := conn.Write(nativeFileRequest(uint32(fileID))); err != nil {
		return err
	}

	// Read Command 4 header (100 bytes)
	var header [100]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return fmt.Errorf("failed to read response header: %w", err)
	}
	ver := binary.BigEndian.Uint32(header[4:8])
	cmd := binary.BigEndian.Uint32(header[8:12])
	if ver != 1 || cmd != 4 || binary.BigEndian.Uint32(header[:]) != uint32(100+totalSize) ||
		binary.BigEndian.Uint32(header[56:]) != uint32(fileID) {
		return errors.New("invalid file response command from native peer")
	}

	out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		out.Close()
		if !complete {
			_ = os.Remove(destPath)
		}
	}()

	var done int64
	buf := make([]byte, 32*1024)
	for done < totalSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
		n, err := conn.Read(buf[:min(int64(len(buf)), totalSize-done)])
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if onProgress != nil {
				onProgress(done, totalSize)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}

	if done < totalSize {
		return fmt.Errorf("incomplete file: got %d of %d bytes", done, totalSize)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func nativeFileRequest(selector uint32) []byte {
	req := make([]byte, 108)
	binary.BigEndian.PutUint32(req, 108)
	binary.BigEndian.PutUint32(req[4:], 1)
	binary.BigEndian.PutUint32(req[8:], 3)
	binary.BigEndian.PutUint32(req[56:], selector)
	return req
}
