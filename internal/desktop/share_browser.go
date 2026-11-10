package desktop

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"image"
	"strings"
	"sync"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

const (
	maxShareQRBytes   = 512 << 10
	maxShareQRPixels  = 2_000_000
	shareQRDecodeSize = 224
)

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

// shareQRWorker decodes only requested QR images off the painter thread.
type shareQRWorker struct {
	mu        sync.Mutex
	events    []shareQRResult
	onReady   func()
	qrPending map[shareQRKey]bool
}

func (b *shareQRWorker) setNotify(notify func()) {
	b.mu.Lock()
	b.onReady = notify
	b.mu.Unlock()
}

func (b *shareQRWorker) startQR(key shareQRKey, data []byte) {
	b.mu.Lock()
	if b.qrPending == nil {
		b.qrPending = make(map[shareQRKey]bool)
	}
	if b.qrPending[key] {
		b.mu.Unlock()
		return
	}
	b.qrPending[key] = true
	b.mu.Unlock()

	go func() {
		bitmap, ok := decodeShareQR(data, shareQRDecodeSize)
		if ok {
			bitmap.ID = shareQRBitmapID(key)
			bitmap.Version = 1
		}
		b.mu.Lock()
		delete(b.qrPending, key)
		b.mu.Unlock()
		b.push(shareQRResult{key: key, bitmap: bitmap, ok: ok})
	}()
}

func (b *shareQRWorker) push(event shareQRResult) {
	b.mu.Lock()
	b.events = append(b.events, event)
	notify := b.onReady
	b.mu.Unlock()
	if notify != nil {
		notify()
	}
}

func (b *shareQRWorker) take() []shareQRResult {
	b.mu.Lock()
	events := b.events
	b.events = nil
	b.mu.Unlock()
	return events
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
