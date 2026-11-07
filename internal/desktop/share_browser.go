package desktop

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"image"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

const (
	maxShareDirectoryEntries = 4000
	maxShareQRBytes          = 512 << 10
	maxShareQRPixels         = 2_000_000
	shareQRDecodeSize        = 224
)

type shareBrowseKind uint8

const (
	shareBrowsePath shareBrowseKind = iota
	shareBrowseHome
	shareBrowseDownloads
	shareBrowseRoot
	shareBrowseDrives
)

type shareBrowseRequest struct {
	kind shareBrowseKind
	path string
}

type shareDirEntry struct {
	name     string
	path     string
	dir      bool
	size     int64
	modified time.Time
}

type shareDirResult struct {
	generation uint64
	kind       shareBrowseKind
	path       string
	parent     string
	title      string
	entries    []shareDirEntry
	err        string
	truncated  bool
}

type shareQRKey struct {
	fileID string
	length int
	digest [sha256.Size]byte
}

type shareQRResult struct {
	key    shareQRKey
	bitmap ui.Bitmap
	ok     bool
	err    string
}

type shareBrowserEvent struct {
	dir *shareDirResult
	qr  *shareQRResult
}

// shareBrowser owns the filesystem work behind the file page. UI code starts a
// request and reads only delivered events during OnWake; Paint never opens a
// directory or decodes an image.
type shareBrowser struct {
	mu         sync.Mutex
	generation uint64
	events     []shareBrowserEvent
	onReady    func()
	qrPending  map[shareQRKey]bool

	homeDir    func() (string, error)
	readDir    func(string) shareDirResult
	listDrives func() []shareDirEntry
	decodeQR   func([]byte, int) (ui.Bitmap, bool)
}

func (b *shareBrowser) ensure() {
	if b.homeDir == nil {
		b.homeDir = os.UserHomeDir
	}
	if b.readDir == nil {
		b.readDir = shareReadDirectory
	}
	if b.listDrives == nil {
		b.listDrives = shareSystemDrives
	}
	if b.decodeQR == nil {
		b.decodeQR = decodeShareQR
	}
}

func (b *shareBrowser) setNotify(notify func()) {
	b.mu.Lock()
	b.onReady = notify
	b.mu.Unlock()
}

func (b *shareBrowser) start(request shareBrowseRequest) uint64 {
	b.ensure()
	b.mu.Lock()
	b.generation++
	generation := b.generation
	homeDir, readDir, listDrives := b.homeDir, b.readDir, b.listDrives
	b.mu.Unlock()

	go func() {
		result := shareLoadDirectory(request, generation, homeDir, readDir, listDrives)
		b.push(shareBrowserEvent{dir: &result})
	}()
	return generation
}

func (b *shareBrowser) startQR(key shareQRKey, data []byte) {
	b.ensure()
	b.mu.Lock()
	if b.qrPending == nil {
		b.qrPending = make(map[shareQRKey]bool)
	}
	if b.qrPending[key] {
		b.mu.Unlock()
		return
	}
	b.qrPending[key] = true
	decode := b.decodeQR
	b.mu.Unlock()

	go func() {
		bitmap, ok := decode(data, shareQRDecodeSize)
		if ok {
			bitmap.ID = shareQRBitmapID(key)
			bitmap.Version = 1
		}
		b.mu.Lock()
		delete(b.qrPending, key)
		b.mu.Unlock()
		b.push(shareBrowserEvent{qr: &shareQRResult{key: key, bitmap: bitmap, ok: ok}})
	}()
}

func (b *shareBrowser) push(event shareBrowserEvent) {
	b.mu.Lock()
	b.events = append(b.events, event)
	notify := b.onReady
	b.mu.Unlock()
	if notify != nil {
		notify()
	}
}

func (b *shareBrowser) take() []shareBrowserEvent {
	b.mu.Lock()
	events := b.events
	b.events = nil
	b.mu.Unlock()
	return events
}

func shareLoadDirectory(request shareBrowseRequest, generation uint64,
	homeDir func() (string, error), readDir func(string) shareDirResult,
	listDrives func() []shareDirEntry) shareDirResult {
	result := shareDirResult{generation: generation, kind: request.kind}
	path := strings.TrimSpace(request.path)
	switch request.kind {
	case shareBrowseHome:
		home, err := homeDir()
		if err != nil {
			result.err = "无法定位主目录：" + err.Error()
			return result
		}
		path = home
	case shareBrowseDownloads:
		if path == "" {
			home, err := homeDir()
			if err != nil {
				result.err = "无法定位下载文件夹：" + err.Error()
				return result
			}
			path = filepath.Join(home, "Downloads")
		}
	case shareBrowseRoot:
		if path == "" {
			home, err := homeDir()
			if err != nil {
				result.err = "无法定位根目录：" + err.Error()
				return result
			}
			path = home
		}
		path = shareRootPath(path)
	case shareBrowseDrives:
		result.title = "此电脑"
		result.entries = append([]shareDirEntry(nil), listDrives()...)
		return result
	}
	if strings.TrimSpace(path) == "" {
		result.err = "无法确定要打开的文件夹"
		return result
	}
	loaded := readDir(path)
	loaded.generation = generation
	loaded.kind = request.kind
	return loaded
}

