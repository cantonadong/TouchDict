//go:build windows

package popup

import "github.com/lxn/walk"

func (w *Window) fitContent() {
	if w.fitting || w.dragging || w.measuredHeight <= 0 {
		return
	}
	w.fitting = true
	defer func() { w.fitting = false }()
	size := w.MW.SizePixels()
	client := w.MW.ClientBoundsPixels()
	position := w.MW.BoundsPixels()
	work := monitorWorkArea(walk.Point{X: position.X + size.Width/2, Y: position.Y + 30})
	if w.autoPlacement {
		work = w.selectionWork
	}
	maxHeight := max(1, int(work.Bottom-work.Top)-24)
	// Size for the entire monitor first; placement can move the card upward
	// when the selection is near the bottom instead of forcing scrolling.
	height := min(maxHeight, max(cardMinHeight, w.measuredHeight*w.MW.DPI()/96+size.Height-client.Height))
	if size.Height != height {
		w.MW.SetSizePixels(walk.Size{Width: cardWidth, Height: height})
	}
	if w.autoPlacement {
		w.placeBesideSelection()
	} else if w.MW.Visible() {
		position = w.MW.BoundsPixels()
		x := max(int(work.Left)+12, min(position.X, int(work.Right)-12-position.Width))
		y := max(int(work.Top)+12, min(position.Y, int(work.Bottom)-12-position.Height))
		if x != position.X || y != position.Y {
			position.X, position.Y = x, y
			_ = w.MW.SetBoundsPixels(position)
		}
		if w.pinned {
			w.captureAnchor()
		}
	}
}
