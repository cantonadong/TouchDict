//go:build windows

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"touchdict/internal/gemini"
	"touchdict/internal/localmodel"
	"touchdict/internal/logging"
	"touchdict/internal/mainwindow"
	"touchdict/internal/model"
	"touchdict/internal/popup"
	"touchdict/internal/preview"
	"touchdict/internal/query"
	"touchdict/internal/selection"
	"touchdict/internal/settings"
	"touchdict/internal/speech"
	"touchdict/internal/trigger"
)

var (
	getCursorPos  = syscall.NewLazyDLL("user32.dll").NewProc("GetCursorPos")
	createMutex   = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW")
	instanceMutex syscall.Handle
)

type point struct{ X, Y int32 }

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--read-selection-context" {
		selection.RunContextHelper()
		return
	}
	runtime.LockOSThread()
	first, instanceErr := singleInstance()
	if instanceErr != nil {
		walk.MsgBox(nil, "TouchDict", "无法检查运行实例："+instanceErr.Error(), walk.MsgBoxIconError)
		return
	}
	if !first {
		walk.MsgBox(nil, "TouchDict", "TouchDict 已经在运行。", walk.MsgBoxIconInformation)
		return
	}
	defer syscall.CloseHandle(instanceMutex)
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	cfg, err := settings.Load(exeDir)
	if err != nil {
		walk.MsgBox(nil, "TouchDict", err.Error(), walk.MsgBoxIconError)
		return
	}
	logger, closer, e := logging.Open()
	if e != nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	} else {
		defer closer.Close()
	}
	if err := gemini.ConfigureCache(exeDir); err != nil {
		logger.Printf("cache init failed: %v", err)
	}
	events := make(chan trigger.Event, 2)
	listener := trigger.New(events)
	defer listener.Close()
	reader := selection.New()
	reader.Diagnostics = func(message string) { logger.Print(message) }
	speaker := speech.New()
	defer speaker.Close()
	var win *popup.Window
	var current model.Selection
	var mu sync.Mutex
	var selectionCancel context.CancelFunc
	localRuntime := localmodel.NewRuntime(exeDir)
	defer localRuntime.Close()
	queryService := query.New(func() query.Client {
		if cfg.ModelProvider == "local" {
			return localmodel.New(localRuntime, cfg.LocalModel)
		}
		return gemini.New(cfg.APIKey, cfg.Model)
	}, 20*time.Second)
	if err := settings.SyncStartup(exe, cfg.StartupEnabled); err != nil {
		logger.Printf("startup sync failed")
	}
	var mainWin *mainwindow.Window
	var mainRequest uint64
	var refreshModelMenu func()
	onModelChanged := func() {
		mainRequest++
		queryService.Cancel("main")
		queryService.Cancel("popup")
		localRuntime.Stop()
		state := model.ViewState{Kind: model.ViewEmpty, Message: "模型已切换，请重新查询"}
		if mainWin != nil {
			mainWin.Update(state)
		}
		if win != nil {
			win.Update(state)
		}
	}
	editSettings := func() {
		old := cfg
		if settings.Edit(nil, &cfg, exe) {
			if old.ModelProvider != cfg.ModelProvider || old.Model != cfg.Model || old.LocalModel != cfg.LocalModel || old.APIKey != cfg.APIKey {
				onModelChanged()
			}
			logger.Print("settings updated")
			if refreshModelMenu != nil {
				refreshModelMenu()
			}
		}
	}
	retry := func() {
		if strings.TrimSpace(current.Text) != "" {
			fresh := current
			fresh.BypassCache = true
			lookupPopup(queryService, win, &cfg, speaker, logger, fresh)
		}
	}
	w, err := popup.New(popup.Callbacks{Diagnostics: func(message string) { logger.Print(message) }, Speak: func() {
		if current.Text != "" {
			if err := speaker.Speak(current.Text); err != nil {
				win.SetStatus(err.Error())
			}
		}
	}, Lookup: func(text string) {
		speaker.Stop()
		current.Text = text
		current.Multiword = len(strings.Fields(text)) > 1
		lookupPopup(queryService, win, &cfg, speaker, logger, current)
	}, Retry: retry, Hidden: speaker.Stop, Settings: editSettings})
	if err != nil {
		walk.MsgBox(nil, "TouchDict", err.Error(), walk.MsgBoxIconError)
		return
	}
	win = w
	mw, err := mainwindow.New(mainwindow.Callbacks{
		Diagnostics: func(message string) { logger.Print(message) },
		CancelLookup: func() {
			mainRequest++
			queryService.Cancel("main")
		},
		Retry: func(sel model.Selection) {
			logger.Printf("lookup submit scope=main retry=true context_chars=%d", len([]rune(sel.Context)))
			mainRequest++
			request := mainRequest
			queryService.Retry("main", sel, func(state model.ViewState) {
				win.MW.Synchronize(func() {
					if request == mainRequest {
						mainWin.Update(state)
					}
				})
			})
		},
		Lookup: func(sel model.Selection) {
			logger.Printf("lookup submit scope=main retry=false context_chars=%d", len([]rune(sel.Context)))
			mainRequest++
			request := mainRequest
			queryService.Lookup("main", sel, func(state model.ViewState) {
				win.MW.Synchronize(func() {
					if request == mainRequest {
						mainWin.Update(state)
					}
				})
			})
		},
		SelectHistory: func(key string) {
			if d, ok := queryService.SelectHistory(key); ok {
				sentence := ""
				for _, entry := range queryService.History() {
					if entry.Key == key {
						sentence = entry.Context
						break
					}
				}
				mainWin.Update(model.ViewState{Kind: model.ViewSuccess, Definition: d, Context: sentence})
				if autoSpeakAllowed(cfg.AutoSpeak, d.Term) && speaker.Available() {
					if err := speaker.Speak(d.Term); err != nil {
						logger.Printf("history speech failed: %v", err)
					}
				}
			}
		},
		DeleteHistory: func(key string) {
			if err := queryService.DeleteHistory(key); err != nil {
				logger.Printf("delete history failed: %v", err)
				walk.MsgBox(mainWin.MW, "TouchDict", "删除历史记录失败："+err.Error(), walk.MsgBoxIconError)
			}
		},
		ClearHistory: queryService.ClearHistory,
		Speak: func(text string) {
			if err := speaker.SpeakAgain(text); err != nil {
				mainWin.Update(model.ViewState{Kind: model.ViewError, Selection: text, Message: err.Error()})
			}
		},
		InitialTermSize: 30,
		InitialHeight:   cfg.MainWindowHeight,
		HeightChanged: func(height int) {
			cfg.MainWindowHeight = height
			if err := cfg.Save(); err != nil {
				logger.Printf("save main window height failed: %v", err)
			}
		},
		InitialContentSize: 12,
		TermSizeChanged:    func(size int) { win.SetTermSize(size) },
		ContentSizeChanged: func(size int) { win.SetContentSize(size) },
	})
	if err != nil {
		walk.MsgBox(nil, "TouchDict", err.Error(), walk.MsgBoxIconError)
		return
	}
	mainWin = mw
	defer mainWin.Close()
	mainWin.SetHistory(queryService.History())
	unsubscribe := queryService.SubscribeHistory(func(entries []model.HistoryEntry) { win.MW.Synchronize(func() { mainWin.SetHistory(entries) }) })
	defer unsubscribe()
	appIcon, _ := walk.NewIconFromResourceId(2)
	if appIcon != nil {
		_ = win.MW.SetIcon(appIcon)
		defer appIcon.Dispose()
	}
	previewName := ""
	mainPreview := false
	for _, a := range os.Args[1:] {
		if a == "--preview" {
			previewName = "normal"
		} else if a == "--main-preview" {
			mainPreview = true
		} else if strings.HasPrefix(a, "--preview=") {
			previewName = strings.TrimPrefix(a, "--preview=")
		}
	}
	if mainPreview {
		entries := []model.HistoryEntry{
			{Key: "systems thinkers", Query: "systems thinkers", Definition: model.Definition{Term: "systems thinkers", PartOfSpeech: "n.", MeaningZH: "系统思考者", ExampleEN: "Systems thinkers can identify the root causes of complex problems.", ExampleZH: "系统思考者能够识别复杂问题的根本原因。"}},
			{Key: "solve", Query: "solve", Definition: model.Definition{Term: "solve", PartOfSpeech: "vt.", MeaningZH: "解决", ExampleEN: "She managed to solve the math problem.", ExampleZH: "她设法解开了这道数学题。"}},
			{Key: "scarce", Query: "scarce", Definition: model.Definition{Term: "scarce", PartOfSpeech: "adj.", MeaningZH: "缺乏的，稀有的", ExampleEN: "Fresh water was scarce during the drought.", ExampleZH: "旱灾期间淡水非常缺乏。"}},
		}
		mainWin.SetHistory(entries)
		mainWin.SetInput("scarce")
		mainWin.Update(model.ViewState{Kind: model.ViewSuccess, Definition: entries[2].Definition})
		mainWin.Show()
		mainWin.MW.Run()
		return
	}
	if previewName != "" {
		win.ShowAt(walk.Point{X: 200, Y: 160}, preview.State(previewName))
		win.MW.Run()
		return
	}
	notify, err := walk.NewNotifyIcon(win.MW)
	if err != nil {
		walk.MsgBox(win.MW, "TouchDict", err.Error(), walk.MsgBoxIconError)
		return
	}
	defer notify.Dispose()
	notify.SetToolTip("TouchDict · 三指划词")
	if appIcon != nil {
		_ = notify.SetIcon(appIcon)
	}
	openMain := func() { mainWin.Show() }
	addAction(notify, "打开主窗口", openMain)
	var lastTrayClick time.Time
	notify.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			now := time.Now()
			if now.Sub(lastTrayClick) < 500*time.Millisecond {
				openMain()
				lastTrayClick = time.Time{}
			} else {
				lastTrayClick = now
			}
		}
	})
	addAction(notify, "查词（Ctrl+Alt+D）", func() {
		select {
		case events <- trigger.Event{At: time.Now(), Source: "tray"}:
		default:
		}
	})
	paused := false
	var pauseAction *walk.Action
	pauseAction = addAction(notify, "监听状态：开", func() {
		paused = !paused
		listener.SetEnabled(!paused)
		if paused {
			pauseAction.SetText("监听状态：关")
		} else {
			pauseAction.SetText("监听状态：开")
		}
	})
	addAction(notify, "界面预览", func() {
		var p point
		getCursorPos.Call(uintptr(unsafe.Pointer(&p)))
		win.ShowAt(walk.Point{X: int(p.X), Y: int(p.Y)}, preview.State("normal"))
	})
	refreshModelMenu, err = addModelMenu(notify, &cfg, win.MW, onModelChanged)
	if err != nil {
		walk.MsgBox(win.MW, "TouchDict", "无法创建模型菜单："+err.Error(), walk.MsgBoxIconError)
		return
	}
	addAction(notify, "设置", editSettings)
	addAction(notify, "退出", func() {
		queryService.Cancel("popup")
		queryService.Cancel("main")
		listener.Close()
		localRuntime.Stop()
		notify.Dispose()
		win.MW.Dispose()
		walk.App().Exit(0)
	})
	_ = notify.SetVisible(true)
	if err := listener.Start(); err != nil {
		logger.Printf("trigger hook unavailable: %v", err)
		walk.MsgBox(win.MW, "TouchDict", "全局触发监听启动失败，请重启程序。", walk.MsgBoxIconWarning)
	}
	go func() {
		for event := range events {
			logger.Printf("trigger received source=%s", event.Source)
			if (event.Source == "hotkey" && !cfg.HotkeyEnabled) || (event.Source == "alt" && !cfg.AltEnabled) {
				continue
			}
			mu.Lock()
			if selectionCancel != nil {
				selectionCancel()
			}
			ctx, c := context.WithCancel(context.Background())
			selectionCancel = c
			mu.Unlock()
			var capturedPoint point
			getCursorPos.Call(uintptr(unsafe.Pointer(&capturedPoint)))
			sel, e := reader.Read(ctx, event.Source == "alt", walk.Point{X: int(capturedPoint.X), Y: int(capturedPoint.Y)})
			sel.Source = event.Source
			wasCanceled := ctx.Err() != nil
			c()
			if e != nil {
				logger.Printf("capture failed source=%s error=%T", event.Source, e)
				if !wasCanceled {
					win.MW.Synchronize(func() {
						var p point
						getCursorPos.Call(uintptr(unsafe.Pointer(&p)))
						win.ShowAt(walk.Point{X: int(p.X), Y: int(p.Y)}, model.ViewState{Kind: model.ViewError, Message: e.Error()})
					})
				}
				continue
			}
			logger.Printf("capture succeeded source=%s context_chars=%d bounds=%t", event.Source, len([]rune(sel.Context)), sel.Bounds != nil)
			if autoSpeakAllowed(cfg.AutoSpeak, sel.Text) {
				_ = speaker.Speak(sel.Text)
			}
			win.MW.Synchronize(func() {
				current = sel
				mainRequest++
				request := mainRequest
				queryService.Cancel("main")
				mainWin.SetInput(sel.Text)
				mainWin.Update(model.ViewState{Kind: model.ViewLoading, Selection: sel.Text, Context: sel.Context})
				win.ShowSelection(walk.Point{X: int(capturedPoint.X), Y: int(capturedPoint.Y)}, sel.Bounds, model.ViewState{Kind: model.ViewLoading, Selection: sel.Text, Context: sel.Context})
				lookupPopup(queryService, win, &cfg, speaker, logger, sel, func(state model.ViewState) {
					if request == mainRequest {
						mainWin.Update(state)
					}
				})
			})
		}
	}()
	logger.Print("application started")
	win.MW.Run()
}

