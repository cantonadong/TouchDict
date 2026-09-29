//go:build windows

package popup

import (
	"github.com/lxn/walk"
	"syscall"
	"touchdict/internal/model"
	"unsafe"
)

const (
	cardWidth     = 440
	cardMinHeight = 300
	cardMaxHeight = 680
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
	onHide                                           func()
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
	mw.SetSize(walk.Size{Width: cardWidth, Height: cardMinHeight})
	mw.SetMinMaxSize(walk.Size{Width: cardWidth, Height: cardMinHeight}, walk.Size{Width: cardWidth, Height: cardMaxHeight})
	baseFont := shellFont(9, 0)
	if baseFont != nil {
		mw.SetFont(baseFont)
	}
	l := walk.NewVBoxLayout()
	l.SetMargins(walk.Margins{HNear: 20, VNear: 16, HFar: 20, VFar: 16})
	l.SetSpacing(8)
	_ = mw.SetLayout(l)
	w := &Window{MW: mw, onHide: c.Hidden}
	termRow, _ := walk.NewComposite(mw)
	termLayout := walk.NewHBoxLayout()
	termLayout.SetMargins(walk.Margins{})
	_ = termRow.SetLayout(termLayout)
	w.term, _ = walk.NewTextLabel(termRow)
	_ = w.term.SetMinMaxSize(walk.Size{}, walk.Size{Width: 400, Height: 16777215})
	f := shellFont(18, walk.FontBold)
	w.term.SetFont(f)
	_, _ = walk.NewHSpacer(termRow)
	w.pos, _ = walk.NewTextLabel(mw)
	_ = w.pos.SetMinMaxSize(walk.Size{}, walk.Size{Width: 400, Height: 16777215})
	pf := shellFont(10, walk.FontBold)
	w.pos.SetFont(pf)
	w.meaning, _ = walk.NewTextLabel(mw)
	_ = w.meaning.SetMinMaxSize(walk.Size{}, walk.Size{Width: 400, Height: 16777215})
	exampleRow, _ := walk.NewComposite(mw)
	exampleLayout := walk.NewHBoxLayout()
	exampleLayout.SetMargins(walk.Margins{})
	_ = exampleRow.SetLayout(exampleLayout)
	w.example, _ = walk.NewTextLabel(exampleRow)
	_ = w.example.SetMinMaxSize(walk.Size{}, walk.Size{Width: 320, Height: 16777215})
	ef := shellFont(11, 0)
	w.example.SetFont(ef)
	_, _ = walk.NewHSpacer(exampleRow)
	w.copyExample, _ = walk.NewPushButton(exampleRow)
	_ = w.copyExample.SetAlignment(walk.AlignHNearVNear)
	w.copyExample.SetText("复制")
	w.copyExample.Clicked().Attach(func() {
		if text := w.example.Text(); text != "" {
			if err := walk.Clipboard().SetText(text); err == nil {
				w.status.SetText("例句已复制")
			}
		}
	})
	w.translation, _ = walk.NewTextLabel(mw)
	_ = w.translation.SetMinMaxSize(walk.Size{}, walk.Size{Width: 400, Height: 16777215})
	w.status, _ = walk.NewTextLabel(mw)
	_ = w.status.SetMinMaxSize(walk.Size{}, walk.Size{Width: 400, Height: 16777215})
	row, _ := walk.NewComposite(mw)
	rl := walk.NewHBoxLayout()
	rl.SetMargins(walk.Margins{})
	_ = row.SetLayout(rl)
	w.speak, _ = walk.NewPushButton(row)
	w.speak.SetText("朗读")
	w.speak.Clicked().Attach(func() {
		if c.Speak != nil {
			c.Speak()
		}
	})
	w.retry, _ = walk.NewPushButton(row)
	w.retry.SetText("重试")
	w.retry.Clicked().Attach(func() {
		if c.Retry != nil {
			c.Retry()
		}
	})
	w.pin, _ = walk.NewPushButton(row)
	w.pin.SetText("固顶")
	w.pin.Clicked().Attach(func() {
		w.pinned = !w.pinned
		if w.pinned {
			w.captureAnchor()
			w.pin.SetText("取消固顶")
		} else {
			w.pin.SetText("固顶")
		}
		w.applyZOrder()
	})
	_, _ = walk.NewHSpacer(row)
	mw.Deactivating().Attach(func() {
		if !w.pinned {
			w.Hide()
		}
	})
	mw.SizeChanged().Attach(w.enforceWidth)
	mw.Closing().Attach(func(cancel *bool, reason walk.CloseReason) { *cancel = true; w.Hide() })
	w.Update(model.ViewState{Kind: model.ViewEmpty, Message: "将三指轻点映射为左 Alt，或按 Ctrl+Alt+D 查词。"})
	return w, nil
}
func (w *Window) ShowAt(p walk.Point, s model.ViewState) {
	// If the user dragged a pinned window, its native position is the new
	// anchor. Capture it before any content or visibility update can move it.
	if w.pinned {
		w.captureAnchor()
	}
	w.Update(s)
	sz := walk.Size{Width: w.MW.IntFrom96DPI(cardWidth), Height: w.currentHeight()}
	work := monitorWorkArea(p)
	x, y := p.X+18, p.Y+20
	if x+sz.Width > int(work.Right)-12 {
		x = p.X - sz.Width - 18
	}
	if y+sz.Height > int(work.Bottom)-12 {
		y = p.Y - sz.Height - 18
	}
	if x < int(work.Left)+12 {
		x = int(work.Left) + 12
	}
	if y < int(work.Top)+12 {
		y = int(work.Top) + 12
	}
	if !w.pinned || !w.anchored {
		w.anchor = walk.Point{X: x, Y: y}
		w.anchored = true
	}
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
	minimum := w.MW.IntFrom96DPI(cardMinHeight)
	maximum := w.MW.IntFrom96DPI(cardMaxHeight)
	var r rect
	if ok, _, _ := getWindowRect.Call(uintptr(w.MW.Handle()), uintptr(unsafe.Pointer(&r))); ok != 0 {
		height := int(r.Bottom - r.Top)
		if height >= minimum && height <= maximum {
			return height
		}
	}
	return minimum
}
func (w *Window) enforceWidth() {
	var r rect
	if ok, _, _ := getWindowRect.Call(uintptr(w.MW.Handle()), uintptr(unsafe.Pointer(&r))); ok == 0 {
		return
	}
	target := w.MW.IntFrom96DPI(cardWidth)
	if int(r.Right-r.Left) == target {
		return
	}
	setWindowPos.Call(uintptr(w.MW.Handle()), 0, uintptr(r.Left), uintptr(r.Top), uintptr(target), uintptr(r.Bottom-r.Top), 0x0004|0x0010)
}
func (w *Window) Update(s model.ViewState) {
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
		w.retry.SetVisible(s.CanRetry)
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
		width, height := w.MW.IntFrom96DPI(cardWidth), w.currentHeight()
		insertAfter := ^uintptr(1)
		if w.pinned {
			insertAfter = ^uintptr(0)
		}
		setWindowPos.Call(uintptr(w.MW.Handle()), insertAfter, uintptr(w.anchor.X), uintptr(w.anchor.Y), uintptr(width), uintptr(height), 0x0010|0x0040)
	}
}
func (w *Window) applyZOrder() {
	insertAfter := ^uintptr(1)
	if w.pinned {
		insertAfter = ^uintptr(0)
	}
	setWindowPos.Call(uintptr(w.MW.Handle()), insertAfter, 0, 0, 0, 0, 0x0001|0x0002|0x0010|0x0040)
}
func (w *Window) showNative() {
	hwnd := uintptr(w.MW.Handle())
	style, _, _ := getWindowLongPtr.Call(hwnd, ^uintptr(15))
	setWindowLongPtr.Call(hwnd, ^uintptr(15), (style&^0x00010000)|0x00C00000|0x00080000|0x00020000)
	exStyle, _, _ := getWindowLongPtr.Call(hwnd, ^uintptr(19))
	setWindowLongPtr.Call(hwnd, ^uintptr(19), exStyle&^(0x00000008|0x00000080|0x08000000))
	setWindowPos.Call(hwnd, 0, 0, 0, 0, 0, 0x0001|0x0002|0x0004|0x0020)
	w.MW.Show()
	showWindow.Call(hwnd, 9)
	w.applyAnchor()
	setForegroundWindow.Call(hwnd)
}
func (w *Window) SetStatus(message string) { w.status.SetText(message) }
func (w *Window) Hide() {
	w.MW.Hide()
	w.anchored = false
	if w.onHide != nil {
		w.onHide()
	}
}
func (w *Window) Close() { w.MW.Dispose() }

var getSystemMetrics = syscall.NewLazyDLL("user32.dll").NewProc("GetSystemMetrics")
var monitorFromPoint = syscall.NewLazyDLL("user32.dll").NewProc("MonitorFromPoint")
var getMonitorInfo = syscall.NewLazyDLL("user32.dll").NewProc("GetMonitorInfoW")
var getWindowLongPtr = syscall.NewLazyDLL("user32.dll").NewProc("GetWindowLongPtrW")
var setWindowLongPtr = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowLongPtrW")
var setWindowPos = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowPos")
var showWindow = syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow")
var setForegroundWindow = syscall.NewLazyDLL("user32.dll").NewProc("SetForegroundWindow")
var getWindowRect = syscall.NewLazyDLL("user32.dll").NewProc("GetWindowRect")

func shellFont(size int, style walk.FontStyle) *walk.Font {
	for _, family := range []string{"Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei UI"} {
		if font, err := walk.NewFont(family, size, style); err == nil {
			return font
		}
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
