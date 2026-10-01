//go:build windows

package popup

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"
	"syscall"
	"touchdict/internal/mainwindow"
	"touchdict/internal/model"
	"touchdict/internal/uistyle"
	"unsafe"
)

const (
	cardWidth     = 600
	cardMinWidth  = 400
	cardMinHeight = 400
)

type Callbacks struct {
	Speak    func()
	Retry    func()
	Settings func()
	Hidden   func()
}
type Window struct {
	MW                                               *walk.MainWindow
	term, pos, meaning, example, translation, status *walk.TextLabel
	speak, retry, pin, copyExample                   *walk.PushButton
	anchor                                           walk.Point
	anchored, pinned                                 bool
	dismissCallback                                  uintptr
	mouseWasDown                                     bool
	onHide                                           func()
	toast                                            *uistyle.CopyToast
	termSize, contentSize                            int
	fitting                                          bool
	subclassCallback                                 uintptr
	fitQueued                                        bool
	dragging                                         bool
	content                                          *walk.Composite
	scroll                                           *walk.ScrollView
	selectionBounds                                  *model.SelectionBounds
	selectionPoint                                   walk.Point
	selectionWork                                    rect
	autoPlacement                                    bool
}
type rect struct{ Left, Top, Right, Bottom int32 }
type monitorInfo struct {
	Size          uint32
	Monitor, Work rect
	Flags         uint32
}

