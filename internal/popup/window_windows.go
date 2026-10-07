//go:build windows

package popup

import (
	"encoding/json"
	"github.com/lxn/walk"
	"github.com/lxn/win"
	"strings"
	"syscall"
	"time"
	"touchdict/internal/edgeview"
	"touchdict/internal/model"
	"unsafe"
)

const (
	cardWidth     = 600
	cardMinWidth  = 400
	cardMinHeight = 180
)

type Callbacks struct {
	Speak       func()
	Retry       func()
	Settings    func()
	Hidden      func()
	Lookup      func(string)
	Diagnostics func(string)
}
type Window struct {
	MW                     *walk.MainWindow
	view                   *edgeview.View
	callbacks              Callbacks
	state                  model.ViewState
	failure                *walk.TextLabel
	ready, closed          bool
	webFailed              bool
	initializationTimer    *time.Timer
	pending                []func()
	measuredHeight         int
	anchor                 walk.Point
	anchored, pinned       bool
	dismissCallback        uintptr
	mouseWasDown           bool
	onHide                 func()
	termSize, contentSize  int
	fitting                bool
	subclassCallback       uintptr
	fitQueued, dragging    bool
	selectionBounds        *model.SelectionBounds
	selectionPoint         walk.Point
	selectionWork          rect
	autoPlacement, editing bool
	lookup                 func(string)
}
type rect struct{ Left, Top, Right, Bottom int32 }
type monitorInfo struct {
	Size          uint32
	Monitor, Work rect
	Flags         uint32
}