func shareReadDirectory(path string) shareDirResult {
	result := shareDirResult{}
	absolute, err := filepath.Abs(path)
	if err != nil {
		result.err = "无法打开文件夹：" + err.Error()
		return result
	}
	result.path = absolute
	result.parent = shareParentPath(absolute)
	directory, err := os.Open(absolute)
	if err != nil {
		result.err = "无法读取文件夹：" + err.Error()
		return result
	}
	defer directory.Close()
	entries := make([]os.DirEntry, 0, min(maxShareDirectoryEntries, 256))
	for len(entries) < maxShareDirectoryEntries {
		batch, readErr := directory.ReadDir(min(256, maxShareDirectoryEntries-len(entries)))
		entries = append(entries, batch...)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			result.err = "无法读取文件夹：" + readErr.Error()
			return result
		}
		if len(batch) == 0 {
			break
		}
	}
	if len(entries) == maxShareDirectoryEntries {
		if extra, readErr := directory.ReadDir(1); readErr == nil && len(extra) > 0 {
			result.truncated = true
		} else if readErr != nil && readErr != io.EOF {
			result.err = "无法读取文件夹：" + readErr.Error()
			return result
		}
	}
	result.entries = make([]shareDirEntry, 0, len(entries))
	for _, entry := range entries {
		item := shareDirEntry{
			name: entry.Name(),
			path: filepath.Join(absolute, entry.Name()),
			dir:  entry.IsDir(),
		}
		if info, infoErr := entry.Info(); infoErr == nil && info != nil {
			item.size = info.Size()
			item.modified = info.ModTime()
			item.dir = info.IsDir()
		} else if item.dir {
			item.size = -1
		}
		result.entries = append(result.entries, item)
	}
	sort.SliceStable(result.entries, func(i, j int) bool {
		if result.entries[i].dir != result.entries[j].dir {
			return result.entries[i].dir
		}
		left := strings.ToLower(result.entries[i].name)
		right := strings.ToLower(result.entries[j].name)
		if left != right {
			return left < right
		}
		return result.entries[i].name < result.entries[j].name
	})
	return result
}

func shareParentPath(path string) string {
	path = filepath.Clean(path)
	parent := filepath.Dir(path)
	if parent == path {
		return ""
	}
	return parent
}

func shareRootPath(path string) string {
	path = filepath.Clean(path)
	if volume := filepath.VolumeName(path); volume != "" {
		return volume + string(filepath.Separator)
	}
	return string(filepath.Separator)
}

func sharePathKey(path string) string {
	key := filepath.Clean(strings.TrimSpace(path))
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

func shareQRKeyFor(file domain.File) (shareQRKey, bool) {
	if strings.TrimSpace(file.ID) == "" || len(file.ShareQR) == 0 {
		return shareQRKey{}, false
	}
	return shareQRKey{
		fileID: file.ID,
		length: len(file.ShareQR),
		digest: sha256.Sum256(file.ShareQR),
	}, true
}

func shareQRBitmapID(key shareQRKey) uint64 {
	id := binary.BigEndian.Uint64(key.digest[:8])
	if id == 0 {
		return 1
	}
	return id
}

func decodeShareQR(data []byte, limit int) (ui.Bitmap, bool) {
	if len(data) == 0 || len(data) > maxShareQRBytes {
		return ui.Bitmap{}, false
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 ||
		int64(config.Width) > maxShareQRPixels || int64(config.Height) > maxShareQRPixels ||
		int64(config.Width)*int64(config.Height) > maxShareQRPixels {
		return ui.Bitmap{}, false
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ui.Bitmap{}, false
	}
	bounds := source.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return ui.Bitmap{}, false
	}
	width, height := bounds.Dx(), bounds.Dy()
	if limit > 0 && (width > limit || height > limit) {
		scale := min(float64(limit)/float64(width), float64(limit)/float64(height))
		width = max(1, int(float64(width)*scale))
		height = max(1, int(float64(height)*scale))
	}
	pixels := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		sourceY := bounds.Min.Y + y*bounds.Dy()/height
		for x := 0; x < width; x++ {
			sourceX := bounds.Min.X + x*bounds.Dx()/width
			red, green, blue, alpha := source.At(sourceX, sourceY).RGBA()
			offset := (y*width + x) * 4
			pixels[offset+0] = uint8(blue >> 8)
			pixels[offset+1] = uint8(green >> 8)
			pixels[offset+2] = uint8(red >> 8)
			pixels[offset+3] = uint8(alpha >> 8)
		}
	}
	return ui.Bitmap{Pix: pixels, Width: width, Height: height, Stride: width * 4}, true
}
