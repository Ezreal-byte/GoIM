package neiwangtong

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	maxFolderItems     = 10_000
	maxFolderPathUnits = 299 // the 300-unit wire field includes a terminator
	folderBlockSize    = 32 << 10
	folderBlockHeader  = 54
	folderTaskStart    = 31
	folderTaskEnd      = folderTaskStart + 64
	folderManifestHead = 76
	folderManifestItem = 635
	folderManifestAck  = 26
)

type directoryItem struct {
	relative  string
	size      int64
	modified  time.Time
	directory bool
}

func (item directoryItem) startMarker() uint32 {
	if item.size == 0 {
		return 1
	}
	return 2
}

type Directory struct {
	Path    string
	Name    string
	Size    int64
	MaxSize int64
	MaxPath int
	Stamp   string
	items   []directoryItem
}

func InspectDirectory(path string) (Directory, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return Directory{}, err
	}
	dir := Directory{Path: path, Name: filepath.Base(path)}
	if !validDirectoryComponent(dir.Name) {
		return Directory{}, errors.New("文件夹名称不适合跨平台传输")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return Directory{}, errors.New("请选择普通文件夹")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return Directory{}, err
	}
	defer root.Close()
	h := sha256.New()
	var empty []directoryItem
	err = fs.WalkDir(root.FS(), ".", func(relative string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || relative == "." {
			return walkErr
		}
		units, err := directoryPathUnits(relative)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			children, err := fs.ReadDir(root.FS(), relative)
			if err != nil || len(children) != 0 {
				return err // paths reconstruct nonempty parent directories
			}
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("文件夹中含有链接或特殊文件")
		}
		if len(dir.items)+len(empty) >= maxFolderItems {
			return errors.New("文件夹包含过多条目")
		}
		item := directoryItem{relative: relative, modified: info.ModTime(), directory: info.IsDir()}
		if item.directory {
			empty = append(empty, item)
			fmt.Fprintf(h, "directory %q %d\n", relative, item.modified.UnixNano())
		} else {
			item.size = info.Size()
			if item.size < 0 || item.size >= 1<<32 || dir.Size > math.MaxInt64-item.size {
				return errors.New("文件夹中单个文件达到 4 GiB")
			}
			dir.items = append(dir.items, item)
			dir.Size += item.size
			fmt.Fprintf(h, "%q %d %d\n", relative, item.size, item.modified.UnixNano())
		}
		dir.MaxPath, dir.MaxSize = max(dir.MaxPath, units), max(dir.MaxSize, item.size)
		return nil
	})
	if err != nil {
		return Directory{}, err
	}
	dir.items = append(dir.items, empty...)
	if len(dir.items) == 0 {
		return Directory{}, errors.New("内网通不能发送空文件夹")
	}
	dir.Stamp = fmt.Sprintf("%x", h.Sum(nil))
	return dir, nil
}

func (d Directory) offer(task string) FolderOffer {
	return FolderOffer{TaskID: task, Name: d.Name, Size: d.Size, MaxItemPath: d.MaxPath, MaxItemSize: d.MaxSize}
}

func directoryPathUnits(path string) (int, error) {
	for _, name := range strings.Split(path, "/") {
		if !validDirectoryComponent(name) {
			return 0, errors.New("文件夹包含无效的相对路径")
		}
	}
	units := len(utf16.Encode([]rune(path)))
	if units > maxFolderPathUnits {
		return 0, errors.New("文件夹包含过长的相对路径")
	}
	return units, nil
}

func validDirectoryComponent(name string) bool {
	if !utf8.ValidString(name) || name == "" || name == "." || name == ".." || len(name) > 255 ||
		strings.ContainsAny(name, "/\\:*?\"<>|\x00") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	stem := strings.ToUpper(strings.TrimSpace(strings.SplitN(name, ".", 2)[0]))
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" {
		return false
	}
	return !(len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) &&
		stem[3] >= '1' && stem[3] <= '9')
}

func newFolderFrame(kind uint32, length int) []byte {
	frame := make([]byte, length)
	binary.BigEndian.PutUint32(frame, uint32(length))
	binary.BigEndian.PutUint32(frame[4:8], 2)
	binary.BigEndian.PutUint32(frame[8:12], kind)
	return frame
}

