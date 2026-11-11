package neiwangtong

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type OutgoingFile struct {
	TaskID uint64
	Path   string
	Size   int64
}

type TransferServer struct {
	listener  net.Listener
	port      int
	closing   atomic.Bool
	avatarMu  sync.RWMutex
	avatarPNG []byte
	filesMu   sync.RWMutex
	files     map[uint64]OutgoingFile
	onProgress func(taskID uint64, done, total int64)
}

func StartTransferServer(bind netip.Addr, port int, onProgress func(taskID uint64, done, total int64)) (*TransferServer, error) {
	bindStr := fmt.Sprintf(":%d", port)
	if bind.IsValid() && bind.Is4() {
		bindStr = fmt.Sprintf("%s:%d", bind.String(), port)
	}
	l, err := net.Listen("tcp4", bindStr)
	if err != nil {
		return nil, err
	}
	s := &TransferServer{
		listener:   l,
		port:       l.Addr().(*net.TCPAddr).Port,
		files:      make(map[uint64]OutgoingFile),
		onProgress: onProgress,
	}
	go s.acceptLoop()
	return s, nil
}

func (s *TransferServer) Port() int {
	return s.port
}

func (s *TransferServer) Close() error {
	s.closing.Store(true)
	return s.listener.Close()
}

func (s *TransferServer) SetAvatar(png []byte) {
	s.avatarMu.Lock()
	defer s.avatarMu.Unlock()
	s.avatarPNG = png
}

func (s *TransferServer) RegisterFile(taskID uint64, path string, size int64) {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	s.files[taskID] = OutgoingFile{TaskID: taskID, Path: path, Size: size}
}

func (s *TransferServer) UnregisterFile(taskID uint64) {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	delete(s.files, taskID)
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
		go s.handleConnection(conn)
	}
}

func (s *TransferServer) handleConnection(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	var head [12]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return
	}
	length := binary.BigEndian.Uint32(head[0:4])
	ver := binary.BigEndian.Uint32(head[4:8])
	cmd := binary.BigEndian.Uint32(head[8:12])
	if ver != 1 || length < 12 || length > 1024 {
		return
	}

	body := make([]byte, length-12)
	if _, err := io.ReadFull(conn, body); err != nil {
		return
	}

	// Selector is at offset 56 of original frame (offset 56 - 12 = 44 of body)
	if len(body) < 48 {
		return
	}
	selector := binary.BigEndian.Uint32(body[44:48])

	// Command 1: Avatar request
	if cmd == 1 && selector == avatarSelector {
		s.serveAvatar(conn)
		return
	}

	// Command 3: File download request
	if cmd == 3 {
		s.serveFile(conn, uint64(selector))
		return
	}
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

	f, err := os.Open(file.Path)
	if err != nil {
		return
	}
	defer f.Close()

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
	for {
		_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := conn.Write(buf[:n]); werr != nil {
				return
			}
			done += int64(n)
			if s.onProgress != nil {
				s.onProgress(taskID, done, file.Size)
			}
		}
		if rerr != nil {
			break
		}
	}
}

// DownloadFile connects to remote TCP 9013, requests the file by taskID, and writes to destPath.
func DownloadFile(ctx context.Context, target netip.AddrPort, taskID uint64, totalSize int64, destPath string, onProgress func(done, total int64)) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp4", target.String())
	if err != nil {
		return err
	}
	defer conn.Close()

	// Send Command 3 (108 bytes)
	req := make([]byte, 108)
	binary.BigEndian.PutUint32(req[0:4], 108)
	binary.BigEndian.PutUint32(req[4:8], 1)
	binary.BigEndian.PutUint32(req[8:12], 3) // cmd 3
	binary.BigEndian.PutUint32(req[56:60], uint32(taskID))
	if _, err := conn.Write(req); err != nil {
		return err
	}

	// Read Command 4 header (100 bytes)
	var header [100]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return fmt.Errorf("failed to read response header: %w", err)
	}
	ver := binary.BigEndian.Uint32(header[4:8])
	cmd := binary.BigEndian.Uint32(header[8:12])
	if ver != 1 || cmd != 4 {
		return errors.New("invalid file response command from native peer")
	}

	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		_ = os.Remove(tmpPath)
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
		n, err := conn.Read(buf)
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
	out.Close()
	return os.Rename(tmpPath, destPath)
}
