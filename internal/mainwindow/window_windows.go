//go:build windows

package mainwindow

import (
	"encoding/json"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"touchdict/internal/edgeview"
	"touchdict/internal/model"
)

type Callbacks struct {
	Lookup             func(model.Selection)
	Retry              func(model.Selection)
	SelectHistory      func(string)
	DeleteHistory      func(string)
	ClearHistory       func() error
	CancelLookup       func()
	Speak              func(string)
	InitialTermSize    int
	TermSizeChanged    func(int)
	InitialContentSize int
	ContentSizeChanged func(int)
	InitialHeight      int
	HeightChanged      func(int)
	Diagnostics        func(string)
}

type Window struct {
	MW                                            *walk.MainWindow
	view                                          *edgeview.View
	callbacks                                     Callbacks
	entries                                       []model.HistoryEntry
	state                                         model.ViewState
	input, currentQuery, selectedKey              string
	inputRevision                                 uint64
	querying, pinned, ready, closed, focusPending bool
	webFailed                                     bool
	termSize, contentSize                         int
	userHeight, resizeStartHeight                 int
	userSizing, fittingHeight                     bool
	resizeCallback                                uintptr
	icon                                          *walk.Icon
	pending                                       []func()
	contentHeight                                 int
	initializationTimer                           *time.Timer
	failure                                       *walk.TextLabel
}

var setWindowPos = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowPos")

func New(c Callbacks) (*Window, error) {
	mw, err := walk.NewMainWindow()
	if err != nil {
		return nil, err
	}
	mw.SetTitle("TouchDict")
	height := c.InitialHeight
	if height < 560 || height > 32767 {
		height = 1200
	}
	mw.SetSizePixels(walk.Size{Width: 1800, Height: height})
	mw.SetSizePixels(walk.Size{Width: windowHeightForScreenPixels(mw, 1800), Height: windowHeightForScreenPixels(mw, height)})
	mw.SetMinMaxSizePixels(walk.Size{Width: 820, Height: 560}, walk.Size{})
	w := &Window{MW: mw, callbacks: c, userHeight: height, termSize: fontSize(c.InitialTermSize, 24), contentSize: fontSize(c.InitialContentSize, 16), state: model.ViewState{Kind: model.ViewEmpty, Message: "在顶部输入英文开始查询"}}
	// Walk's FormBase.startLayout always measures its client Composite when
	// showing the window. ContainerBase.CreateLayoutItem dereferences Layout(),
	// even though the visible content is supplied by WebView2 rather than Walk.
	root := walk.NewVBoxLayout()
	root.SetMargins(walk.Margins{HNear: 24, VNear: 24, HFar: 24, VFar: 24})
	if err := mw.SetLayout(root); err != nil {
		w.Close()
		return nil, err
	}
	if icon, err := walk.NewIconFromResourceId(2); err == nil {
		w.icon = icon
		_ = mw.SetIcon(icon)
	}
	if err := w.addHistoryMenu(); err != nil {
		w.Close()
		return nil, err
	}
	if err := w.trackUserResize(); err != nil {
		w.Close()
		return nil, err
	}
	w.failure, err = walk.NewTextLabel(mw)
	if err != nil {
		w.Close()
		return nil, err
	}
	w.failure.SetText("正在打开 TouchDict…")
	w.failure.SetBoundsPixels(walk.Rectangle{X: 24, Y: 24, Width: 720, Height: 180})
	// FormBase owns a full-size client Composite above sibling child windows.
	// Embed inside that Composite, so its native background cannot cover Edge.
	host := mw.AsContainerBase()
	view, err := edgeview.New(uintptr(host.Handle()), func(message string) {
		w.enqueue(func() { w.handleMessage(message) })
	}, func(err error) {
		w.enqueue(func() { w.webFailure(err.Error()) })
	}, c.Diagnostics)
	if err != nil {
		w.Close()
		return nil, err
	}
	w.view = view
	host.SizeChanged().Attach(func() {
		if !w.closed {
			w.view.Resize()
		}
	})
	w.view.SetHTML(mainPage())
	w.initializationTimer = time.AfterFunc(30*time.Second, func() {
		mw.Synchronize(func() {
			if !w.ready && !w.closed {
				w.webFailure("系统 WebView2 启动超时，请关闭 TouchDict 后重新打开。")
			}
		})
	})
	mw.Closing().Attach(func(cancel *bool, reason walk.CloseReason) { *cancel = true; w.Hide() })
	mw.Disposing().Attach(func() { w.disposeView() })
	return w, nil
}

// COM callbacks must return before opening native modal dialogs.
func (w *Window) enqueue(fn func()) {
	if w.closed {
		return
	}
	w.pending = append(w.pending, fn)
	win.PostMessage(w.MW.Handle(), win.WM_APP+44, 0, 0)
}

func (w *Window) webFailure(message string) {
	if w.closed {
		return
	}
	w.ready = false
	w.webFailed = true
	if w.callbacks.Diagnostics != nil {
		w.callbacks.Diagnostics("main window unavailable: " + message)
	}
	if w.initializationTimer != nil {
		w.initializationTimer.Stop()
	}
	if w.view != nil {
		w.view.Close()
	}
	w.failure.SetText("无法打开主窗口\n\n" + message + "\n\n浮窗查词仍可通过托盘和快捷键使用。")
	w.failure.SetVisible(true)
	w.failure.SetBoundsPixels(walk.Rectangle{X: 24, Y: 24, Width: max(200, w.MW.ClientBoundsPixels().Width-48), Height: 220})
}

