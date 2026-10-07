//go:build windows

package mainwindow

import (
	"encoding/json"
	"strings"

	"github.com/lxn/walk"
	"touchdict/internal/model"
)

type pageAction struct {
	Action string `json:"action"`
	Text   string `json:"text"`
	Key    string `json:"key"`
	Delta  int    `json:"delta"`
	Reset  bool   `json:"reset"`
	Height int    `json:"height"`
}

func (w *Window) handleMessage(message string) {
	if w.closed || w.webFailed || len(message) > 1024*1024 {
		return
	}
	var action pageAction
	if json.Unmarshal([]byte(message), &action) != nil {
		return
	}
	if action.Action == "ready" {
		if w.ready {
			return
		}
		w.ready = true
		if w.callbacks.Diagnostics != nil {
			w.callbacks.Diagnostics("webview2: main page ready")
		}
		if w.initializationTimer != nil {
			w.initializationTimer.Stop()
		}
		w.failure.SetVisible(false)
		w.render()
		if w.focusPending {
			w.focusSearch()
		}
		return
	}
	if !w.ready {
		return
	}
	switch action.Action {
	case "input":
		w.input = action.Text
	case "lookup", "retry":
		w.input = action.Text
		q := strings.TrimSpace(action.Text)
		if q == "" {
			w.notice("请输入要查询的英文内容")
			return
		}
		if w.querying && action.Action == "lookup" && q == w.currentQuery {
			return
		}
		sentence := w.state.Context
		if action.Action == "lookup" {
			sentence = ""
		}
		selection := model.Selection{Text: q, Context: sentence, Multiword: len(strings.Fields(q)) > 1}
		w.currentQuery, w.selectedKey = q, ""
		w.querying = true
		w.state = model.ViewState{Kind: model.ViewLoading, Selection: q, Context: sentence}
		w.render()
		if action.Action == "retry" {
			w.SetInput(q)
			if w.callbacks.Diagnostics != nil {
				w.callbacks.Diagnostics("main window: retry requested, bypassing cache")
			}
			if w.callbacks.Retry != nil {
				w.callbacks.Retry(selection)
			}
		} else if w.callbacks.Lookup != nil {
			w.callbacks.Lookup(selection)
		}
	case "suggestion":
		for _, candidate := range w.state.Suggestions {
			if candidate == action.Text {
				w.SetInput(candidate)
				w.handleMessage(mustJSON(pageAction{Action: "lookup", Text: candidate}))
				break
			}
		}
	case "history-select":
		for _, entry := range w.entries {
			if entry.Key == action.Key {
				if w.callbacks.CancelLookup != nil {
					w.callbacks.CancelLookup()
				}
				w.selectedKey = entry.Key
				text := entry.Query
				if text == "" {
					text = entry.Definition.Term
				}
				w.querying = false
				w.currentQuery = text
				w.SetInput(text)
				if w.callbacks.SelectHistory != nil {
					w.callbacks.SelectHistory(entry.Key)
				}
				break
			}
		}
	case "history-delete":
		for _, entry := range w.entries {
			if entry.Key == action.Key && w.callbacks.DeleteHistory != nil {
				w.callbacks.DeleteHistory(entry.Key)
				break
			}
		}
	case "history-clear":
		w.clearHistory()
	case "history-export":
		w.exportHistory()
	case "speak":
		if w.state.Kind == model.ViewSuccess && w.callbacks.Speak != nil {
			w.callbacks.Speak(w.state.Definition.Term)
		}
	case "copy-example":
		if w.state.Kind != model.ViewSuccess || w.state.Definition.ExampleEN == "" {
			return
		}
		if err := walk.Clipboard().SetText(w.state.Definition.ExampleEN); err != nil {
			w.notice("复制失败：" + err.Error())
		} else {
			w.notice("例句已复制")
		}
	case "pin":
		w.pinned = !w.pinned
		w.applyPinned()
	case "zoom":
		if action.Delta >= -1 && action.Delta <= 1 {
			w.changeSizes(action.Delta, action.Reset)
		}
	case "content-height":
		if action.Height >= 0 && action.Height <= 100000 {
			w.contentHeight = action.Height
			w.growForContent()
		}
	}
}

func mustJSON(value any) string { data, _ := json.Marshal(value); return string(data) }

func (w *Window) notice(message string) {
	if w.ready && !w.closed {
		w.view.Eval("window.touchdict.notice(" + mustJSON(message) + ")")
	}
}

func (w *Window) clearHistory() {
	if walk.MsgBox(w.MW, "清空历史记录", "确定清空所有查词历史和查询次数吗？此操作无法撤销。", walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) != walk.DlgCmdYes {
		return
	}
	if w.callbacks.ClearHistory == nil {
		return
	}
	if err := w.callbacks.ClearHistory(); err != nil {
		walk.MsgBox(w.MW, "TouchDict", "清空历史记录失败："+err.Error(), walk.MsgBoxIconError)
		return
	}
	w.selectedKey = ""
	w.SetHistory(nil)
	w.notice("历史记录已清空")
}
