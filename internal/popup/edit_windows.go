//go:build windows

package popup

import "github.com/lxn/win"

func (w *Window) beginTermEdit() {
	if w.editing || w.lookup == nil {
		return
	}
	w.editing = true
	style := win.GetWindowLong(w.MW.Handle(), win.GWL_EXSTYLE)
	win.SetWindowLong(w.MW.Handle(), win.GWL_EXSTYLE, style&^win.WS_EX_NOACTIVATE)
	win.SetForegroundWindow(w.MW.Handle())
	w.view.Focus()
	w.view.Eval("window.touchdict.beginEdit()")
}
func (w *Window) endTermEdit() {
	if !w.editing {
		return
	}
	w.editing = false
	style := win.GetWindowLong(w.MW.Handle(), win.GWL_EXSTYLE)
	win.SetWindowLong(w.MW.Handle(), win.GWL_EXSTYLE, style|win.WS_EX_NOACTIVATE)
	if w.ready {
		w.view.Eval("window.touchdict.endEdit()")
	}
}
