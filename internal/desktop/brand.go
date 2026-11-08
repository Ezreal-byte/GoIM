package desktop

import "github.com/vivaui/viva/ui"

// The broken speech-bubble outline and two nodes form GoIM's LAN mark.
var goIMBubble = ui.Icon{
	ViewBox: 32, Stroke: 2, Cap: ui.CapRound, Join: ui.JoinRound,
	Commands: []ui.PathCommand{
		ui.MoveTo(24, 13), ui.LineTo(24, 11), ui.LineTo(8, 11),
		ui.LineTo(8, 21), ui.LineTo(10.5, 21), ui.LineTo(10.5, 25),
		ui.LineTo(15, 21), ui.LineTo(24, 21), ui.LineTo(24, 17),
	},
}

// PaintGoIMMark draws the shared GoIM application and sharing-page identity.
// The mark is vector-based so title bars, lock screens, and generated icons all
// stay visually consistent without loading an asset at paint time.
func PaintGoIMMark(canvas *ui.Canvas, bounds ui.Rect) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	edge := min(bounds.Width, bounds.Height)
	canvas.Rounded(bounds, edge*0.25, ui.RGB(37, 86, 216))
	canvas.DrawIcon(goIMBubble, bounds, ui.RGB(255, 255, 255))
	dot := edge * 3 / 32
	x, y := edge*14.5/32, edge*14.5/32
	canvas.Rounded(ui.Rect{X: bounds.X + x, Y: bounds.Y + y, Width: dot, Height: dot}, dot/2, ui.RGB(255, 255, 255))
	canvas.Rounded(ui.Rect{X: bounds.X + x + edge*8/32, Y: bounds.Y + y, Width: dot, Height: dot}, dot/2, ui.RGB(142, 244, 217))
}
