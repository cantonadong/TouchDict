//go:build windows

package uistyle

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"
	"syscall"
	"time"
)

const copyToastTimer = 0x5444

type CopyToast struct {
	Widget   *walk.CustomWidget
	owner    *walk.MainWindow
	font     *walk.Font
	start    time.Time
	alpha    float64
	callback uintptr
}

func NewCopyToast(parent walk.Container, owner *walk.MainWindow) (*CopyToast, error) {
	w := &CopyToast{owner: owner}
	w.font, _ = walk.NewFont("Microsoft YaHei UI", 12, 0)
	toast, err := walk.NewCustomWidgetPixels(parent, 0, func(canvas *walk.Canvas, bounds walk.Rectangle) error {
		base := walk.Color(win.GetSysColor(win.COLOR_BTNFACE))
		blend := func(target byte, shift uint) byte {
			original := byte(uint32(base) >> shift)
			return byte(float64(original) + (float64(target)-float64(original))*w.alpha)
		}
		background := walk.RGB(blend(45, 0), blend(45, 8), blend(45, 16))
		foreground := walk.RGB(blend(255, 0), blend(255, 8), blend(255, 16))
		brush, err := walk.NewSolidColorBrush(background)
		if err != nil {
			return err
		}
		defer brush.Dispose()
		clear, err := walk.NewSolidColorBrush(base)
		if err != nil {
			return err
		}
		defer clear.Dispose()
		bounds = w.Widget.ClientBoundsPixels()
		if err := canvas.FillRectanglePixels(clear, bounds); err != nil {
			return err
		}
		if err := canvas.FillRoundedRectanglePixels(brush, bounds, walk.Size{Width: 16, Height: 16}); err != nil {
			return err
		}
		return canvas.DrawTextPixels("已复制", w.font, foreground, bounds, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
	})
	if err != nil {
		return nil, err
	}
	w.Widget = toast
	toast.SetPaintMode(walk.PaintBuffered)
	toast.SetVisible(false)
	toast.SetAlwaysConsumeSpace(true)
	w.callback = syscall.NewCallback(func(hwnd uintptr, msg uint32, timer, tick uintptr) uintptr { w.advance(); return 0 })
	owner.Disposing().Attach(func() { win.KillTimer(owner.Handle(), copyToastTimer) })
	return w, nil
}

func (w *CopyToast) Show() {
	w.start = time.Now()
	w.alpha = 0
	// Keep Walk's layout visibility unchanged: this is an overlay, not a
	// new content row. Native visibility cannot restart the layout tree.
	win.ShowWindow(w.Widget.Handle(), win.SW_SHOWNOACTIVATE)
	// The footer composite spans the center of the result panel. New child
	// windows start behind older siblings, so explicitly raise the overlay.
	win.SetWindowPos(w.Widget.Handle(), win.HWND_TOP, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	w.Widget.Invalidate()
	win.SetTimer(w.owner.Handle(), copyToastTimer, 16, w.callback)
}

func (w *CopyToast) Hide() {
	win.KillTimer(w.owner.Handle(), copyToastTimer)
	win.ShowWindow(w.Widget.Handle(), win.SW_HIDE)
	if parent := w.Widget.Parent(); parent != nil {
		parent.Invalidate()
	}
}

func (w *CopyToast) advance() {
	elapsed := time.Since(w.start).Seconds()
	if elapsed >= 1 {
		w.Hide()
		return
	}
	w.alpha = 0.5
	if elapsed < 0.1 {
		w.alpha = 0.5 * elapsed / 0.1
	}
	if elapsed > 0.9 {
		w.alpha = 0.5 * (1 - elapsed) / 0.1
	}
	w.Widget.Invalidate()
}
