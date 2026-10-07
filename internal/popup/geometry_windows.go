//go:build windows

package popup

import (
	"github.com/lxn/win"
	"syscall"
)

var (
	geometryComctl         = syscall.NewLazyDLL("comctl32.dll")
	geometrySetSubclass    = geometryComctl.NewProc("SetWindowSubclass")
	geometryRemoveSubclass = geometryComctl.NewProc("RemoveWindowSubclass")
	geometryDefSubclass    = geometryComctl.NewProc("DefSubclassProc")
)

const fitMessage = win.WM_APP + 42

func (w *Window) queueFit() {
	if w.fitting || w.dragging || w.fitQueued {
		return
	}
	w.fitQueued = true
	win.PostMessage(w.MW.Handle(), fitMessage, 0, 0)
}

func (w *Window) trackGeometry() error {
	w.subclassCallback = syscall.NewCallback(func(hwnd uintptr, msg uint32, wp, lp, id, data uintptr) uintptr {
		// A card is a stable pixel layout. Do not apply Windows' suggested
		// DPI rectangle or Walk's descendant-font scaling when crossing monitors.
		if msg == win.WM_DPICHANGED {
			return 0
		}
		if msg == win.WM_APP+45 {
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
			w.dragging = true
			w.autoPlacement = false
		}
		if msg == win.WM_SYSCOMMAND {
			command := wp & 0xfff0
			if command == win.SC_SIZE || command == win.SC_MINIMIZE || command == win.SC_MAXIMIZE || command == win.SC_RESTORE {
				return 0
			}
		}
		if msg == win.WM_NCLBUTTONDBLCLK && wp == win.HTCAPTION {
			return 0
		}
		if (msg == win.WM_NCLBUTTONDOWN || msg == win.WM_NCLBUTTONDBLCLK) && (wp == win.HTMINBUTTON || wp == win.HTMAXBUTTON) {
			return 0
		}
		if msg == fitMessage {
			w.fitQueued = false
			if w.dragging {
				return 0
			}
			w.fitContent()
			// Synchronize Walk's proposed size even if the native size did not
			// change. DPI's suggested rectangle must not drive later layouts.
			_ = w.MW.SetBoundsPixels(w.MW.BoundsPixels())
			return 0
		}
		result, _, _ := geometryDefSubclass.Call(hwnd, uintptr(msg), wp, lp)
		if w.view != nil {
			if msg == win.WM_SIZE {
				w.view.Resize()
			}
			if msg == win.WM_MOVE || msg == win.WM_WINDOWPOSCHANGED {
				w.view.ParentMoved()
			}
		}
		if msg == win.WM_INITMENU || msg == win.WM_INITMENUPOPUP {
			w.disableWindowCommands()
		}
		if msg == win.WM_NCHITTEST && (result == win.HTMINBUTTON || result == win.HTMAXBUTTON) {
			return win.HTBORDER
		}
		if msg == win.WM_NCHITTEST && result >= win.HTLEFT && result <= win.HTBOTTOMRIGHT {
			return win.HTBORDER
		}
		if msg == win.WM_EXITSIZEMOVE {
			w.dragging = false
			w.queueFit()
		}
		if msg == win.WM_NCDESTROY {
			geometryRemoveSubclass.Call(hwnd, w.subclassCallback, id)
		}
		return result
	})
	ok, _, err := geometrySetSubclass.Call(uintptr(w.MW.Handle()), w.subclassCallback, 2, 0)
	if ok == 0 {
		return err
	}
	return nil
}

func (w *Window) disableWindowCommands() {
	menu := win.GetSystemMenu(w.MW.Handle(), false)
	for _, command := range []uint32{win.SC_MINIMIZE, win.SC_MAXIMIZE, win.SC_RESTORE, win.SC_SIZE} {
		win.EnableMenuItem(menu, command, win.MF_BYCOMMAND|win.MF_GRAYED)
	}
	win.DrawMenuBar(w.MW.Handle())
}
