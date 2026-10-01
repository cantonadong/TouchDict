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
	callback := syscall.NewCallback(func(hwnd uintptr, msg uint32, wp, lp, id, data uintptr) uintptr {
		if msg == win.WM_ENTERSIZEMOVE {
			w.userSizing = true
			w.resizeStartHeight = w.MW.SizePixels().Height
		}
		result, _, _ := defSubclass.Call(hwnd, uintptr(msg), wp, lp)
		if msg == win.WM_EXITSIZEMOVE {
			height := w.MW.SizePixels().Height
			if height != w.resizeStartHeight {
				w.userHeight = height
			}
			w.userSizing = false
			w.growForContent()
		}
		if msg == win.WM_NCDESTROY {
			removeSubclass.Call(hwnd, w.resizeCallback, id)
		}
		return result
	})
	w.resizeCallback = callback
	ok, _, err := setSubclass.Call(uintptr(w.MW.Handle()), callback, 1, 0)
	if ok == 0 {
		return err
	}
	return nil
}
