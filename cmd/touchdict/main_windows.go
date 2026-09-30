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
	getCursorPos = syscall.NewLazyDLL("user32.dll").NewProc("GetCursorPos")
	createMutex  = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW")
)

type point struct{ X, Y int32 }

func main() {
	runtime.LockOSThread()
	if !singleInstance() {
		walk.MsgBox(nil, "TouchDict", "TouchDict 已经在运行。", walk.MsgBoxIconInformation)
		return
	}
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
	speaker := speech.New()
	defer speaker.Close()
	var win *popup.Window
	var current model.Selection
	var mu sync.Mutex
	var selectionCancel context.CancelFunc
	queryService := query.New(func() *gemini.Client { return gemini.New(cfg.APIKey, cfg.Model) }, 20*time.Second)
	if err := settings.SyncStartup(exe, cfg.StartupEnabled); err != nil {
		logger.Printf("startup sync failed")
	}
	var mainWin *mainwindow.Window
	retry := func() {
		if strings.TrimSpace(current.Text) != "" {
			lookupPopup(queryService, win, &cfg, speaker, logger, current)
		}
	}
	w, err := popup.New(popup.Callbacks{Speak: func() {
		if current.Text != "" {
			if err := speaker.Speak(current.Text); err != nil {
				win.SetStatus(err.Error())
			}
		}
	}, Retry: retry, Hidden: speaker.Stop, Settings: func() {
		if settings.Edit(nil, &cfg, exe) {
			logger.Print("settings updated")
		}
	}})
	if err != nil {
		walk.MsgBox(nil, "TouchDict", err.Error(), walk.MsgBoxIconError)
		return
	}
	win = w
	mw, err := mainwindow.New(mainwindow.Callbacks{
		Lookup: func(text string) {
			sel := model.Selection{Text: text}
			queryService.Lookup("main", sel, func(state model.ViewState) { win.MW.Synchronize(func() { mainWin.Update(state) }) })
		},
		SelectHistory: func(key string) {
			if d, ok := queryService.SelectHistory(key); ok {
				mainWin.Update(model.ViewState{Kind: model.ViewSuccess, Definition: d})
			}
		},
		Speak: func(text string) {
			if err := speaker.SpeakAgain(text); err != nil {
				mainWin.Update(model.ViewState{Kind: model.ViewError, Selection: text, Message: err.Error()})
			}
		},
		InitialScale: cfg.ResultFontScale,
		ZoomChanged: func(scale int) {
			cfg.ResultFontScale = scale
			if err := cfg.Save(); err != nil {
				logger.Printf("font scale save failed")
			}
		},
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
	for _, a := range os.Args[1:] {
		if a == "--preview" {
			previewName = "normal"
		} else if strings.HasPrefix(a, "--preview=") {
			previewName = strings.TrimPrefix(a, "--preview=")
		}
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
	pauseAction = addAction(notify, "暂停监听", func() {
		paused = !paused
		listener.SetEnabled(!paused)
		if paused {
			pauseAction.SetText("继续监听")
		} else {
			pauseAction.SetText("暂停监听")
		}
	})
	addAction(notify, "界面预览", func() {
		var p point
		getCursorPos.Call(uintptr(unsafe.Pointer(&p)))
		win.ShowAt(walk.Point{X: int(p.X), Y: int(p.Y)}, preview.State("normal"))
	})
	addAction(notify, "设置", func() { _ = settings.Edit(nil, &cfg, exe) })
	addAction(notify, "退出", func() {
		queryService.Cancel("popup")
		queryService.Cancel("main")
		listener.Close()
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
			sel, e := reader.Read(ctx, event.Source == "alt")
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
			logger.Printf("capture succeeded source=%s", event.Source)
			if autoSpeakAllowed(cfg.AutoSpeak, sel.Text) {
				_ = speaker.Speak(sel.Text)
			}
			win.MW.Synchronize(func() {
				current = sel
				var p point
				getCursorPos.Call(uintptr(unsafe.Pointer(&p)))
				win.ShowAt(walk.Point{X: int(p.X), Y: int(p.Y)}, model.ViewState{Kind: model.ViewLoading, Selection: sel.Text})
			})
			lookupPopup(queryService, win, &cfg, speaker, logger, sel)
		}
	}()
	logger.Print("application started")
	win.MW.Run()
}

func lookupPopup(service *query.Service, win *popup.Window, cfg *settings.Config, speaker *speech.Service, logger *log.Logger, sel model.Selection) {
	service.Lookup("popup", sel, func(state model.ViewState) {
		win.MW.Synchronize(func() {
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
func singleInstance() bool {
	name, _ := syscall.UTF16PtrFromString("Local\\TouchDict.SingleInstance")
	h, _, _ := createMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	return h != 0 && syscall.GetLastError() != syscall.Errno(183)
}
func autoSpeakAllowed(enabled bool, text string) bool {
	return enabled && len(strings.Fields(text)) <= 3
}
func init() { _ = fmt.Sprintf("") }
