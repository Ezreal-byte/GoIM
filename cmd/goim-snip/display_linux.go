//go:build linux

package main

import (
	"image"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func desktopCursor() (image.Point, bool) {
	c, err := xgb.NewConn()
	if err != nil {
		return image.Point{}, false
	}
	defer c.Close()
	root := xproto.Setup(c).DefaultScreen(c).Root
	cursor, err := xproto.QueryPointer(c, root).Reply()
	if err != nil || cursor == nil || !cursor.SameScreen {
		return image.Point{}, false
	}
	return image.Pt(int(cursor.RootX), int(cursor.RootY)), true
}

func desktopWindows() []image.Rectangle {
	c, err := xgb.NewConn()
	if err != nil {
		return nil
	}
	defer c.Close()
	root := xproto.Setup(c).DefaultScreen(c).Root
	stacking := snipXAtom(c, "_NET_CLIENT_LIST_STACKING")
	extents := snipXAtom(c, "_NET_FRAME_EXTENTS")
	windowType := snipXAtom(c, "_NET_WM_WINDOW_TYPE")
	desktopType := snipXAtom(c, "_NET_WM_WINDOW_TYPE_DESKTOP")
	windows := make([]xproto.Window, 0)
	if stacking != 0 {
		list, err := xproto.GetProperty(c, false, root, stacking, xproto.AtomWindow, 0, 4096).Reply()
		if err == nil && list != nil && list.Format == 32 {
			for offset := 0; offset+4 <= len(list.Value); offset += 4 {
				windows = append(windows, xproto.Window(xgb.Get32(list.Value[offset:])))
			}
		}
	}
	if len(windows) == 0 {
		if tree, err := xproto.QueryTree(c, root).Reply(); err == nil && tree != nil {
			windows = tree.Children
		}
	}
	// EWMH and QueryTree order windows from bottom to top.
	result := make([]image.Rectangle, 0, min(len(windows), 512))
	for i := len(windows) - 1; i >= 0 && len(windows)-1-i < 512; i-- {
		w := windows[i]
		attrs, err := xproto.GetWindowAttributes(c, w).Reply()
		if err != nil || attrs == nil || attrs.MapState != xproto.MapStateViewable || attrs.Class != xproto.WindowClassInputOutput {
			continue
		}
		if windowType != 0 && desktopType != 0 {
			prop, _ := xproto.GetProperty(c, false, w, windowType, xproto.AtomAtom, 0, 16).Reply()
			if prop != nil && prop.Format == 32 {
				isDesktop := false
				for j := 0; j+4 <= len(prop.Value); j += 4 {
					if xgb.Get32(prop.Value[j:]) == uint32(desktopType) {
						isDesktop = true
						break
					}
				}
				if isDesktop {
					continue
				}
			}
		}
		frame, err := xproto.GetGeometry(c, xproto.Drawable(w)).Reply()
		if err != nil || frame == nil || frame.Width < 32 || frame.Height < 24 {
			continue
		}
		location, err := xproto.TranslateCoordinates(c, w, root, 0, 0).Reply()
		if err != nil || location == nil || !location.SameScreen {
			continue
		}
		x, y := int(location.DstX), int(location.DstY)
		r := image.Rect(x, y, x+int(frame.Width), y+int(frame.Height))
		if extents != 0 {
			prop, _ := xproto.GetProperty(c, false, w, extents, xproto.AtomCardinal, 0, 4).Reply()
			if prop != nil && prop.Format == 32 && len(prop.Value) == 16 {
				left, right, top, bottom := int(xgb.Get32(prop.Value)), int(xgb.Get32(prop.Value[4:])), int(xgb.Get32(prop.Value[8:])), int(xgb.Get32(prop.Value[12:]))
				if max(left, right, top, bottom) <= 256 {
					r = image.Rect(r.Min.X-left, r.Min.Y-top, r.Max.X+right, r.Max.Y+bottom)
				}
			}
		}
		result = append(result, r)
	}
	return result
}

func snipXAtom(c *xgb.Conn, name string) xproto.Atom {
	reply, err := xproto.InternAtom(c, true, uint16(len(name)), name).Reply()
	if err != nil || reply == nil {
		return 0
	}
	return reply.Atom
}
