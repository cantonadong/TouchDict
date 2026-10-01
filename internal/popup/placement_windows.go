//go:build windows

package popup

import "github.com/lxn/walk"

// Re-evaluate after results change card dimensions. Clamping a left-side
// placement back into the work area can cover the selection: use a vertical
// placement instead when neither horizontal side has enough space.
func (w *Window) placeBesideSelection() {
	if !w.autoPlacement || w.dragging {
		return
	}
	size := w.MW.SizePixels()
	work := w.selectionWork
	left, top, right, bottom := int(work.Left)+12, int(work.Top)+12, int(work.Right)-12, int(work.Bottom)-12
	bounds := w.selectionBounds
	x, y := w.selectionPoint.X+12, w.selectionPoint.Y+32
	if bounds != nil {
		y = max(top, min(bounds.Top, bottom-size.Height))
		switch {
		case bounds.Right+12+size.Width <= right:
			x = bounds.Right + 12
		case bounds.Left-12-size.Width >= left:
			x = bounds.Left - 12 - size.Width
		default:
			x = max(left, min(bounds.Left, right-size.Width))
			if bounds.Bottom+12+size.Height <= bottom {
				y = bounds.Bottom + 12
			} else if bounds.Top-12-size.Height >= top {
				y = bounds.Top - 12 - size.Height
			} else {
				y = bounds.Bottom + 12
			}
		}
	} else {
		x = max(left, min(x, right-size.Width))
	}
	if w.pinned && w.anchored && bounds != nil {
		if w.anchor.X+size.Width <= bounds.Left || w.anchor.X >= bounds.Right || w.anchor.Y+size.Height <= bounds.Top || w.anchor.Y >= bounds.Bottom {
			x, y = w.anchor.X, w.anchor.Y
		}
	}
	// Final boundary check applies to every candidate, including pinned and
	// fallback placements, after asynchronous results change the dimensions.
	x = max(left, min(x, right-size.Width))
	y = max(top, min(y, bottom-size.Height))
	w.anchor = walk.Point{X: x, Y: y}
	w.anchored = true
	if w.MW.Visible() {
		position := w.MW.BoundsPixels()
		if position.X != x || position.Y != y {
			position.X, position.Y = x, y
			_ = w.MW.SetBoundsPixels(position)
		}
	}
}