func New(c Callbacks) (*Window, error) {
	mw, e := walk.NewMainWindow()
	if e != nil {
		return nil, e
	}
	mw.SetTitle("TouchDict")
	style := win.GetWindowLong(mw.Handle(), win.GWL_STYLE)
	win.SetWindowLong(mw.Handle(), win.GWL_STYLE, style|win.WS_MAXIMIZEBOX|win.WS_MINIMIZEBOX|win.WS_CAPTION|win.WS_SYSMENU|win.WS_THICKFRAME)
	menu := win.GetSystemMenu(mw.Handle(), false)
	win.EnableMenuItem(menu, win.SC_MINIMIZE, win.MF_BYCOMMAND|win.MF_GRAYED)
	win.EnableMenuItem(menu, win.SC_MAXIMIZE, win.MF_BYCOMMAND|win.MF_GRAYED)
	exStyle := win.GetWindowLong(mw.Handle(), win.GWL_EXSTYLE)
	win.SetWindowLong(mw.Handle(), win.GWL_EXSTYLE, (exStyle&^win.WS_EX_TOOLWINDOW)|win.WS_EX_NOACTIVATE)
	win.SetWindowPos(mw.Handle(), 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
	mw.SetSizePixels(walk.Size{Width: cardWidth, Height: cardMinHeight})
	mw.SetMinMaxSizePixels(walk.Size{Width: cardMinWidth, Height: cardMinHeight}, walk.Size{Width: cardWidth})
	baseFont := shellFont(14, 0)
	if baseFont != nil {
		mw.SetFont(baseFont)
	}
	w := &Window{MW: mw, onHide: c.Hidden}
	scroll, _ := walk.NewScrollView(mw)
	w.scroll = scroll
	scroll.SetScrollbars(false, false)
	scrollLayout := walk.NewVBoxLayout()
	scrollLayout.SetMargins(walk.Margins{})
	scrollLayout.SetSpacing(0)
	_ = scroll.SetLayout(scrollLayout)
	content, _ := walk.NewComposite(scroll)
	w.content = content
	termRow, _ := walk.NewComposite(content)
	w.term, _ = walk.NewTextLabel(termRow)
	w.retry, _ = walk.NewPushButton(termRow)
	w.retry.SetText("重试")
	w.retry.Clicked().Attach(func() {
		if c.Retry != nil {
			c.Retry()
		}
	})
	_ = termRow.SetLayout(mainwindow.ResultLayout("term", w.term))
	w.pos, _ = walk.NewTextLabel(content)
	w.meaning, _ = walk.NewTextLabel(content)
	exampleRow, _ := walk.NewComposite(content)
	w.example, _ = walk.NewTextLabel(exampleRow)
	w.copyExample, _ = walk.NewPushButton(exampleRow)
	_ = w.copyExample.SetAlignment(walk.AlignHNearVNear)
	w.copyExample.SetText("复制")
	_ = exampleRow.SetLayout(mainwindow.ResultLayout("example", w.example))
	w.copyExample.Clicked().Attach(func() {
		if text := w.example.Text(); text != "" {
			if err := walk.Clipboard().SetText(text); err == nil {
				w.toast.Show()
			}
		}
	})
	w.translation, _ = walk.NewTextLabel(content)
	w.status, _ = walk.NewTextLabel(content)
	for _, label := range []*walk.TextLabel{w.term, w.pos, w.meaning, w.example, w.translation, w.status} {
		_ = label.SetTextAlignment(walk.AlignHNearVNear)
		_ = label.SetMinMaxSizePixels(walk.Size{Width: 1}, walk.Size{Width: 16777215})
	}
	_ = content.SetLayout(mainwindow.ResultLayout("column", w.pos))
	_ = mw.SetLayout(mainwindow.CardLayout(content, w.pos))
	row, _ := walk.NewComposite(mw)
	_ = row.SetLayout(mainwindow.ResultLayout("footer", nil))
	w.speak, _ = walk.NewPushButton(row)
	w.speak.SetText("朗读")
	w.speak.Clicked().Attach(func() {
		if c.Speak != nil {
			c.Speak()
		}
	})
	w.pin, _ = walk.NewPushButton(row)
	w.pin.SetText("固顶")
	w.pin.Clicked().Attach(func() {
		w.pinned = !w.pinned
		if w.pinned {
			w.captureAnchor()
			w.pin.SetText("取消固顶")
			_ = w.pin.SetMinMaxSizePixels(walk.Size{Width: 126, Height: 42}, walk.Size{Width: 126, Height: 42})
		} else {
			w.pin.SetText("固顶")
			uistyle.FitButton(w.pin)
		}
		w.applyZOrder()
	})
	for _, button := range []*walk.PushButton{w.speak, w.retry, w.pin, w.copyExample} {
		uistyle.FitButton(button)
	}
	w.toast, e = uistyle.NewCopyToast(mw, mw)
	if e != nil {
		mw.Dispose()
		return nil, e
	}
	w.SetFontSizes(30, 12)
	w.dismissCallback = syscall.NewCallback(func(hwnd uintptr, msg uint32, timer, tick uintptr) uintptr {
		w.dismissOnOutsideClick()
		return 0
	})
	if e := w.trackGeometry(); e != nil {
		mw.Dispose()
		return nil, e
	}
	mw.SizeChanged().Attach(w.queueFit)
	mw.Closing().Attach(func(cancel *bool, reason walk.CloseReason) { *cancel = true; w.Hide() })
	w.Update(model.ViewState{Kind: model.ViewEmpty, Message: "将三指轻点映射为左 Alt，或按 Ctrl+Alt+D 查词。"})
	return w, nil
}
func (w *Window) ShowAt(p walk.Point, s model.ViewState) {
	w.ShowSelection(p, nil, s)
}

func (w *Window) ShowSelection(p walk.Point, bounds *model.SelectionBounds, s model.ViewState) {
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
func (w *Window) fitContent() {
	if w.fitting || w.dragging || win.IsIconic(w.MW.Handle()) || w.content == nil {
		return
	}
	w.fitting = true
	defer func() { w.fitting = false }()
	size := w.MW.SizePixels()
	client := w.MW.ClientBoundsPixels().Size()
	outerWidth := w.contentWidth() + 24 + size.Width - client.Width
	outerWidth = max(cardMinWidth, min(cardWidth, outerWidth))
	width := max(1, outerWidth-(size.Width-client.Width))
	lineHeight := w.contentLineHeight()
	required := mainwindow.ResultHeight(w.content, max(1, width-24)) + 42 + 24 + lineHeight + size.Height - client.Height
	position := w.MW.BoundsPixels()
	work := monitorWorkArea(walk.Point{X: position.X + size.Width/2, Y: position.Y + 30})
	if w.autoPlacement {
		work = w.selectionWork
	}
	maxHeight := max(1, int(work.Bottom-work.Top)-24)
	height := min(maxHeight, max(cardMinHeight, required))
	w.scroll.SetScrollbars(false, required > maxHeight)
	if required > maxHeight {
		// Re-measure with the scrollbar's reserved width so wrapped text is
		// fully represented by the scrollable content extent.
		width -= int(win.GetSystemMetricsForDpi(win.SM_CXVSCROLL, uint32(w.MW.DPI())))
		_ = mainwindow.ResultHeight(w.content, max(1, width-24))
	}
	if size.Width != outerWidth || size.Height != height {
		_ = w.MW.SetSizePixels(walk.Size{Width: outerWidth, Height: height})
	}
	if w.autoPlacement {
		w.placeBesideSelection()
	} else if w.MW.Visible() {
		position = w.MW.BoundsPixels()
		x := max(int(work.Left)+12, min(position.X, int(work.Right)-12-outerWidth))
		y := max(int(work.Top)+12, min(position.Y, int(work.Bottom)-12-height))
		if x != position.X || y != position.Y {
			position.X, position.Y = x, y
			_ = w.MW.SetBoundsPixels(position)
		}
	}
}

func (w *Window) SetFontSizes(termSize, contentSize int) {
	w.MW.SetSuspended(true)
	w.term.SetFont(shellFont(termSize, 0))
	font := shellFont(contentSize, 0)
	for _, label := range []*walk.TextLabel{w.pos, w.meaning, w.example, w.translation, w.status} {
		label.SetFont(font)
	}
	w.termSize, w.contentSize = termSize, contentSize
	w.MW.SetSuspended(false)
	w.fitContent()
}
func (w *Window) SetTermSize(size int)    { w.SetFontSizes(size, w.contentSize) }
func (w *Window) SetContentSize(size int) { w.SetFontSizes(w.termSize, size) }
func (w *Window) Update(s model.ViewState) {
	w.MW.SetSuspended(true)
	if w.toast != nil {
		w.toast.Hide()
	}
	defer func() {
		for _, label := range []*walk.TextLabel{w.pos, w.meaning, w.example, w.translation, w.status} {
			label.SetVisible(label.Text() != "")
		}
		w.MW.SetSuspended(false)
		w.fitContent()
	}()
	w.retry.SetVisible(false)
	w.speak.SetEnabled(false)
	w.copyExample.SetVisible(false)
	switch s.Kind {
	case model.ViewLoading:
		w.term.SetText(s.Selection)
		w.pos.SetText("")
		w.meaning.SetText("正在理解当前语境…")
		w.example.SetText("")
		w.translation.SetText("")
		w.status.SetText("请稍候")
	case model.ViewSuccess:
		d := s.Definition
		w.term.SetText(d.Term)
		w.pos.SetText(d.PartOfSpeech)
		w.meaning.SetText(d.MeaningZH)
		w.example.SetText(d.ExampleEN)
		w.translation.SetText(d.ExampleZH)
		if s.Message == "缓存结果" {
			w.status.SetText("")
		} else {
			w.status.SetText(s.Message)
		}
		w.speak.SetEnabled(true)
		w.copyExample.SetVisible(d.ExampleEN != "")
	case model.ViewError:
		w.term.SetText(s.Selection)
		w.pos.SetText("")
		w.meaning.SetText(s.Message)
		w.example.SetText("")
		w.translation.SetText("")
		w.status.SetText("查询失败")
		w.retry.SetVisible(true)
	default:
		w.term.SetText("TouchDict")
		w.pos.SetText("")
		w.meaning.SetText(s.Message)
		w.example.SetText("")
		w.translation.SetText("")
		w.status.SetText("")
	}
	w.MW.SetTitle("TouchDict")
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
	w.fitContent()
	w.applyAnchor()
	showWindow.Call(hwnd, 4) // SW_SHOWNOACTIVATE: never compete with the main window.
	w.disableWindowCommands()
	w.mouseWasDown = popupMouseDown()
	win.SetTimer(w.MW.Handle(), 0x5445, 30, w.dismissCallback)
}
func (w *Window) SetStatus(message string) {
	w.status.SetText(message)
	w.status.SetVisible(message != "")
	w.fitContent()
}
func (w *Window) Hide() {
	win.KillTimer(w.MW.Handle(), 0x5445)
	if w.toast != nil {
		w.toast.Hide()
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
