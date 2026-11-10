package desktop

import "github.com/vivaui/viva/ui"

// Scissors uses the same stroke geometry as the Lucide toolbar icons.
var Scissors = ui.Icon{ViewBox: 24, Stroke: 2, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
	ui.MoveTo(10, 7), ui.CubicTo(10, 11, 4, 11, 4, 7), ui.CubicTo(4, 3, 10, 3, 10, 7),
	ui.MoveTo(10, 17), ui.CubicTo(10, 21, 4, 21, 4, 17), ui.CubicTo(4, 13, 10, 13, 10, 17),
	ui.MoveTo(9.2, 9.1), ui.LineTo(20, 19),
	ui.MoveTo(9.2, 14.9), ui.LineTo(20, 5),
}}