func readFolderFrame(conn net.Conn, kind uint32, length int, exact bool) ([]byte, error) {
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	var header [12]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	n, got := binary.BigEndian.Uint32(header[:4]), binary.BigEndian.Uint32(header[8:12])
	if binary.BigEndian.Uint32(header[4:8]) != 2 || got != kind || n < 12 || n > uint32(length) || exact && n != uint32(length) {
		return nil, fmt.Errorf("内网通文件夹帧类型或长度不符：收到 %d/%d，需要 %d/%d", got, n, kind, length)
	}
	frame := make([]byte, n)
	copy(frame, header[:])
	_, err := io.ReadFull(conn, frame[12:])
	return frame, err
}

func writeFolderFrame(conn net.Conn, frame []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, err := conn.Write(frame)
	return err
}

func requireFolderFrame(conn net.Conn, kind uint32, length int) ([]byte, error) {
	return readFolderFrame(conn, kind, length, true)
}

func putFolderString(field []byte, value string, order binary.ByteOrder) {
	for i, unit := range utf16.Encode([]rune(value)) {
		order.PutUint16(field[i*2:i*2+2], unit)
	}
}

func readFolderString(field []byte, order binary.ByteOrder) string {
	units := make([]uint16, 0, len(field)/2)
	for len(field) >= 2 {
		if u := order.Uint16(field[:2]); u != 0 {
			units = append(units, u)
		} else {
			break
		}
		field = field[2:]
	}
	return string(utf16.Decode(units))
}

func makeFolderManifest(items []directoryItem) []byte {
	frame := newFolderFrame(3, folderManifestHead+len(items)*folderManifestItem)
	binary.BigEndian.PutUint32(frame[32:36], uint32(len(items)))
	for i, item := range items {
		at := folderManifestHead + i*folderManifestItem
		record := frame[at : at+folderManifestItem]
		// The manifest stores Windows paths in UTF-16LE; the initiation
		// record uses UTF-16BE for the hexadecimal task ID.
		putFolderString(record[:600], strings.ReplaceAll(item.relative, "/", "\\"), binary.LittleEndian)
		record[600] = 1
		if item.directory {
			record[600] = 2
		}
		if item.size == 0 {
			record[624] = 1 // stock marks entries without file data
		}
		binary.BigEndian.PutUint32(record[601:605], uint32(i))
		binary.BigEndian.PutUint64(record[605:613], uint64(item.size))
		binary.BigEndian.PutUint64(record[613:621], uint64(max(item.modified.Unix(), 0)))
	}
	return frame
}

func parseFolderManifest(frame []byte, count int, offer FolderOffer) ([]directoryItem, error) {
	if len(frame) != folderManifestHead+count*folderManifestItem || binary.BigEndian.Uint32(frame[32:36]) != uint32(count) {
		return nil, errors.New("内网通文件夹清单数量不符")
	}
	items := make([]directoryItem, 0, count)
	seen := make(map[string]bool, count)
	remaining := offer.Size
	for i := 0; i < count; i++ {
		at := folderManifestHead + i*folderManifestItem
		record := frame[at : at+folderManifestItem]
		relative := strings.ReplaceAll(readFolderString(record[:600], binary.LittleEndian), "\\", "/")
		units, err := directoryPathUnits(relative)
		size := binary.BigEndian.Uint64(record[605:613])
		directory := record[600] == 2
		key := strings.ToLower(relative)
		if err != nil || units > offer.MaxItemPath || seen[key] || record[600] != 1 && !directory || directory && size != 0 ||
			binary.BigEndian.Uint32(record[601:605]) != uint32(i) || size > uint64(remaining) || size > uint64(offer.MaxItemSize) {
			return nil, errors.New("内网通子文件路径、编号或大小无效")
		}
		seen[key] = true
		remaining -= int64(size)
		item := directoryItem{relative: relative, size: int64(size), directory: directory}
		if modified := binary.BigEndian.Uint64(record[613:621]); modified > 0 && modified <= math.MaxInt64 {
			item.modified = time.Unix(int64(modified), 0)
		}
		items = append(items, item)
	}
	if remaining != 0 {
		return nil, errors.New("内网通文件夹内容大小不符")
	}
	return items, nil
}

