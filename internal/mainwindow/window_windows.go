//go:build windows

package mainwindow

import (
	"fmt"
	"html"
	"strings"

	"github.com/lxn/walk"
	"touchdict/internal/model"
)

type Callbacks struct {
	Lookup        func(string)
	SelectHistory func(string)
	Speak         func(string)
	InitialScale  int
	ZoomChanged   func(int)
}

type Window struct {
	MW                                               *walk.MainWindow
	input                                            *walk.LineEdit
	history                                          *walk.ListBox
	term, pos, meaning, example, translation, status *walk.TextLabel
	suggestions                                      *walk.LinkLabel
	speak, queryButton, retry                        *walk.PushButton
	entries                                          []model.HistoryEntry
	callbacks                                        Callbacks
	querying                                         bool
	currentQuery                                     string
	fontScale                                        int
	resultFonts                                      []*walk.Font
}

func New(c Callbacks) (*Window, error) {
	mw, err := walk.NewMainWindow()
	if err != nil {
		return nil, err
	}
	mw.SetTitle("TouchDict")
	mw.SetSize(walk.Size{Width: 920, Height: 620})
	mw.SetMinMaxSize(walk.Size{Width: 720, Height: 460}, walk.Size{})
	root := walk.NewVBoxLayout()
	root.SetMargins(walk.Margins{HNear: 16, VNear: 16, HFar: 16, VFar: 16})
	root.SetSpacing(12)
	_ = mw.SetLayout(root)
	w := &Window{MW: mw, callbacks: c}
	searchRow, _ := walk.NewComposite(mw)
	searchLayout := walk.NewHBoxLayout()
	searchLayout.SetMargins(walk.Margins{})
	_ = searchRow.SetLayout(searchLayout)
	w.input, _ = walk.NewLineEdit(searchRow)
	w.input.SetCueBanner("输入英文单词、短语或句子")
	w.queryButton, _ = walk.NewPushButton(searchRow)
	w.queryButton.SetText("查询")
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
	bodyLayout := walk.NewHBoxLayout()
	bodyLayout.SetMargins(walk.Margins{})
	bodyLayout.SetSpacing(14)
	_ = body.SetLayout(bodyLayout)
	w.history, _ = walk.NewListBox(body)
	_ = w.history.SetMinMaxSize(walk.Size{Width: 210}, walk.Size{Width: 210, Height: 16777215})
	result, _ := walk.NewComposite(body)
	resultLayout := walk.NewVBoxLayout()
	resultLayout.SetMargins(walk.Margins{HNear: 12, VNear: 8, HFar: 12, VFar: 8})
	resultLayout.SetSpacing(10)
	_ = result.SetLayout(resultLayout)
	w.term, _ = walk.NewTextLabel(result)
	w.pos, _ = walk.NewTextLabel(result)
	w.meaning, _ = walk.NewTextLabel(result)
	_ = w.meaning.SetMinMaxSize(walk.Size{}, walk.Size{Width: 16777215, Height: 16777215})
	w.example, _ = walk.NewTextLabel(result)
	_ = w.example.SetMinMaxSize(walk.Size{}, walk.Size{Width: 16777215, Height: 16777215})
	w.translation, _ = walk.NewTextLabel(result)
	_ = w.translation.SetMinMaxSize(walk.Size{}, walk.Size{Width: 16777215, Height: 16777215})
	w.suggestions, _ = walk.NewLinkLabel(result)
	w.suggestions.LinkActivated().Attach(func(link *walk.LinkLabelLink) { w.input.SetText(link.URL()); submit() })
	w.status, _ = walk.NewTextLabel(result)
	actions, _ := walk.NewComposite(result)
	actions.SetLayout(walk.NewHBoxLayout())
	w.speak, _ = walk.NewPushButton(actions)
	w.speak.SetText("朗读")
	w.speak.Clicked().Attach(func() {
		if c.Speak != nil {
			c.Speak(w.term.Text())
		}
	})
	w.retry, _ = walk.NewPushButton(actions)
	w.retry.SetText("重试")
	w.retry.Clicked().Attach(submit)
	_, _ = walk.NewHSpacer(actions)
	w.history.CurrentIndexChanged().Attach(func() {
		i := w.history.CurrentIndex()
		if i >= 0 && i < len(w.entries) && c.SelectHistory != nil {
			w.input.SetText(w.entries[i].Query)
			c.SelectHistory(w.entries[i].Key)
		}
	})
	if c.InitialScale < 80 || c.InitialScale > 200 {
		c.InitialScale = 120
	}
	w.applyScale(c.InitialScale)
	zoomKey := func(key walk.Key) {
		if !walk.ControlDown() {
			return
		}
		delta := 0
		if key == walk.KeyAdd || key == walk.Key(0xBB) {
			delta = 10
		}
		if key == walk.KeySubtract || key == walk.Key(0xBD) {
			delta = -10
		}
		if delta != 0 {
			w.applyScale(w.fontScale + delta)
			if c.ZoomChanged != nil {
				c.ZoomChanged(w.fontScale)
			}
		}
	}
	mw.KeyDown().Attach(zoomKey)
	for _, widget := range []walk.Widget{w.input, w.history, w.queryButton, w.speak, w.retry, w.suggestions} {
		widget.KeyDown().Attach(zoomKey)
	}
	mw.Closing().Attach(func(cancel *bool, reason walk.CloseReason) { *cancel = true; mw.Hide() })
	w.Update(model.ViewState{Kind: model.ViewEmpty, Message: "在顶部输入英文开始查询"})
	return w, nil
}

func (w *Window) Show() { w.MW.Show(); _ = w.input.SetFocus() }
func (w *Window) Hide() { w.MW.Hide() }
func (w *Window) Close() {
	w.MW.Dispose()
	for _, font := range w.resultFonts {
		font.Dispose()
	}
}

func (w *Window) SetHistory(entries []model.HistoryEntry) {
	w.entries = append([]model.HistoryEntry(nil), entries...)
	labels := make([]string, len(entries))
	for i, entry := range entries {
		labels[i] = entry.Query
		if labels[i] == "" {
			labels[i] = entry.Definition.Term
		}
		labels[i] = truncateHistory(labels[i], 25)
	}
	_ = w.history.SetModel(labels)
}

func truncateHistory(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit-1]) + "…"
}

func (w *Window) applyScale(scale int) {
	if scale < 80 {
		scale = 80
	}
	if scale > 200 {
		scale = 200
	}
	type fontTarget struct {
		widget walk.Widget
		size   int
		style  walk.FontStyle
	}
	targets := []fontTarget{{w.term, 20, walk.FontBold}, {w.pos, 11, walk.FontBold}, {w.meaning, 13, 0}, {w.example, 12, 0}, {w.translation, 12, 0}, {w.suggestions, 12, 0}, {w.status, 10, 0}}
	newFonts := make([]*walk.Font, 0, len(targets))
	for _, target := range targets {
		font, err := walk.NewFont("Segoe UI", target.size*scale/100, target.style)
		if err == nil {
			target.widget.SetFont(font)
			newFonts = append(newFonts, font)
		}
	}
	old := w.resultFonts
	w.resultFonts = newFonts
	w.fontScale = scale
	for _, font := range old {
		font.Dispose()
	}
}

func (w *Window) SetInput(text string) { w.input.SetText(text) }

func (w *Window) Update(s model.ViewState) {
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
		w.retry.SetVisible(s.CanRetry)
	default:
		w.term.SetText("TouchDict")
		w.pos.SetText("")
		w.meaning.SetText(s.Message)
		w.example.SetText("")
		w.translation.SetText("")
		w.status.SetText("")
	}
}