func (w *Window) Show() {
	if win.IsIconic(w.MW.Handle()) {
		win.ShowWindow(w.MW.Handle(), win.SW_RESTORE)
	}
	w.MW.Show()
	if w.view != nil {
		w.view.Show()
		w.view.Resize()
	}
	w.growForContent()
	win.SetForegroundWindow(w.MW.Handle())
	w.focusSearch()
}

func (w *Window) focusSearch() {
	w.focusPending = true
	if !w.ready || w.view == nil {
		return
	}
	w.focusPending = false
	w.view.Focus()
	w.view.Eval("window.touchdict.focusSearch()")
}

func (w *Window) Hide() {
	if w.view != nil {
		w.view.Hide()
	}
	w.MW.Hide()
}

func (w *Window) disposeView() {
	if w.closed {
		return
	}
	w.closed = true
	if w.initializationTimer != nil {
		w.initializationTimer.Stop()
	}
	if w.view != nil {
		w.view.Close()
	}
	w.pending = nil
}

func (w *Window) Close() {
	w.disposeView()
	w.MW.Dispose()
	if w.icon != nil {
		w.icon.Dispose()
		w.icon = nil
	}
}

func fontSize(size, fallback int) int {
	if size == 0 {
		size = fallback
	}
	return max(8, min(72, size))
}

func (w *Window) changeSizes(delta int, reset bool) {
	term, content := fontSize(w.termSize+delta, 24), fontSize(w.contentSize+delta, 16)
	if reset {
		term, content = 24, 16
	}
	if term != w.termSize {
		w.termSize = term
		if w.callbacks.TermSizeChanged != nil {
			w.callbacks.TermSizeChanged(term)
		}
	}
	if content != w.contentSize {
		w.contentSize = content
		if w.callbacks.ContentSizeChanged != nil {
			w.callbacks.ContentSizeChanged(content)
		}
	}
	w.render()
}

func (w *Window) applyPinned() {
	insertAfter := ^uintptr(1)
	if w.pinned {
		insertAfter = ^uintptr(0)
	}
	setWindowPos.Call(uintptr(w.MW.Handle()), insertAfter, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	w.render()
}

func (w *Window) SetInput(text string) {
	w.input = text
	w.inputRevision++
	w.render()
}

func (w *Window) SetHistory(entries []model.HistoryEntry) {
	previousIndex := -1
	for i, entry := range w.entries {
		if entry.Key == w.selectedKey {
			previousIndex = i
			break
		}
	}
	w.entries = append([]model.HistoryEntry(nil), entries...)
	found := false
	for _, entry := range entries {
		if entry.Key == w.selectedKey {
			found = true
			break
		}
	}
	if !found && previousIndex >= 0 {
		w.selectedKey = ""
		if len(entries) > 0 {
			w.selectedKey = entries[min(previousIndex, len(entries)-1)].Key
		}
	}
	w.render()
}

func (w *Window) Update(state model.ViewState) {
	if w.closed {
		return
	}
	w.state = state
	w.querying = state.Kind == model.ViewLoading
	if w.querying {
		w.currentQuery = strings.TrimSpace(state.Selection)
	}
	w.render()
}

func (w *Window) render() {
	if !w.ready || w.closed || w.view == nil {
		return
	}
	state := struct {
		Input         string               `json:"input"`
		InputRevision uint64               `json:"inputRevision"`
		CurrentQuery  string               `json:"currentQuery"`
		SelectedKey   string               `json:"selectedKey"`
		State         model.ViewState      `json:"state"`
		History       []model.HistoryEntry `json:"history"`
		TermSize      int                  `json:"termSize"`
		ContentSize   int                  `json:"contentSize"`
		Pinned        bool                 `json:"pinned"`
	}{w.input, w.inputRevision, w.currentQuery, w.selectedKey, w.state, w.entries, w.termSize, w.contentSize, w.pinned}
	data, err := json.Marshal(state)
	if err == nil {
		w.view.Eval("window.touchdict.applyState(" + string(data) + ")")
	}
}

type monitorInfo struct {
	Size          uint32
	Monitor, Work win.RECT
	Flags         uint32
}

func (w *Window) growForContent() {
	if w.contentHeight <= 0 || w.closed || w.userSizing || w.fittingHeight || win.IsZoomed(w.MW.Handle()) || win.IsIconic(w.MW.Handle()) {
		return
	}
	size := w.MW.SizePixels()
	wanted := windowHeightForScreenPixels(w.MW, w.userHeight)
	info := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	user32 := syscall.NewLazyDLL("user32.dll")
	monitor, _, _ := user32.NewProc("MonitorFromWindow").Call(uintptr(w.MW.Handle()), 2)
	if ok, _, _ := user32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info))); ok != 0 {
		wanted = min(wanted, int(info.Work.Bottom-info.Work.Top))
	}
	if wanted == size.Height {
		return
	}
	w.fittingHeight = true
	defer func() { w.fittingHeight = false }()
	setWindowPos.Call(uintptr(w.MW.Handle()), 0, 0, 0, uintptr(size.Width), uintptr(max(560, wanted)), win.SWP_NOMOVE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
}
