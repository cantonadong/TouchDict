//go:build windows

package popup

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"
	"syscall"
	"touchdict/internal/uistyle"
	"unsafe"
)

var (
	geometryComctl         = syscall.NewLazyDLL("comctl32.dll")
	geometrySetSubclass    = geometryComctl.NewProc("SetWindowSubclass")
	geometryRemoveSubclass = geometryComctl.NewProc("RemoveWindowSubclass")
	geometryDefSubclass    = geometryComctl.NewProc("DefSubclassProc")
	captionBoundsProc      = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")
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
		// DWM owns the standard caption buttons. Reject their screen-space
		// region BEFORE DefSubclassProc can start native hover/press tracking.
		if msg == win.WM_NCHITTEST || msg == win.WM_NCMOUSEMOVE || msg == win.WM_NCLBUTTONDOWN || msg == win.WM_NCLBUTTONUP || msg == win.WM_NCLBUTTONDBLCLK {
			if w.inDisabledCaptionButtons(lp) {
				if msg == win.WM_NCHITTEST {
					return win.HTNOWHERE
				}
				return 0
			}
		}
		// A card is a stable pixel layout. Do not apply Windows' suggested
		// DPI rectangle or Walk's descendant-font scaling when crossing monitors.
		if msg == win.WM_DPICHANGED {
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
			w.MW.SetSuspended(true)
			_ = w.MW.SetMinMaxSizePixels(walk.Size{Width: cardMinWidth, Height: cardMinHeight}, walk.Size{Width: cardWidth})
			for _, button := range []*walk.PushButton{w.speak, w.pin, w.retry, w.copyExample} {
				uistyle.LockButtonSize(button)
			}
			w.MW.SetSuspended(false)
			w.fitContent()
			// Synchronize Walk's proposed size even if the native size did not
			// change. DPI's suggested rectangle must not drive later layouts.
			_ = w.MW.SetBoundsPixels(w.MW.BoundsPixels())
			return 0
		}
		result, _, _ := geometryDefSubclass.Call(hwnd, uintptr(msg), wp, lp)
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

func (w *Window) inDisabledCaptionButtons(position uintptr) bool {
	var caption, window win.RECT
	hr, _, _ := captionBoundsProc.Call(uintptr(w.MW.Handle()), 5, uintptr(unsafe.Pointer(&caption)), unsafe.Sizeof(caption))
	if int32(hr) < 0 || caption.Right <= caption.Left {
		return false
	}
	if !win.GetWindowRect(w.MW.Handle(), &window) {
		return false
	}
	x := int32(int16(position&0xffff)) - window.Left
	y := int32(int16((position>>16)&0xffff)) - window.Top
	closeLeft := caption.Right - (caption.Right-caption.Left)/3
	return x >= caption.Left && x < closeLeft && y >= caption.Top && y < caption.Bottom
}

func (w *Window) disableWindowCommands() {
	menu := win.GetSystemMenu(w.MW.Handle(), false)
	for _, command := range []uint32{win.SC_MINIMIZE, win.SC_MAXIMIZE, win.SC_RESTORE, win.SC_SIZE} {
		win.EnableMenuItem(menu, command, win.MF_BYCOMMAND|win.MF_GRAYED)
	}
	win.DrawMenuBar(w.MW.Handle())
}