func sendDirectory(ctx context.Context, conn net.Conn, task string, dir Directory, progress func(int64)) error {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	init := newFolderFrame(1, 684)
	putFolderString(init[folderTaskStart:folderTaskEnd], task, binary.BigEndian)
	binary.BigEndian.PutUint32(init[632:636], uint32(len(dir.items)))
	binary.BigEndian.PutUint64(init[636:644], uint64(dir.Size))
	if err := writeFolderFrame(conn, init); err != nil {
		return err
	}
	ready, err := requireFolderFrame(conn, 2, 672)
	if err != nil {
		return err
	}
	if readFolderString(ready[folderTaskStart:folderTaskEnd], binary.BigEndian) != task {
		return errors.New("内网通文件夹任务编号不匹配")
	}
	if err := writeFolderFrame(conn, makeFolderManifest(dir.items)); err != nil {
		return err
	}
	ack, err := requireFolderFrame(conn, 4, folderManifestHead+len(dir.items)*folderManifestAck)
	if err != nil {
		return err
	}
	if binary.BigEndian.Uint32(ack[32:36]) != uint32(len(dir.items)) {
		return errors.New("内网通文件夹清单确认数量不符")
	}
	offsets := make([]int64, len(dir.items))
	var done int64
	for i, item := range dir.items {
		at := folderManifestHead + i*folderManifestAck
		offset := binary.BigEndian.Uint64(ack[at+4 : at+12])
		if offset != 0 && offset >= uint64(item.size) {
			return errors.New("内网通子文件续传偏移无效")
		}
		offsets[i] = int64(offset)
		done += offsets[i]
	}
	expected := acknowledgeFolderManifest(dir.items, offsets)
	if !bytes.Equal(ack[folderManifestHead:], expected[folderManifestHead:]) {
		return errors.New("内网通文件夹清单确认无效")
	}
	root, err := os.OpenRoot(dir.Path)
	if err != nil {
		return err
	}
	defer root.Close()
	for i, item := range dir.items {
		offset := offsets[i]
		if err := sendDirectoryItem(conn, root, uint32(i), item, offset, func(n int64) {
			if progress != nil {
				progress(done + (n - offset))
			}
		}); err != nil {
			return err
		}
		complete, err := requireFolderFrame(conn, 6, 58)
		if err != nil {
			return err
		}
		if binary.BigEndian.Uint32(complete[32:36]) != uint32(i) || binary.BigEndian.Uint64(complete[36:44]) != uint64(item.size) ||
			item.size == 0 && binary.BigEndian.Uint32(complete[44:48]) != 1 {
			return errors.New("内网通子文件确认编号或大小不匹配")
		}
		done += item.size - offset
	}
	if err := writeFolderFrame(conn, newFolderFrame(7, 72)); err != nil {
		return err
	}
	_, err = requireFolderFrame(conn, 8, 72)
	return err
}

func sendDirectoryItem(conn net.Conn, root *os.Root, index uint32, item directoryItem, offset int64, progress func(int64)) error {
	if item.directory {
		entries, err := fs.ReadDir(root.FS(), item.relative)
		if err != nil || len(entries) != 0 {
			return errors.New("源文件夹已变化")
		}
		return writeFolderItemStart(conn, index, item.startMarker())
	}
	if info, err := root.Lstat(item.relative); err != nil || !info.Mode().IsRegular() {
		return errors.New("源文件夹子文件已变化")
	}
	file, err := root.Open(item.relative)
	if err != nil {
		return err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() != item.size ||
		!info.ModTime().Equal(item.modified) {
		return errors.New("源文件夹子文件已变化")
	}
	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return err
		}
	}
	if err := writeFolderItemStart(conn, index, item.startMarker()); err != nil {
		return err
	}
	block := newFolderFrame(5, folderBlockHeader+folderBlockSize)
	binary.BigEndian.PutUint32(block[32:36], index)
	for done := offset; done < item.size; {
		n := int(min(int64(folderBlockSize), item.size-done))
		if _, err := io.ReadFull(file, block[folderBlockHeader:folderBlockHeader+n]); err != nil {
			return err
		}
		binary.BigEndian.PutUint32(block[:4], uint32(folderBlockHeader+n))
		binary.BigEndian.PutUint32(block[36:40], uint32(n))
		if err := writeFolderFrame(conn, block[:folderBlockHeader+n]); err != nil {
			return err
		}
		done += int64(n)
		progress(done)
	}
	return nil
}

func writeFolderItemStart(conn net.Conn, index, marker uint32) error {
	start := newFolderFrame(5, folderBlockHeader+1)
	binary.BigEndian.PutUint32(start[32:36], index)
	binary.BigEndian.PutUint32(start[36:40], 1)
	binary.BigEndian.PutUint32(start[40:44], marker)
	return writeFolderFrame(conn, start)
}

