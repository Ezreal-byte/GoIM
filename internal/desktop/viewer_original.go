package desktop

import (
	"bytes"
	"image"
	"image/png"

	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
	"goim/internal/i18n"
)

// Keep a single full-size preview, only for the current detached image. Chat
// rows retain their small thumbnails. The keyed worker coalesces fast navigation
// and rotations, and stale results cannot replace the current image.
func (u *UI) queueViewerOriginal() {
	if u.presentViewer == nil || u.media == nil {
		return
	}
	state := &u.surface.viewer
	if !state.open || u.state.File(state.fileID).Video() {
		return
	}
	path := u.fileLocalPath[state.fileID]
	if state.originalPath == path && state.originalTurn == state.rotation {
		return
	}
	state.originalPath, state.originalTurn = path, state.rotation
	state.original = gfx.Bitmap{}
	u.mediaImageGen++
	state.originalGen = u.mediaImageGen
	if path != "" {
		u.media.submitOne(mediaJob{
			key: mediaJobKey{kind: mediaJobViewerImage}, kind: mediaJobViewerImage,
			path: path, generation: state.originalGen, imageLimit: maxImageDimension, imageRotation: state.rotation,
		})
	}
}

func (u *UI) applyViewerOriginal(result mediaImageResult) {
	if result.generation == 0 {
		return
	}
	state := &u.surface.viewer
	if !state.open || result.generation != state.originalGen || result.path != state.originalPath || !result.decoded {
		return
	}
	u.nextImageID++
	result.bitmap.ID, result.bitmap.Version = u.nextImageID, 1
	state.original = result.bitmap
}

// The decoded bitmap is immutable, so copying captures the full image and its
// current rotation even if the user navigates before PNG encoding finishes.
func (u *UI) copyViewerToClipboard(bitmap gfx.Bitmap) {
	if !u.viewerCopying.CompareAndSwap(false, true) {
		return
	}
	wake := u.wakeViewer
	go func() {
		defer func() {
			u.viewerCopying.Store(false)
			if wake != nil {
				wake()
			}
		}()
		img := image.NewRGBA(image.Rect(0, 0, bitmap.Width, bitmap.Height))
		for y := 0; y < bitmap.Height; y++ {
			row := bitmap.Pix[y*bitmap.Stride:][:bitmap.Width*4]
			dst := img.Pix[y*img.Stride:]
			for i := 0; i < len(row); i += 4 {
				dst[i], dst[i+1], dst[i+2], dst[i+3] = row[i+2], row[i+1], row[i], row[i+3]
			}
		}
		var encoded bytes.Buffer
		err := png.Encode(&encoded, img)
		if err == nil {
			err = u.writeClipboardPNG("", encoded.Bytes())
		}
		if err != nil {
			u.backend.Submit(domain.Command{Kind: domain.ReportError, Text: i18n.T("复制失败：") + err.Error()})
		}
	}()
}
