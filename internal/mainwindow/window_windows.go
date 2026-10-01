//go:build windows

package mainwindow

import (
	"fmt"
	"html"
	"strings"
	"syscall"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"touchdict/internal/model"
	"touchdict/internal/uistyle"
)

type Callbacks struct {
	Lookup             func(string)
	SelectHistory      func(string)
	Speak              func(string)
	InitialTermSize    int
	TermSizeChanged    func(int)
	InitialContentSize int
	ContentSizeChanged func(int)
}

type Window struct {
	MW                                               *walk.MainWindow
	input                                            *walk.LineEdit
	history                                          *walk.ListBox
	term, pos, meaning, example, translation, status *walk.TextLabel
	suggestions                                      *walk.LinkLabel
	speak, queryButton, retry                        *walk.PushButton
	pin                                              *walk.PushButton
	entries                                          []model.HistoryEntry
	callbacks                                        Callbacks
	querying                                         bool
	currentQuery                                     string
	termSize                                         int
	fontFamily                                       string
	fontSizeLabel                                    *walk.TextLabel
	copyExample                                      *walk.PushButton
	contentSize                                      int
	resultFonts                                      []*walk.Font
	icon                                             *walk.Icon
	pinned                                           bool
	userHeight, resizeStartHeight                    int
	userSizing, fittingHeight                        bool
	resizeCallback                                   uintptr
	toast                                            *uistyle.CopyToast
}

var setWindowPos = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowPos")