func acknowledgeFolderManifest(items []directoryItem, offsets []int64) []byte {
	ack := newFolderFrame(4, folderManifestHead+len(items)*folderManifestAck)
	binary.BigEndian.PutUint32(ack[32:36], uint32(len(items)))
	for i, item := range items {
		at := folderManifestHead + i*folderManifestAck
		binary.BigEndian.PutUint32(ack[at:at+4], uint32(i))
		binary.BigEndian.PutUint64(ack[at+4:at+12], uint64(offsets[i]))
		if item.size == 0 {
			binary.BigEndian.PutUint32(ack[at+12:at+16], 1) // no file content requested
		}
	}
	return ack
}

func receiveDirectory(ctx context.Context, conn net.Conn, init []byte, offer FolderOffer, target directoryTarget, progress func(int64)) error {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	count := int(binary.BigEndian.Uint32(init[632:636]))
	if count <= 0 || count > maxFolderItems || binary.BigEndian.Uint64(init[636:644]) != uint64(offer.Size) {
		return errors.New("文件夹数据与邀请大小不符")
	}
	ok := newFolderFrame(2, 672)
	putFolderString(ok[folderTaskStart:folderTaskEnd], offer.TaskID, binary.BigEndian)
	if err := writeFolderFrame(conn, ok); err != nil {
		return err
	}
	manifest, err := requireFolderFrame(conn, 3, folderManifestHead+count*folderManifestItem)
	if err != nil {
		return err
	}
	items, err := parseFolderManifest(manifest, count, offer)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := target.prepare(manifest, items); err != nil {
		return err
	}
	root, err := os.OpenRoot(target.path)
	if err != nil {
		return err
	}
	defer root.Close()
	offsets, err := target.offsets(root, items)
	if err != nil {
		return err
	}
	if err := writeFolderFrame(conn, acknowledgeFolderManifest(items, offsets)); err != nil {
		return err
	}
	var done int64
	for _, offset := range offsets {
		done += offset
	}
	if target.manifest != "" && progress != nil {
		progress(done)
	}
	for i, item := range items {
		offset := offsets[i]
		if err := receiveDirectoryItem(ctx, conn, root, uint32(i), item, offset, target.manifest != "", func(n int64) {
			if progress != nil {
				progress(done + (n - offset))
			}
		}); err != nil {
			return err
		}
		done += item.size - offset
		finished := newFolderFrame(6, 58)
		binary.BigEndian.PutUint32(finished[32:36], uint32(i))
		binary.BigEndian.PutUint64(finished[36:44], uint64(item.size))
		if item.size == 0 {
			binary.BigEndian.PutUint32(finished[44:48], 1)
		}
		if err := writeFolderFrame(conn, finished); err != nil {
			return err
		}
	}
	if _, err := requireFolderFrame(conn, 7, 72); err != nil {
		return err
	}
	return writeFolderFrame(conn, newFolderFrame(8, 72))
}

func receiveDirectoryItem(ctx context.Context, conn net.Conn, root *os.Root, index uint32, item directoryItem, offset int64,
	resumable bool, progress func(int64)) error {
	start, err := requireFolderFrame(conn, 5, folderBlockHeader+1)
	if err != nil {
		return err
	}
	if binary.BigEndian.Uint32(start[32:36]) != index || binary.BigEndian.Uint32(start[36:40]) != 1 ||
		binary.BigEndian.Uint32(start[40:44]) != item.startMarker() || start[54] != 0 {
		return errors.New("无效的内网通子文件起始位置")
	}
	relative, size := filepath.FromSlash(item.relative), item.size
	if item.directory {
		return root.MkdirAll(relative, 0700)
	}
	if err := root.MkdirAll(filepath.Dir(relative), 0700); err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if offset > 0 {
		flags = os.O_WRONLY
	} else if resumable {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := root.OpenFile(relative, flags, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if offset > 0 {
		if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() < offset || info.Size() > size {
			return errors.New("内网通文件夹暂存文件已变化")
		}
		if err := file.Truncate(offset); err != nil {
			return err
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return err
		}
	}
	for done := offset; done < size; {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, err := readFolderFrame(conn, 5, folderBlockHeader+folderBlockSize, false)
		if err != nil {
			return err
		}
		amount := int64(len(frame) - folderBlockHeader)
		if amount <= 0 || amount > size-done ||
			binary.BigEndian.Uint32(frame[32:36]) != index || binary.BigEndian.Uint32(frame[36:40]) != uint32(amount) ||
			binary.BigEndian.Uint32(frame[40:44]) != 0 {
			return errors.New("无效的内网通子文件数据块")
		}
		if _, err := file.Write(frame[folderBlockHeader:]); err != nil {
			return err
		}
		done += amount
		progress(done)
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return file.Close()
}