func New(c Callbacks) (*Window, error) {
	mw, err := walk.NewMainWindow()
	if err != nil {
		return nil, err
	}
	mw.SetTitle("TouchDict")
	style := win.GetWindowLong(mw.Handle(), win.GWL_STYLE)
	win.SetWindowLong(mw.Handle(), win.GWL_STYLE, (style&^(win.WS_MAXIMIZEBOX|win.WS_MINIMIZEBOX))|win.WS_CAPTION|win.WS_SYSMENU|win.WS_THICKFRAME)
	exStyle := win.GetWindowLong(mw.Handle(), win.GWL_EXSTYLE)
	win.SetWindowLong(mw.Handle(), win.GWL_EXSTYLE, (exStyle&^win.WS_EX_TOOLWINDOW)|win.WS_EX_NOACTIVATE)
	win.SetWindowPos(mw.Handle(), 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
	mw.SetSizePixels(walk.Size{Width: cardWidth, Height: cardMinHeight})
	mw.SetMinMaxSizePixels(walk.Size{Width: cardMinWidth, Height: cardMinHeight}, walk.Size{Width: cardWidth})
	root := walk.NewVBoxLayout()
	root.SetMargins(walk.Margins{})
	if err := mw.SetLayout(root); err != nil {
		mw.Dispose()
		return nil, err
	}
	w := &Window{MW: mw, callbacks: c, onHide: c.Hidden, lookup: c.Lookup, termSize: 30, contentSize: 12, state: model.ViewState{Kind: model.ViewEmpty}}
	if err := w.trackGeometry(); err != nil {
		mw.Dispose()
		return nil, err
	}
	w.failure, err = walk.NewTextLabel(mw)
	if err != nil {
		mw.Dispose()
		return nil, err
	}
	w.failure.SetText("正在打开查词卡片…")
	w.failure.SetBoundsPixels(walk.Rectangle{X: 20, Y: 20, Width: 360, Height: 180})
	host := mw.AsContainerBase()
	view, err := edgeview.New(uintptr(host.Handle()), func(message string) { w.enqueue(func() { w.handleMessage(message) }) }, func(err error) { w.enqueue(func() { w.webFailure(err.Error()) }) }, c.Diagnostics)
	if err != nil {
		mw.Dispose()
		return nil, err
	}
	w.view = view
	host.SizeChanged().Attach(func() {
		if !w.closed {
			w.view.Resize()
		}
	})
	view.SetHTML(popupPage())
	w.initializationTimer = time.AfterFunc(30*time.Second, func() {
		mw.Synchronize(func() {
			if !w.ready && !w.closed {
				w.webFailure("WebView2 启动超时，请重新打开 TouchDict")
			}
		})
	})
	w.dismissCallback = syscall.NewCallback(func(hwnd uintptr, msg uint32, timer, tick uintptr) uintptr { w.dismissOnOutsideClick(); return 0 })
	mw.Closing().Attach(func(cancel *bool, reason walk.CloseReason) { *cancel = true; w.Hide() })
	mw.Disposing().Attach(func() {
		w.closed = true
		if w.initializationTimer != nil {
			w.initializationTimer.Stop()
		}
		if w.view != nil {
			w.view.Close()
		}
	})
	return w, nil
}
func (w *Window) enqueue(fn func()) {
	if !w.closed {
		w.pending = append(w.pending, fn)
		win.PostMessage(w.MW.Handle(), win.WM_APP+45, 0, 0)
	}
}
func (w *Window) webFailure(message string) {
	if w.closed {
		return
	}
	w.ready = false
	w.webFailed = true
	if w.initializationTimer != nil {
		w.initializationTimer.Stop()
	}
	if w.view != nil {
		w.view.Close()
	}
	w.failure.SetText(message)
	w.failure.SetVisible(true)
	if w.callbacks.Diagnostics != nil {
		w.callbacks.Diagnostics("popup webview: " + message)
	}
}
func (w *Window) SetFontSizes(termSize, contentSize int) {
	w.termSize, w.contentSize = termSize, contentSize
	w.render()
}
func (w *Window) SetTermSize(size int)     { w.SetFontSizes(size, w.contentSize) }
func (w *Window) SetContentSize(size int)  { w.SetFontSizes(w.termSize, size) }
func (w *Window) Update(s model.ViewState) { w.state = s; w.endTermEdit(); w.render() }
func (w *Window) render() {
	if !w.ready || w.closed {
		return
	}
	data, _ := json.Marshal(struct {
		State       model.ViewState `json:"state"`
		TermSize    int             `json:"termSize"`
		ContentSize int             `json:"contentSize"`
		Pinned      bool            `json:"pinned"`
	}{w.state, w.termSize, w.contentSize, w.pinned})
	w.view.Eval("window.touchdict.applyState(" + string(data) + ")")
}
func (w *Window) handleMessage(message string) {
	if w.closed || w.webFailed || len(message) > 1024*1024 {
		return
	}
	var action struct {
		Action string `json:"action"`
		Text   string `json:"text"`
		Height int    `json:"height"`
	}
	if json.Unmarshal([]byte(message), &action) != nil {
		return
	}
	if action.Action == "ready" {
		if w.ready {
			return
		}
		w.ready = true
		w.initializationTimer.Stop()
		w.failure.SetVisible(false)
		w.render()
		return
	}
	if !w.ready {
		return
	}
	switch action.Action {
	case "speak":
		if w.state.Kind == model.ViewSuccess && w.callbacks.Speak != nil {
			w.callbacks.Speak()
		}
	case "retry":
		if w.callbacks.Retry != nil {
			w.callbacks.Retry()
		}
	case "pin":
		w.pinned = !w.pinned
		if w.pinned {
			w.captureAnchor()
		}
		w.applyZOrder()
		w.render()
	case "copy-example":
		if w.state.Kind == model.ViewSuccess && w.state.Definition.ExampleEN != "" {
			if err := walk.Clipboard().SetText(w.state.Definition.ExampleEN); err == nil {
				w.view.Eval("window.touchdict.notice()")
			}
		}
	case "edit":
		w.beginTermEdit()
	case "edit-cancel":
		w.endTermEdit()
	case "lookup":
		if text := strings.TrimSpace(action.Text); text != "" && w.lookup != nil {
			w.endTermEdit()
			w.lookup(text)
		}
	case "content-height":
		if action.Height > 0 && action.Height < 100000 {
			w.measuredHeight = action.Height
			w.queueFit()
		}
	}
}
func (w *Window) ShowAt(p walk.Point, s model.ViewState) {
	w.ShowSelection(p, nil, s)
}

func (w *Window) ShowSelection(p walk.Point, bounds *model.SelectionBounds, s model.ViewState) {
	w.endTermEdit()
	// If the user dragged a pinned window, its native position is the new
	// anchor. Capture it before any content or visibility update can move it.
	if w.pinned {
		w.captureAnchor()
	}
	w.selectionBounds = bounds
	w.selectionPoint = p
	w.selectionWork = monitorWorkArea(p)
	w.autoPlacement = true
	w.Update(s)
	w.placeBesideSelection()
	w.showNative()
}
func (w *Window) captureAnchor() {
	var r rect
	if ok, _, _ := getWindowRect.Call(uintptr(w.MW.Handle()), uintptr(unsafe.Pointer(&r))); ok != 0 {
		w.anchor = walk.Point{X: int(r.Left), Y: int(r.Top)}
		w.anchored = true
	}
}
func (w *Window) currentHeight() int {
	minimum := cardMinHeight
	var r rect
	if ok, _, _ := getWindowRect.Call(uintptr(w.MW.Handle()), uintptr(unsafe.Pointer(&r))); ok != 0 {
		height := int(r.Bottom - r.Top)
		if height >= minimum {
			return height
		}
	}
	return minimum
}
func (w *Window) applyAnchor() {
	if w.anchored {
		width, height := w.MW.SizePixels().Width, w.currentHeight()
		insertAfter := ^uintptr(0)
		setWindowPos.Call(uintptr(w.MW.Handle()), insertAfter, uintptr(w.anchor.X), uintptr(w.anchor.Y), uintptr(width), uintptr(height), 0x0010|0x0040)
	}
}
func (w *Window) applyZOrder() {
	// A visible lookup card stays above normal windows without activating.
	insertAfter := ^uintptr(0)
	setWindowPos.Call(uintptr(w.MW.Handle()), insertAfter, 0, 0, 0, 0, 0x0001|0x0002|0x0010|0x0040)
}
func (w *Window) showNative() {
	hwnd := uintptr(w.MW.Handle())
	w.MW.Show()
	if w.view != nil {
		w.view.Show()
		w.view.Resize()
	}
	w.fitContent()
	w.applyAnchor()
	showWindow.Call(hwnd, 4) // SW_SHOWNOACTIVATE: never compete with the main window.
	w.disableWindowCommands()
	w.mouseWasDown = popupMouseDown()
	win.SetTimer(w.MW.Handle(), 0x5445, 30, w.dismissCallback)
}
func (w *Window) SetStatus(message string) {
	w.state.Message = message
	w.render()
}
func (w *Window) Hide() {
	w.endTermEdit()
	win.KillTimer(w.MW.Handle(), 0x5445)
	if w.view != nil {
		w.view.Hide()
	}
	w.MW.Hide()
	w.anchored = false
	if w.onHide != nil {
		w.onHide()
	}
}
func (w *Window) Close() { win.KillTimer(w.MW.Handle(), 0x5445); w.MW.Dispose() }

var popupKeyState = syscall.NewLazyDLL("user32.dll").NewProc("GetAsyncKeyState")

func popupMouseDown() bool {
	for _, key := range []uintptr{1, 2, 4} {
		state, _, _ := popupKeyState.Call(key)
		if state&0x8000 != 0 {
			return true
		}
	}
	return false
}

func (w *Window) dismissOnOutsideClick() {
	down := popupMouseDown()
	clicked := down && !w.mouseWasDown
	w.mouseWasDown = down
	if !clicked || w.pinned || !w.MW.Visible() {
		return
	}
	var pointer win.POINT
	var bounds rect
	if !win.GetCursorPos(&pointer) {
		return
	}
	if ok, _, _ := getWindowRect.Call(uintptr(w.MW.Handle()), uintptr(unsafe.Pointer(&bounds))); ok == 0 {
		return
	}
	if pointer.X < bounds.Left || pointer.X >= bounds.Right || pointer.Y < bounds.Top || pointer.Y >= bounds.Bottom {
		w.Hide()
	}
}

var getSystemMetrics = syscall.NewLazyDLL("user32.dll").NewProc("GetSystemMetrics")
var monitorFromPoint = syscall.NewLazyDLL("user32.dll").NewProc("MonitorFromPoint")
var getMonitorInfo = syscall.NewLazyDLL("user32.dll").NewProc("GetMonitorInfoW")
var setWindowPos = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowPos")
var showWindow = syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow")
var getWindowRect = syscall.NewLazyDLL("user32.dll").NewProc("GetWindowRect")

func shellFont(size int, style walk.FontStyle) *walk.Font {
	if font, err := walk.NewFont("Microsoft YaHei UI", size, 0); err == nil {
		return font
	}
	return nil
}

func metric(i uintptr) int { v, _, _ := getSystemMetrics.Call(i); return int(v) }
func monitorWorkArea(p walk.Point) rect {
	packed := uintptr(uint64(uint32(p.X)) | uint64(uint32(p.Y))<<32)
	h, _, _ := monitorFromPoint.Call(packed, 2)
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if h != 0 {
		if ok, _, _ := getMonitorInfo.Call(h, uintptr(unsafe.Pointer(&mi))); ok != 0 {
			return mi.Work
		}
	}
	return rect{0, 0, int32(metric(0)), int32(metric(1))}
}