func New(c Callbacks) (*Window, error) {
	mw, err := walk.NewMainWindow()
	if err != nil {
		return nil, err
	}
	mw.SetTitle("TouchDict")
	baseFont, err := walk.NewFont("Microsoft YaHei UI", 14, 0)
	if err != nil {
		mw.Dispose()
		return nil, err
	}
	mw.SetFont(baseFont)
	mw.SetSizePixels(walk.Size{Width: 1200, Height: 1000})
	mw.SetMinMaxSizePixels(walk.Size{Width: 820, Height: 560}, walk.Size{})
	root := walk.NewVBoxLayout()
	root.SetMargins(walk.Margins{HNear: 12, VNear: 12, HFar: 12, VFar: 12})
	root.SetSpacing(12)
	_ = mw.SetLayout(&widthPreservingLayout{BoxLayout: root, mode: "root"})
	w := &Window{MW: mw, callbacks: c, fontFamily: mw.Font().Family(), userHeight: 1000}
	if err := w.trackUserResize(); err != nil {
		mw.Dispose()
		return nil, err
	}
	if icon, iconErr := walk.NewIconFromResourceId(2); iconErr == nil {
		w.icon = icon
		_ = mw.SetIcon(icon)
	}
	searchRow, _ := walk.NewComposite(mw)
	searchLayout := walk.NewHBoxLayout()
	searchLayout.SetMargins(walk.Margins{})
	_ = searchRow.SetLayout(&widthPreservingLayout{BoxLayout: searchLayout, mode: "search"})
	inputFrame, _ := walk.NewComposite(searchRow)
	border, _ := walk.NewSolidColorBrush(walk.RGB(110, 110, 110))
	inputFrame.SetBackground(border)
	mw.Disposing().Attach(func() { border.Dispose() })
	inputLayout := walk.NewVBoxLayout()
	inputLayout.SetMargins(walk.Margins{})
	_ = inputFrame.SetLayout(&widthPreservingLayout{BoxLayout: inputLayout, mode: "input"})
	w.input, _ = walk.NewLineEdit(inputFrame)
	w.input.SetCueBanner("输入英文单词、短语或句子")
	// The wrapper paints an explicit one-pixel outline on every side.
	style := win.GetWindowLong(w.input.Handle(), win.GWL_STYLE)
	win.SetWindowLong(w.input.Handle(), win.GWL_STYLE, style&^win.WS_BORDER)
	exStyle := win.GetWindowLong(w.input.Handle(), win.GWL_EXSTYLE)
	win.SetWindowLong(w.input.Handle(), win.GWL_EXSTYLE, exStyle&^win.WS_EX_CLIENTEDGE)
	win.SetWindowPos(w.input.Handle(), 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
	w.queryButton, _ = walk.NewPushButton(searchRow)
	w.queryButton.SetText("查询")
	_ = w.queryButton.SetMinMaxSize(walk.Size{Width: 80, Height: 34}, walk.Size{Width: 80, Height: 34})
	uistyle.FitButton(w.queryButton)
	w.input.TextChanged().Attach(func() {
		w.queryButton.SetEnabled(!w.querying || strings.TrimSpace(w.input.Text()) != w.currentQuery)
	})
	submit := func() {
		if q := strings.TrimSpace(w.input.Text()); q != "" && c.Lookup != nil {
			if w.querying && q == w.currentQuery {
				return
			}
			w.currentQuery = q
			c.Lookup(q)
		} else {
			w.status.SetText("请输入要查询的英文内容")
		}
	}
	w.queryButton.Clicked().Attach(submit)
	w.input.KeyDown().Attach(func(key walk.Key) {
		if key == walk.KeyReturn {
			submit()
		}
	})
	body, _ := walk.NewComposite(mw)
	_ = body.SetMinMaxSize(walk.Size{}, walk.Size{Width: 16777215, Height: 16777215})
	bodyLayout := walk.NewHBoxLayout()
	bodyLayout.SetMargins(walk.Margins{})
	bodyLayout.SetSpacing(0)
	_ = bodyLayout.SetAlignment(walk.AlignHNearVNear)
	_ = body.SetLayout(&widthPreservingLayout{BoxLayout: bodyLayout, mode: "body"})
	w.history, _ = walk.NewListBox(body)
	w.history.SendMessage(win.LB_SETITEMHEIGHT, 0, uintptr(w.history.IntFrom96DPI(28)))
	w.history.SizeChanged().Attach(func() {
		w.history.SendMessage(win.LB_SETITEMHEIGHT, 0, uintptr(w.history.IntFrom96DPI(28)))
	})
	result, _ := walk.NewComposite(body)
	_ = result.SetAlignment(walk.AlignHNearVNear)
	_ = result.SetMinMaxSize(walk.Size{Height: 400}, walk.Size{Width: 16777215, Height: 16777215})
	resultLayout := walk.NewHBoxLayout()
	resultLayout.SetMargins(walk.Margins{HNear: 12, VNear: 0, HFar: 0, VFar: 0})
	resultLayout.SetSpacing(12)
	_ = resultLayout.SetAlignment(walk.AlignHNearVNear)
	_ = result.SetLayout(&widthPreservingLayout{BoxLayout: resultLayout, mode: "result"})
	resultContent, _ := walk.NewComposite(result)
	contentColumn := walk.NewVBoxLayout()
	contentColumn.SetMargins(walk.Margins{})
	contentColumn.SetSpacing(20)
	_ = contentColumn.SetAlignment(walk.AlignHNearVNear)
	columnLayout := &widthPreservingLayout{BoxLayout: contentColumn, mode: "column"}
	_ = resultContent.SetLayout(columnLayout)
	termRow, _ := walk.NewComposite(resultContent)
	termLayout := walk.NewHBoxLayout()
	termLayout.SetMargins(walk.Margins{})
	w.term, _ = walk.NewTextLabel(termRow)
	_ = w.term.SetTextAlignment(walk.AlignHNearVNear)
	_ = w.term.SetMinMaxSize(walk.Size{Width: 1}, walk.Size{Width: 16777215})
	w.retry, _ = walk.NewPushButton(termRow)
	w.retry.SetText("重试")
	uistyle.FitButton(w.retry)
	w.retry.Clicked().Attach(submit)
	_ = termRow.SetLayout(&widthPreservingLayout{BoxLayout: termLayout, mode: "term", term: w.term, trimTop: true})
	w.fontSizeLabel, _ = walk.NewTextLabel(result)
	w.fontSizeLabel.SetVisible(false)
	_ = w.fontSizeLabel.SetTextAlignment(walk.AlignHFarVNear)
	if font, err := walk.NewFont("Microsoft YaHei UI", 12, 0); err == nil {
		w.fontSizeLabel.SetFont(font)
	}
	w.pos, _ = walk.NewTextLabel(resultContent)
	columnLayout.contentFont = w.pos
	_ = w.pos.SetTextAlignment(walk.AlignHNearVNear)
	_ = w.pos.SetMinMaxSize(walk.Size{Width: 1}, walk.Size{Width: 16777215})
	w.meaning, _ = walk.NewTextLabel(resultContent)
	_ = w.meaning.SetTextAlignment(walk.AlignHNearVNear)
	_ = w.meaning.SetMinMaxSize(walk.Size{Width: 1}, walk.Size{Width: 16777215})
	exampleRow, _ := walk.NewComposite(resultContent)
	exampleLayout := walk.NewHBoxLayout()
	exampleLayout.SetMargins(walk.Margins{})
	exampleLayout.SetSpacing(12)
	_ = exampleLayout.SetAlignment(walk.AlignHNearVNear)
	w.example, _ = walk.NewTextLabel(exampleRow)
	_ = w.example.SetTextAlignment(walk.AlignHNearVNear)
	_ = w.example.SetMinMaxSize(walk.Size{Width: 1}, walk.Size{Width: 16777215})
	w.copyExample, _ = walk.NewPushButton(exampleRow)
	w.copyExample.SetText("复制")
	_ = w.copyExample.SetMinMaxSize(walk.Size{Width: 64}, walk.Size{Width: 64})
	uistyle.FitButton(w.copyExample)
	_ = exampleRow.SetLayout(&widthPreservingLayout{BoxLayout: exampleLayout, mode: "term", term: w.example, firstLine: true})
	w.copyExample.Clicked().Attach(func() {
		if text := w.example.Text(); text != "" {
			if err := walk.Clipboard().SetText(text); err == nil {
				w.toast.Show()
			}
		}
	})
	w.translation, _ = walk.NewTextLabel(resultContent)
	_ = w.translation.SetTextAlignment(walk.AlignHNearVNear)
	_ = w.translation.SetMinMaxSize(walk.Size{Width: 1}, walk.Size{Width: 16777215})
	w.suggestions, _ = walk.NewLinkLabel(resultContent)
	w.suggestions.LinkActivated().Attach(func(link *walk.LinkLabelLink) { w.input.SetText(link.URL()); submit() })
	w.status, _ = walk.NewTextLabel(resultContent)
	_ = w.status.SetMinMaxSize(walk.Size{Width: 1}, walk.Size{Width: 16777215})
	actions, _ := walk.NewComposite(result)
	actionLayout := walk.NewHBoxLayout()
	actionLayout.SetMargins(walk.Margins{})
	actionLayout.SetSpacing(12)
	actions.SetLayout(&widthPreservingLayout{BoxLayout: actionLayout, mode: "footer"})
	w.speak, _ = walk.NewPushButton(actions)
	w.speak.SetText("朗读")
	w.speak.Clicked().Attach(func() {
		if c.Speak != nil {
			c.Speak(w.term.Text())
		}
	})
	w.pin, _ = walk.NewPushButton(actions)
	w.pin.SetText("固顶")
	_ = w.pin.SetMinMaxSize(walk.Size{Width: 80, Height: 34}, walk.Size{Width: 80, Height: 34})
	w.pin.Clicked().Attach(func() { w.pinned = !w.pinned; w.applyPinned() })
	_ = w.speak.SetMinMaxSize(walk.Size{Width: 64, Height: 34}, walk.Size{Width: 64, Height: 34})
	_ = w.retry.SetMinMaxSize(walk.Size{Width: 64, Height: 34}, walk.Size{Width: 64, Height: 34})
	for _, button := range []*walk.PushButton{w.speak, w.retry, w.pin} {
		uistyle.FitButton(button)
	}
	w.history.CurrentIndexChanged().Attach(func() {
		i := w.history.CurrentIndex()
		if i >= 0 && i < len(w.entries) && c.SelectHistory != nil {
			w.input.SetText(w.entries[i].Query)
			c.SelectHistory(w.entries[i].Key)
		}
	})
	w.toast, err = uistyle.NewCopyToast(result, mw)
	if err != nil {
		mw.Dispose()
		return nil, err
	}
	w.applyTermSize(c.InitialTermSize)
	w.applyContentSize(c.InitialContentSize)
	zoomKey := func(key walk.Key) {
		if !walk.ControlDown() {
			return
		}
		if key == walk.Key0 || key == walk.KeyNumpad0 {
			w.changeTermSize(30 - w.termSize)
			w.changeContentSize(12 - w.contentSize)
			return
		}
		delta := 0
		if key == walk.KeyAdd || key == walk.Key(0xBB) {
			delta = 1
		}
		if key == walk.KeySubtract || key == walk.Key(0xBD) {
			delta = -1
		}
		if delta != 0 {
			w.changeTermSize(delta)
			w.changeContentSize(delta)
		}
	}
	mw.KeyDown().Attach(zoomKey)
	for _, widget := range []walk.Widget{w.input, w.history, w.queryButton, w.speak, w.retry, w.pin, w.copyExample, w.suggestions} {
		widget.KeyDown().Attach(zoomKey)
	}
	mw.Closing().Attach(func(cancel *bool, reason walk.CloseReason) { *cancel = true; w.Hide() })
	w.Update(model.ViewState{Kind: model.ViewEmpty, Message: "在顶部输入英文开始查询"})
	return w, nil
}

func (w *Window) Show() {
	w.MW.Show()
	w.growForContent()
	_ = w.input.SetFocus()
}
func (w *Window) Hide() {
	if w.toast != nil {
		w.toast.Hide()
	}
	w.MW.Hide()
}
func (w *Window) Close() {
	w.MW.Dispose()
	if w.icon != nil {
		w.icon.Dispose()
	}
	for _, font := range w.resultFonts {
		font.Dispose()
	}
}

func (w *Window) changeTermSize(delta int) {
	before := w.termSize
	w.applyTermSize(before + delta)
	if w.termSize != before && w.callbacks.TermSizeChanged != nil {
		w.callbacks.TermSizeChanged(w.termSize)
	}
}

func (w *Window) applyPinned() {
	insertAfter := ^uintptr(1)
	if w.pinned {
		insertAfter = ^uintptr(0)
		w.pin.SetText("取消固顶")
		_ = w.pin.SetMinMaxSizePixels(walk.Size{Width: 126, Height: 42}, walk.Size{Width: 126, Height: 42})
	} else {
		w.pin.SetText("固顶")
		uistyle.FitButton(w.pin)
	}
	setWindowPos.Call(uintptr(w.MW.Handle()), insertAfter, 0, 0, 0, 0, 0x0001|0x0002|0x0010|0x0040)
}

func (w *Window) SetHistory(entries []model.HistoryEntry) {
	w.entries = append([]model.HistoryEntry(nil), entries...)
	labels := make([]string, len(entries))
	for i, entry := range entries {
		labels[i] = entry.Query
		if labels[i] == "" {
			labels[i] = entry.Definition.Term
		}
	}
	_ = w.history.SetModel(labels)
	w.history.SendMessage(win.LB_SETITEMHEIGHT, 0, uintptr(w.history.IntFrom96DPI(28)))
	w.history.SendMessage(win.LB_SETHORIZONTALEXTENT, 0, 0)
}

func (w *Window) applyTermSize(size int) {
	if size == 0 {
		size = 30
	}
	if size < 8 {
		size = 8
	}
	if size > 72 {
		size = 72
	}
	if size == w.termSize {
		return
	}
	type fontTarget struct {
		widget walk.Widget
		size   int
		style  walk.FontStyle
	}
	targets := []fontTarget{{w.term, size, 0}}
	for _, target := range targets {
		font, err := walk.NewFont(w.fontFamily, target.size, target.style)
		if err == nil {
			target.widget.SetFont(font)
			// Walk caches fonts by family, size and style. Keep each font alive
			// until the window closes: adjacent scales can share the same font.
			known := false
			for _, existing := range w.resultFonts {
				if existing == font {
					known = true
					break
				}
			}
			if !known {
				w.resultFonts = append(w.resultFonts, font)
			}
		}
	}
	w.termSize = size
	w.updateSizeLabel()
	w.growForContent()
}

func (w *Window) changeContentSize(delta int) {
	before := w.contentSize
	w.applyContentSize(before + delta)
	if before != w.contentSize && w.callbacks.ContentSizeChanged != nil {
		w.callbacks.ContentSizeChanged(w.contentSize)
	}
}

func (w *Window) applyContentSize(size int) {
	if size == 0 {
		size = 12
	}
	if size < 8 {
		size = 8
	}
	if size > 72 {
		size = 72
	}
	font, err := walk.NewFont(w.fontFamily, size, 0)
	if err != nil {
		return
	}
	for _, widget := range []walk.Widget{w.pos, w.meaning, w.example, w.translation, w.suggestions, w.status} {
		widget.SetFont(font)
	}
	// These fonts are shared through Walk's global cache; retain them for
	// the window lifetime rather than deleting a handle another widget uses.
	w.contentSize = size
	w.updateSizeLabel()
	w.growForContent()
}

func (w *Window) updateSizeLabel() {
	contentSize := w.contentSize
	if contentSize == 0 {
		contentSize = 12
	}
	w.fontSizeLabel.SetText(fmt.Sprintf("%d/%d pt", w.termSize, contentSize))
}

// Grow only when wrapped results need more room; preserve user resizing.
func (w *Window) growForContent() {
	if win.IsZoomed(w.MW.Handle()) || win.IsIconic(w.MW.Handle()) || w.userSizing || w.fittingHeight {
		return
	}
	client := w.MW.ClientBoundsPixels().Size()
	if client.Width <= 0 {
		return
	}
	minimum := walk.CreateLayoutItemsForContainer(w.MW).(*widthPreservingLayoutItem).requiredSize(client)
	size := w.MW.SizePixels()
	height := max(w.userHeight, minimum.Height+size.Height-client.Height)
	if height == size.Height {
		return
	}
	w.fittingHeight = true
	defer func() { w.fittingHeight = false }()
	setWindowPos.Call(uintptr(w.MW.Handle()), 0, 0, 0, uintptr(size.Width), uintptr(height), win.SWP_NOMOVE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
}

func (w *Window) SetInput(text string) { w.input.SetText(text) }

func (w *Window) Update(s model.ViewState) {
	if w.toast != nil {
		w.toast.Hide()
	}
	w.MW.SetSuspended(true)
	defer func() {
		w.copyExample.SetVisible(w.example.Text() != "")
		w.copyExample.SetEnabled(w.example.Text() != "")
		for _, label := range []*walk.TextLabel{w.pos, w.meaning, w.example, w.translation, w.status} {
			label.SetVisible(label.Text() != "")
		}
		w.MW.SetSuspended(false)
		w.growForContent()
	}()
	w.suggestions.SetVisible(false)
	w.speak.SetEnabled(false)
	w.retry.SetVisible(false)
	w.querying = s.Kind == model.ViewLoading
	w.queryButton.SetEnabled(!w.querying || strings.TrimSpace(w.input.Text()) != w.currentQuery)
	switch s.Kind {
	case model.ViewLoading:
		w.term.SetText(s.Selection)
		w.pos.SetText("")
		w.meaning.SetText("正在查询…")
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
		w.status.SetText(s.Message)
		w.speak.SetEnabled(true)
	case model.ViewSuggestions:
		w.term.SetText(s.Selection)
		w.pos.SetText("")
		w.meaning.SetText("你可能想查：")
		w.example.SetText("")
		w.translation.SetText("")
		w.status.SetText("")
		links := make([]string, 0, len(s.Suggestions))
		for _, value := range s.Suggestions {
			escaped := html.EscapeString(value)
			links = append(links, fmt.Sprintf(`<a href="%s">%s</a>`, escaped, escaped))
		}
		w.suggestions.SetText(strings.Join(links, "    "))
		w.suggestions.SetVisible(true)
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
}
