package neiwangtong

import (
	"bytes"
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

type outgoingFile struct {
	TaskID    uint64 // application-level offer identifier
	Path      string
	Size      int64
	Recipient netip.Addr
	Info      os.FileInfo
	chunked   bool
	allowed   func() bool
	ctx       context.Context
	cancel    context.CancelFunc
}

const maxFileSelector = math.MaxUint32

const (
	nativeReverseFileCommand = 100
	nativeReverseFileFrame   = 560
	nativeFileHeader         = 100
	nativeFileBlock          = 32 << 10
)

var (
	ErrFileRejected  = errors.New("对方拒绝了文件")
	ErrFileCancelled = errors.New("对方取消了文件传输")
)

func validFileSize(size int64, chunked bool) bool {
	return size >= 0 && (chunked || size <= math.MaxUint32-nativeFileHeader)
}

type TransferServer struct {
	listener      net.Listener
	port          int
	bindIP        netip.Addr
	reverse       net.Listener
	reversePort   int
	closing       atomic.Bool
	avatarMu      sync.RWMutex
	avatarPNG     []byte
	avatarModTime time.Time
	filesMu       sync.RWMutex
	files         map[uint64]outgoingFile
	onProgress    func(taskID uint64, done, total int64, failure error)
	reverseMu     sync.Mutex
	reverseWait   map[reverseKey]*incomingReverse
	avatarAllowed func(netip.Addr) bool
	active        sync.Map
	slots         chan struct{}
}

type reverseKey struct {
	remote   netip.Addr
	selector uint64
}

type incomingReverse struct {
	ready chan struct{}
	conn  net.Conn
}

func StartTransferServer(bind netip.Addr, port int, onProgress func(taskID uint64, done, total int64)) (*TransferServer, error) {
	return startTransferServer(bind, port, func(taskID uint64, done, total int64, failure error) {
		if onProgress != nil && failure == nil {
			onProgress(taskID, done, total)
		}
	}, nil)
}

func startTransferServer(bind netip.Addr, port int, onProgress func(taskID uint64, done, total int64, failure error), avatarAllowed func(netip.Addr) bool) (*TransferServer, error) {
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
		files:         make(map[uint64]outgoingFile),
		onProgress:    onProgress,
		reverseWait:   make(map[reverseKey]*incomingReverse),
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
	if !bytes.Equal(s.avatarPNG, png) {
		s.avatarPNG = bytes.Clone(png)
		s.avatarModTime = time.Now()
	}
}

func (s *TransferServer) RegisterFile(taskID uint64, path string, size int64) {
	_ = s.registerFile(taskID, taskID, path, size, netip.Addr{}, true)
}

func (s *TransferServer) registerFile(fileID, taskID uint64, path string, size int64, recipient netip.Addr, chunked bool) error {
	return s.registerFileWithAccess(fileID, taskID, path, size, recipient, chunked, nil)
}

func (s *TransferServer) registerFileWithAccess(fileID, taskID uint64, path string, size int64, recipient netip.Addr, chunked bool, allowed func() bool) error {
	if fileID == 0 || fileID > maxFileSelector || taskID == 0 || !validFileSize(size, chunked) {
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
	if existing, ok := s.files[fileID]; ok {
		if existing.TaskID == taskID && existing.Recipient == recipient && existing.Path == path &&
			existing.Size == size && existing.chunked == chunked && (existing.allowed == nil) == (allowed == nil) && os.SameFile(existing.Info, info) &&
			existing.Info.ModTime().Equal(info.ModTime()) {
			return nil // the same offer was queued again after a reconnect
		}
		return errors.New("内网通文件编号已被占用")
	}
	if len(s.files) >= 1024 {
		return errors.New("内网通待接收文件数量已达上限")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.files[fileID] = outgoingFile{TaskID: taskID, Path: path, Size: size, Recipient: recipient,
		Info: info, chunked: chunked, allowed: allowed, ctx: ctx, cancel: cancel}
	return nil
}

func (s *TransferServer) UnregisterFile(fileID uint64) {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	if file, ok := s.files[fileID]; ok {
		delete(s.files, fileID)
		file.cancel()
	}
}

func (s *TransferServer) expectReverse(key reverseKey) (<-chan struct{}, error) {
	s.reverseMu.Lock()
	defer s.reverseMu.Unlock()
	if s.reverseWait[key] != nil {
		return nil, errors.New("native file transfer is already pending")
	}
	ready := make(chan struct{})
	s.reverseWait[key] = &incomingReverse{ready: ready}
	return ready, nil
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
			if selector == avatarSelector {
				if addr, ok := conn.RemoteAddr().(*net.TCPAddr); !ok || s.avatarAllowed != nil && !s.avatarAllowed(addr.AddrPort().Addr().Unmap()) {
					return
				}
				s.serveAvatar(conn)
			} else {
				file, ok := s.fileForRequest(conn, selector)
				if !ok {
					return
				}
				if err := writeTransferConfirmation(conn, selector, file.Size, file.Info.ModTime()); err != nil {
					return
				}
			}
		case 3:
			if len(frame) == 108 {
				position := binary.BigEndian.Uint64(frame[60:68])
				if position <= math.MaxInt64 {
					s.serveFile(conn, selector, int64(position))
				}
			}
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
	key := reverseKey{remote: remoteAddr.AddrPort().Addr().Unmap(), selector: selector}
	s.reverseMu.Lock()
	waiter := s.reverseWait[key]
	if waiter == nil || waiter.conn != nil {
		s.reverseMu.Unlock()
		return
	}
	waiter.conn = conn
	close(waiter.ready)
	s.reverseMu.Unlock()
	// The native sender keeps this bootstrap connection open until the
	// separate cmd=3 transfer finishes. Closing it immediately after cmd=100
	// makes the original client cancel the pending transfer.
	_ = conn.SetDeadline(time.Time{})
	var unexpected [1]byte
	_, _ = io.ReadFull(conn, unexpected[:])
}

func (s *TransferServer) finishReverse(key reverseKey) {
	s.reverseMu.Lock()
	waiter := s.reverseWait[key]
	delete(s.reverseWait, key)
	s.reverseMu.Unlock()
	if waiter != nil && waiter.conn != nil {
		_ = waiter.conn.Close()
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
	png, modified := s.avatarPNG, s.avatarModTime
	s.avatarMu.RUnlock()
	if len(png) == 0 {
		return
	}

	if err := writeTransferConfirmation(conn, avatarSelector, int64(len(png)), modified); err != nil {
		return
	}

	frame := make([]byte, nativeFileHeader+len(png))
	encodeFileHeader(frame, avatarSelector, int64(len(png)), 0, uint32(len(png)))
	copy(frame[nativeFileHeader:], png)
	_, _ = conn.Write(frame)
}

func writeTransferConfirmation(w io.Writer, selector uint64, size int64, modified time.Time) error {
	confirm := make([]byte, 108)
	binary.BigEndian.PutUint32(confirm[0:4], 108)
	binary.BigEndian.PutUint32(confirm[4:8], 1)
	binary.BigEndian.PutUint32(confirm[8:12], 2) // cmd 2
	binary.BigEndian.PutUint32(confirm[56:60], uint32(selector))
	binary.BigEndian.PutUint64(confirm[60:68], uint64(size))
	binary.BigEndian.PutUint64(confirm[68:76], uint64(max(modified.Unix(), 0)))
	_, err := w.Write(confirm)
	return err
}

func (s *TransferServer) fileForRequest(conn net.Conn, selector uint64) (outgoingFile, bool) {
	s.filesMu.RLock()
	file, ok := s.files[selector]
	s.filesMu.RUnlock()
	addr, isTCP := conn.RemoteAddr().(*net.TCPAddr)
	return file, ok && isTCP && (!file.Recipient.IsValid() || file.Recipient == addr.AddrPort().Addr().Unmap()) &&
		(file.allowed == nil || file.allowed())
}

func (s *TransferServer) serveFile(conn net.Conn, selector uint64, position int64) (failure error) {
	file, ok := s.fileForRequest(conn, selector)
	if !ok || position < 0 || position > file.Size || position != 0 && !file.chunked {
		return nil
	}
	done := position
	defer func() {
		if failure != nil && s.onProgress != nil && file.ctx.Err() == nil && !s.closing.Load() {
			s.onProgress(file.TaskID, done, file.Size, failure)
		}
	}()

	f, err := os.Open(file.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(current, file.Info) ||
		current.Size() != file.Size || !current.ModTime().Equal(file.Info.ModTime()) {
		return errors.New("源文件已变化，请重新发送")
	}
	if _, err := f.Seek(position, io.SeekStart); err != nil {
		return err
	}

	stop := context.AfterFunc(file.ctx, func() { _ = conn.Close() })
	defer stop()
	var lastProgress time.Time
	// Native clients use cmd 4 blocks with 64-bit positions. Keep a single
	// frame only for old GoIM peers using this compatibility transport.
	frame := make([]byte, nativeFileHeader+nativeFileBlock)
	for first := true; first || done < file.Size; first = false {
		if file.ctx.Err() != nil || s.closing.Load() {
			return nil
		}
		_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
		count := min(file.Size-done, nativeFileBlock)
		start := nativeFileHeader
		if file.chunked {
			encodeFileHeader(frame, uint32(selector), file.Size, done, uint32(count))
			start = 0
		} else if first {
			encodeFileHeader(frame, uint32(selector), file.Size, 0, uint32(file.Size))
			if _, err := conn.Write(frame[:nativeFileHeader]); err != nil {
				return err
			}
		}
		end := nativeFileHeader + int(count)
		if _, err := io.ReadFull(f, frame[nativeFileHeader:end]); err != nil {
			return err
		}
		if _, err := conn.Write(frame[start:end]); err != nil {
			return err
		}
		done += count
		if s.onProgress != nil && (done == file.Size || time.Since(lastProgress) >= 150*time.Millisecond) {
			s.onProgress(file.TaskID, done, file.Size, nil)
			lastProgress = time.Now()
		}
	}
	return nil
}

func encodeFileHeader(header []byte, selector uint32, total, offset int64, count uint32) {
	binary.BigEndian.PutUint32(header[0:4], nativeFileHeader+count)
	binary.BigEndian.PutUint32(header[4:8], 1)
	binary.BigEndian.PutUint32(header[8:12], 4)
	binary.BigEndian.PutUint32(header[56:60], selector)
	binary.BigEndian.PutUint64(header[60:68], uint64(total))
	binary.BigEndian.PutUint64(header[68:76], uint64(offset))
	binary.BigEndian.PutUint32(header[76:80], count)
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

func downloadFile(ctx context.Context, bind netip.Addr, target netip.AddrPort, fileID uint64, totalSize int64, destPath string, confirm bool, onProgress func(done, total int64)) error {
	return downloadFileTo(ctx, bind, target, fileID, totalSize, destPath, confirm, false, onProgress)
}

func downloadFileTo(ctx context.Context, bind netip.Addr, target netip.AddrPort, fileID uint64, totalSize int64,
	destPath string, confirm, resume bool, onProgress func(done, total int64)) error {
	if fileID == 0 || fileID > maxFileSelector || totalSize < 0 || !validUnicast(target) {
		return errors.New("invalid native file transfer")
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if resume {
		flags = os.O_RDWR | os.O_CREATE
		if info, err := os.Lstat(destPath); err == nil && !info.Mode().IsRegular() {
			return errors.New("native transfer partial is not a regular file")
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	out, err := os.OpenFile(destPath, flags, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		out.Close()
		if !complete && !resume {
			_ = os.Remove(destPath)
		}
	}()
	var position int64
	if resume {
		info, err := out.Stat()
		if err != nil {
			return err
		}
		position = info.Size()
		if !info.Mode().IsRegular() || position > totalSize || !confirm && position != 0 {
			return errors.New("invalid native transfer partial")
		}
		if _, err := out.Seek(position, io.SeekStart); err != nil {
			return err
		}
	}
	conn, err := dialTransfer(ctx, bind, target)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(12 * time.Second))

	if confirm {
		if _, err := conn.Write(transferOpenRequest(uint32(fileID))); err != nil {
			return err
		}
		confirmation, err := readNativeTCPFrame(conn, 108)
		if err != nil {
			return fmt.Errorf("failed to read native file confirmation: %w", err)
		}
		if len(confirmation) != 108 || binary.BigEndian.Uint32(confirmation[8:12]) != 2 ||
			binary.BigEndian.Uint32(confirmation[56:60]) != uint32(fileID) {
			return errors.New("invalid native file confirmation")
		}
	}

	if _, err := conn.Write(nativeFileRequestAt(uint32(fileID), position)); err != nil {
		return err
	}

	if err := receiveFileFramesAt(conn, out, uint32(fileID), totalSize, position, onProgress); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
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

// Native clients send successive Command 4 records with a total length,
// byte offset and block size. Older GoIM sends one record with those fields
// unset. Neither format can write beyond the size announced in the offer.
func receiveFileFrames(conn net.Conn, out io.Writer, selector uint32, total int64, progress func(int64, int64)) error {
	return receiveFileFramesAt(conn, out, selector, total, 0, progress)
}

func receiveFileFramesAt(conn net.Conn, out io.Writer, selector uint32, total, position int64, progress func(int64, int64)) error {
	var header [nativeFileHeader]byte
	done := position
	buf := make([]byte, nativeFileBlock)
	for first := true; first || done < total; first = false {
		_ = conn.SetReadDeadline(time.Now().Add(12 * time.Second))
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return fmt.Errorf("read native file frame after %d bytes: %w", done, err)
		}
		block := int64(binary.BigEndian.Uint32(header[:4])) - nativeFileHeader
		if binary.BigEndian.Uint32(header[4:8]) != 1 || binary.BigEndian.Uint32(header[8:12]) != 4 ||
			binary.BigEndian.Uint32(header[56:60]) != selector || block < 0 || block > total-done ||
			block == 0 && done < total {
			return errors.New("invalid native file data frame")
		}
		size := binary.BigEndian.Uint64(header[60:68])
		offset := binary.BigEndian.Uint64(header[68:76])
		count := binary.BigEndian.Uint32(header[76:80])
		legacy := first && block == total && size == 0 && offset == 0 && count == 0
		if !legacy && (size != uint64(total) || offset != uint64(done) || int64(count) != block) {
			return errors.New("native file frame does not match the offered size or stream position")
		}
		for remaining := block; remaining > 0; {
			_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			n, err := io.ReadFull(conn, buf[:min(remaining, int64(len(buf)))])
			if err != nil {
				return fmt.Errorf("read native file block after %d bytes: %w", done, err)
			}
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			remaining -= int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
	}
	return nil
}

func nativeFileRequest(selector uint32) []byte {
	return nativeFileRequestAt(selector, 0)
}

func nativeFileRequestAt(selector uint32, position int64) []byte {
	req := make([]byte, 108)
	binary.BigEndian.PutUint32(req, 108)
	binary.BigEndian.PutUint32(req[4:], 1)
	binary.BigEndian.PutUint32(req[8:], 3)
	binary.BigEndian.PutUint32(req[56:], selector)
	binary.BigEndian.PutUint64(req[60:68], uint64(position))
	return req
}
