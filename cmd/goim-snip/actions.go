package main

import (
	"errors"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/capture"
	"goim/internal/i18n"
)

var (
	ellipseID     = tree.ID(tools, "ellipse")
	textID        = tree.ID(tools, "text")
	mosaicID      = tree.ID(tools, "mosaic")
	redoID        = tree.ID(page, "redo")
	saveID        = tree.ID(page, "save-as")
	inkScope      = tree.Sub(page, tree.Name("ink"))
	widthScope    = tree.Sub(page, tree.Name("stroke-width"))
	textScope     = tree.ID(page, "text-dialog")
	textDismissID = tree.ID(page, "text-dismiss")
	textCloseID   = tree.ID(page, "text-close")
	textFieldID   = tree.ID(page, "text-entry")
	textOKID      = tree.ID(page, "text-add")
	textCancelID  = tree.ID(page, "text-cancel")
)

var toolIDs = [...]gfx.ID{selectID, rectID, arrowID, drawID, ellipseID, textID, mosaicID}

var inkPresets = [...]struct {
	label string
	color color.NRGBA
}{
	{"蓝色", capture.DefaultShapeColor},
	{"红色", color.NRGBA{R: 231, G: 70, B: 65, A: 255}},
	{"黄色", color.NRGBA{R: 249, G: 191, B: 53, A: 255}},
	{"绿色", color.NRGBA{R: 24, G: 163, B: 93, A: 255}},
	{"白色", color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
	{"黑色", color.NRGBA{R: 25, G: 29, B: 37, A: 255}},
}

var strokePresets = [...]struct {
	label string
	width float64
}{{"细线", 2}, {"中等线宽", 4}, {"粗线", 8}}

type snipText struct {
	page  *ui.Page
	theme gfx.Theme
	field ui.Field
	at    capture.Point
	scale float64
	open  bool
}

func (e *editor) beginText(at capture.Point, scale float64) {
	e.text.at = clampCapturePoint(at, e.document.Crop())
	e.text.scale = scale
	e.text.field.SetText("")
	e.text.open = true
}

func (e *editor) textInput(ctx app.TextInputContext) *app.TextInput {
	if e.text.open {
		return e.text.page.TextInput(ctx)
	}
	return nil
}

func (e *editor) paintActions(c *gfx.Canvas, theme gfx.Theme, bounds gfx.Rect) {
	if e.notice != "" {
		components.PaintToast(c, theme, bounds, components.ToastSnapshot{
			Title: e.notice, Type: components.ToastError,
		}, components.ToastOptions{Layer: 8, Position: components.ToastTopRight})
	}
	if !e.text.open {
		return
	}
	e.text.theme = theme
	e.text.page.Draw(c, bounds, func(f ui.C) {
		dialog := ui.Dialog(f, ui.DialogSpec{
			Title: i18n.T("文字标注"), Width: 380, Layer: 10, Scope: textScope,
			DismissID: textDismissID, CloseID: textCloseID, InitialFocusID: textFieldID,
		})
		var submitted, canceled bool
		dialog.Body(func() {
			submitted = ui.TextFieldID(f, textFieldID, &e.text.field,
				i18n.T("标注文字"), i18n.T("输入标注文字…")).FillWidth().Submitted()
		})
		dialog.Footer(func() {
			canceled = ui.Button(f, i18n.T("取消")).Hit(textCancelID, i18n.T("取消")).Outline().Clicked()
			submitted = ui.Button(f, i18n.T("添加文字")).Hit(textOKID, i18n.T("添加文字")).Clicked() || submitted
		})
		if canceled || dialog.Dismissed() {
			e.text.open = false
			e.text.field.SetText("")
		} else if submitted {
			value := strings.TrimSpace(e.text.field.Text())
			if value != "" {
				e.text.open = false
				e.text.field.SetText("")
				c.AfterPaint(func() { e.addText(c, value); c.RequestFrame() })
			}
		}
	})
}

func (e *editor) addText(c *gfx.Canvas, value string) {
	if utf8.RuneCountInString(value) > 160 {
		e.notice = i18n.T("文字标注最多 160 个字符")
		return
	}
	style := gfx.TextStyle{Size: float32(min(80, max(12, 20/e.text.scale))), Weight: gfx.WeightSemibold, Color: e.inkColor()}
	rasterize := func(canvas *gfx.Canvas) (gfx.Bitmap, error) {
		width := min(8192, max(1, int(math.Ceil(float64(canvas.TextWidthWeight(value, style.Size, style.Weight))))+8))
		return canvas.RasterizeText(value, style, 1, width, int(math.Ceil(float64(style.Size)*1.6))+2)
	}
	bitmap, err := rasterize(c)
	if errors.Is(err, gfx.ErrTextRasterizerUnavailable) {
		// Offscreen and software painters may have metrics only. Rasterize
		// once with the platform text engine; never do this in the paint loop.
		var native gfx.Canvas
		native.SetTextEngine(app.PlatformTextEngine())
		bitmap, err = rasterize(&native)
	}
	if err != nil || bitmap.Width <= 0 || bitmap.Height <= 0 {
		e.notice = i18n.T("无法绘制文字标注")
		return
	}
	ink := image.NewRGBA(image.Rect(0, 0, bitmap.Width, bitmap.Height))
	for y := range bitmap.Height {
		for x := range bitmap.Width {
			i, j := y*bitmap.Stride+x*4, ink.PixOffset(x, y)
			ink.Pix[j], ink.Pix[j+1], ink.Pix[j+2], ink.Pix[j+3] = bitmap.Pix[i+2], bitmap.Pix[i+1], bitmap.Pix[i], bitmap.Pix[i+3]
		}
	}
	if err := e.document.Add(capture.Shape{Kind: capture.Text, Start: e.text.at, Ink: ink}); err != nil {
		e.notice = i18n.T("无法绘制文字标注")
		return
	}
	e.notice = ""
	e.rebuildPreview()
}

func (e *editor) scheduleSaveAs(c *gfx.Canvas) {
	c.AfterPaint(func() {
		e.saveAs()
		if !e.done {
			c.RequestFrame()
		}
	})
}

func (e *editor) saveAs() {
	path, err := app.SaveFile(app.SaveFileOptions{
		Title: i18n.T("截图另存为"), SuggestedName: "GoIM-" + time.Now().Format("20060102-150405") + ".png", DefaultExtension: "png",
		Filters: []app.FileFilter{{Name: "PNG", Pattern: "*.png"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && path == "" {
		return
	}
	if err == nil {
		if filepath.Ext(path) == "" {
			path += ".png"
		}
		if !strings.EqualFold(filepath.Ext(path), ".png") {
			e.notice = i18n.T("请选择 PNG 文件名")
			return
		}
		err = e.document.ExportPNG(path)
	}
	if err != nil {
		e.notice = i18n.T("无法保存截图")
		return
	}
	e.finish(outcomeSaved)
}

func (e *editor) redo() bool {
	if e.done || !e.document.Redo() {
		return false
	}
	e.gesture = nil
	e.rebuildPreview()
	return true
}

func (e *editor) rebuildPreview() {
	if err := e.refreshPreview(); err != nil {
		e.err = err
	}
}

func (e *editor) annotation(g *gesture, scale float64) capture.Shape {
	width := annotationWidth(scale) * strokePresets[e.stroke].width / 4
	if g.shape == capture.Mosaic {
		width = 12
	}
	return capture.Shape{Kind: g.shape, Start: g.start, End: g.end, Points: g.points,
		Color: inkPresets[e.ink].color, Width: width}
}

func inkColor(ink color.NRGBA) gfx.Color { return gfx.RGB(ink.R, ink.G, ink.B) }
func (e *editor) inkColor() gfx.Color    { return inkColor(inkPresets[e.ink].color) }

func (e *editor) paintStyle(c *gfx.Canvas, theme gfx.Theme, bounds gfx.Rect, horizontal bool) {
	strokes := bounds
	strokes.Y += 25
	if horizontal {
		const offset = 6*23 + 16
		strokes.X, strokes.Y, strokes.Width = bounds.X+offset, bounds.Y, 92
		c.Fill(gfx.Rect{X: strokes.X - 8, Y: bounds.Y + 4, Width: 1, Height: 15}, theme.Border)
	}
	size := min(float32(23), (bounds.Width-4)/float32(len(inkPresets)))
	if size < 10 {
		return
	}
	for i, preset := range inkPresets {
		r := gfx.Rect{X: bounds.X + float32(i)*size, Y: bounds.Y, Width: size, Height: 23}
		c.RoundedBorder(r.Inset(gfx.All(4)), 6, 1, theme.MutedForeground.Mix(theme.Border, 0.45), inkColor(preset.color))
		id := inkScope.At(i)
		if i == e.ink || c.FocusVisible() == id {
			c.RoundedOutline(r.Inset(gfx.All(1)), 6, 1.5, theme.Ring)
		}
		c.HitWithOptions(id, r, i18n.T(preset.label), gfx.HitOptions{Role: gfx.RoleRadio, Selected: i == e.ink, Hint: i18n.T(preset.label)})
		if c.Clicked(id) {
			e.ink = i
		}
	}
	for i, preset := range strokePresets {
		width := strokes.Width / float32(len(strokePresets))
		r := gfx.Rect{X: strokes.X + float32(i)*width, Y: strokes.Y, Width: width, Height: 23}
		id := widthScope.At(i)
		if i == e.stroke || c.FocusVisible() == id {
			c.Rounded(r.Inset(gfx.All(1)), 6, theme.Hover)
			c.RoundedOutline(r.Inset(gfx.All(1)), 6, 1, theme.Ring)
		}
		weight := float32(preset.width)
		c.Rounded(gfx.Rect{X: r.X + r.Width/2 - 9, Y: r.Y + (r.Height-weight)/2, Width: 18, Height: weight}, weight/2, theme.Foreground)
		c.HitWithOptions(id, r, i18n.T(preset.label), gfx.HitOptions{Role: gfx.RoleRadio, Selected: i == e.stroke, Hint: i18n.T(preset.label)})
		if c.Clicked(id) {
			e.stroke = i
		}
	}
}