func lookupPopup(service *query.Service, win *popup.Window, cfg *settings.Config, speaker *speech.Service, logger *log.Logger, sel model.Selection, observers ...func(model.ViewState)) {
	logger.Printf("lookup submit scope=popup retry=%t context_chars=%d", sel.BypassCache, len([]rune(sel.Context)))
	service.Lookup("popup", sel, func(state model.ViewState) {
		win.MW.Synchronize(func() {
			if state.Kind == model.ViewSuccess && sel.Source != "" && sel.Context == "" {
				state.Message = "未能读取原句，当前结果未使用上下文"
			}
			for _, observer := range observers {
				observer(state)
			}
			if state.Kind == model.ViewError {
				logger.Printf("lookup failed: %s", state.Message)
			}
			if state.Kind == model.ViewSuggestions {
				state = model.ViewState{Kind: model.ViewError, Selection: sel.Text, Message: "可能存在拼写错误，请打开主窗口查看候选词"}
			}
			win.Update(state)
			if state.Kind == model.ViewSuccess && autoSpeakAllowed(cfg.AutoSpeak, sel.Text) {
				if speaker.Available() {
					if err := speaker.SpeakAgain(state.Definition.Term); err != nil {
						win.SetStatus(err.Error())
					}
				} else {
					win.SetStatus("Windows 美式英语语音不可用")
				}
			}
		})
	})
}
func addAction(n *walk.NotifyIcon, text string, fn func()) *walk.Action {
	a := walk.NewAction()
	a.SetText(text)
	a.Triggered().Attach(fn)
	_ = n.ContextMenu().Actions().Add(a)
	return a
}
func singleInstance() (bool, error) {
	name, _ := syscall.UTF16PtrFromString("Local\\TouchDict.SingleInstance")
	h, _, callErr := createMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return false, callErr
	}
	if callErr == syscall.Errno(183) {
		syscall.CloseHandle(syscall.Handle(h))
		return false, nil
	}
	instanceMutex = syscall.Handle(h)
	return true, nil
}
func autoSpeakAllowed(enabled bool, text string) bool {
	return enabled && len(strings.Fields(text)) <= 3
}
func init() { _ = fmt.Sprintf("") }
