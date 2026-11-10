package desktop

import "github.com/vivaui/viva/ui"

// Keep a single full-size preview, only for the current detached image. Chat
// rows retain their small thumbnails. The keyed worker coalesces fast navigation
// and rotations, and stale results cannot replace the current image.
func (u *UI) queueViewerOriginal() {
	if u.presentViewer == nil || u.media == nil {
		return
	}
	state := &u.chatSurface().viewer
	if !state.open {
		return
	}
	path := u.fileLocalPath[state.fileID]
	if state.originalPath == path && state.originalTurn == state.rotation {
		return
	}
	state.originalPath, state.originalTurn = path, state.rotation
	state.original = ui.Bitmap{}
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
	state := &u.chatSurface().viewer
	if !state.open || result.generation != state.originalGen || result.path != state.originalPath || !result.decoded {
		return
	}
	u.nextImageID++
	result.bitmap.ID, result.bitmap.Version = u.nextImageID, 1
	state.original = result.bitmap
}
