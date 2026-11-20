package desktop

import (
	"fmt"
	"math"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/i18n"
	"goim/internal/media"
)

func (u *UI) startViewerVideo(at time.Duration) {
	s := &u.surface.viewer
	s.video.Stop()
	s.video = nil
	path := u.fileLocalPath[s.fileID]
	s.videoPosition = max(0, at)
	s.videoError = ""
	s.videoEnded = false
	if path == "" {
		return
	}
	wake := u.wakeViewer
	if wake == nil {
		wake = u.invalidator.Invalidate
	}
	u.nextImageID++
	s.videoID = u.nextImageID
	s.video = media.Start(media.HelperPath(), path, u.state.Settings.VoiceSpeaker, u.state.Settings.VoiceSpeakerVolume, s.videoPosition, wake)
}
func (u *UI) toggleViewerVideo() {
	s := &u.surface.viewer
	if s.video != nil && !s.videoEnded {
		s.videoPosition = s.video.Snapshot().Position
		s.video.Stop()
		s.video = nil
		return
	}
	if s.videoEnded {
		s.videoPosition = 0
	}
	u.startViewerVideo(s.videoPosition)
}
func (u *UI) drawViewerVideo(c *gfx.Canvas, stage ui.Rect, theme ui.Theme, muted ui.Color) {
	s := &u.surface.viewer
	if s.video != nil {
		snapshot := s.video.Snapshot()
		s.videoPosition, s.videoEnded = snapshot.Position, snapshot.Ended
		if snapshot.Err != nil {
			s.videoError = i18n.T(snapshot.Err.Error())
		}
		frame := snapshot.Frame
		if frame.Number != 0 {
			s.videoFrame = gfx.Bitmap{ID: s.videoID, Version: frame.Number, Pix: frame.Pixels, Width: frame.Width, Height: frame.Height, Stride: 4 * frame.Width}
		}
	}
	area := stage
	area.Height = max(0, area.Height-46)
	if b := s.videoFrame; bitmapReady(b) && s.videoError == "" {
		scale := min(area.Width/float32(b.Width), area.Height/float32(b.Height))
		w, h := float32(b.Width)*scale, float32(b.Height)*scale
		c.DrawBitmap(b, ui.Rect{X: area.X + (area.Width-w)/2, Y: area.Y + (area.Height-h)/2, Width: w, Height: h})
	} else {
		text := s.videoError
		if text == "" {
			text = i18n.T("正在加载视频…")
			if u.fileLocalPath[s.fileID] == "" {
				text = i18n.T("视频尚未保存到本机")
			}
		}
		c.LabelCenter(text, area, gfx.TextStyle{Size: 14, Color: muted})
	}
}
func (u *UI) drawVideoControls(c *gfx.Canvas, stage ui.Rect, theme ui.Theme) {
	s := &u.surface.viewer
	ids := u.surface.ids
	colors := paletteFor(theme)
	bar := ui.Rect{X: stage.X, Y: stage.Bottom() - 44, Width: stage.Width, Height: 44}
	c.Fill(bar, colors.rail)
	controlsX := bar.X + 12
	button := func(id gfx.ID, label string, icon gfx.Icon, rotate float32) bool {
		rect := ui.Rect{X: controlsX, Y: bar.Y + 5, Width: 38, Height: 34}
		controlsX += 42
		return viewerTitleButton(c, theme, colors, rect, id, i18n.T(label), icon, gfx.IconTransform{Rotate: rotate}, u.fileLocalPath[s.fileID] == "", false)
	}
	label, icon := "播放视频", Play
	if s.video != nil && !s.videoEnded {
		label, icon = "暂停播放", Pause
	}
	if button(ids.videoPlay, label, icon, 0) {
		u.toggleViewerVideo()
	}
	if button(ids.videoBack, "后退 10 秒", viewerBackIcon, 0) {
		u.startViewerVideo(max(0, s.videoPosition-10*time.Second))
	}
	if button(ids.videoForward, "前进 10 秒", viewerBackIcon, math.Pi) {
		u.startViewerVideo(s.videoPosition + 10*time.Second)
	}
	seconds := int(s.videoPosition.Seconds())
	c.Label(fmt.Sprintf("%02d:%02d", seconds/60, seconds%60), ui.Rect{X: controlsX + 8, Y: bar.Y, Width: 88, Height: bar.Height}, gfx.TextStyle{Size: 13, Color: colors.secondaryText})
}
