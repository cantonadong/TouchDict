//go:build windows

package mainwindow

import (
	"github.com/lxn/win"
	"syscall"
)

var (
	comctl         = syscall.NewLazyDLL("comctl32.dll")
	setSubclass    = comctl.NewProc("SetWindowSubclass")
	removeSubclass = comctl.NewProc("RemoveWindowSubclass")
	defSubclass    = comctl.NewProc("DefSubclassProc")
)

func (w *Window) trackUserResize() error {
	w.resizeCallback = syscall.NewCallback(func(hwnd uintptr, msg uint32, wp, lp, id, data uintptr) uintptr {
		if msg == win.WM_APP+44 {
			queue := w.pending
			w.pending = nil
			for _, fn := range queue {
				if !w.closed {
					fn()
				}
			}
			return 0
		}
		if msg == win.WM_ENTERSIZEMOVE {
			w.userSizing = true
			w.resizeStartHeight = w.MW.SizePixels().Height
		}
		result, _, _ := defSubclass.Call(hwnd, uintptr(msg), wp, lp)
		if msg == win.WM_NCDESTROY {
			removeSubclass.Call(hwnd, w.resizeCallback, id)
		}
		if w.closed {
			return result
		}
		if msg == win.WM_ACTIVATE && wp&0xffff != win.WA_INACTIVE {
			win.PostMessage(w.MW.Handle(), win.WM_APP+43, 0, 0)
		}
		if msg == win.WM_APP+43 && win.GetForegroundWindow() == w.MW.Handle() {
			w.focusSearch()
		}
		if w.view != nil {
			if msg == win.WM_SIZE || msg == 0x02E0 {
				w.view.Resize()
			} // WM_DPICHANGED
			if msg == win.WM_MOVE || msg == win.WM_WINDOWPOSCHANGED {
				w.view.ParentMoved()
			}
		}
		if msg == win.WM_EXITSIZEMOVE {
			height := w.MW.SizePixels().Height
			if height != w.resizeStartHeight && !win.IsZoomed(w.MW.Handle()) && !win.IsIconic(w.MW.Handle()) {
				w.userHeight = screenHeight(w.MW)
				if w.callbacks.HeightChanged != nil {
					w.callbacks.HeightChanged(w.userHeight)
				}
			}
			w.userSizing = false
			w.growForContent()
		}
		return result
	})
	ok, _, err := setSubclass.Call(uintptr(w.MW.Handle()), w.resizeCallback, 1, 0)
	if ok == 0 {
		return err
	}
	return nil
}
