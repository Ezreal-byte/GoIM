package desktop

import (
	"github.com/vivaui/viva/gfx"
)

// Scissors uses the same stroke geometry as the Lucide toolbar icons.
var Scissors = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(10, 7), gfx.CubicTo(10, 11, 4, 11, 4, 7), gfx.CubicTo(4, 3, 10, 3, 10, 7),
	gfx.MoveTo(10, 17), gfx.CubicTo(10, 21, 4, 21, 4, 17), gfx.CubicTo(4, 13, 10, 13, 10, 17),
	gfx.MoveTo(9.2, 9.1), gfx.LineTo(20, 19),
	gfx.MoveTo(9.2, 14.9), gfx.LineTo(20, 5),
}}
