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
	"touchdict/internal/model"
	"touchdict/internal/popup"
	"touchdict/internal/preview"
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
	cfg, err := settings.Load(filepath.Dir(exe))
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
	events := make(chan trigger.Event, 2)
	listener := trigger.New(events)
	defer listener.Close()
	reader := selection.New()
	speaker := speech.New()
	defer speaker.Close()
	var win *popup.Window
	var current model.Selection
	var cancel context.CancelFunc
	var mu sync.Mutex
	retry := func() {
		if strings.TrimSpace(current.Text) != "" {
			startLookup(win, &cfg, speaker, logger, current, &mu, &cancel)
		}
	}
	w, err := popup.New(popup.Callbacks{Speak: func() {
		if current.Text != "" {
			if err := speaker.Speak(current.Text); err != nil {
				win.SetStatus(err.Error())
			}
		}
	}, Retry: retry, Settings: func() {
		if settings.Edit(win.MW, &cfg) {
			logger.Print("settings updated")
		}
	}})
	if err != nil {
		walk.MsgBox(nil, "TouchDict", err.Error(), walk.MsgBoxIconError)
		return
	}
	win = w
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
	addAction(notify, "查词（Ctrl+Alt+D）", func() {
		select {
		case events <- trigger.Event{At: time.Now(), Source: "tray"}:
		default:
		}
	})
	paused := false
	pauseAction := addAction(notify, "暂停监听", func() {
		paused = !paused
		listener.SetEnabled(!paused)
		if paused {
			pauseActionText(notify, "继续监听")
		} else {
			pauseActionText(notify, "暂停监听")
		}
	})
	_ = pauseAction
	addAction(notify, "界面预览", func() {
		var p point
		getCursorPos.Call(uintptr(unsafe.Pointer(&p)))
		win.ShowAt(walk.Point{X: int(p.X), Y: int(p.Y)}, preview.State("normal"))
	})
	addAction(notify, "设置", func() { _ = settings.Edit(win.MW, &cfg) })
	addAction(notify, "退出", func() {
		if cancel != nil {
			cancel()
		}
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
			if cancel != nil {
				cancel()
			}
			ctx, c := context.WithCancel(context.Background())
			cancel = c
			mu.Unlock()
			sel, e := reader.Read(ctx, event.Source == "alt")
			if e != nil {
				logger.Printf("capture failed source=%s error=%T", event.Source, e)
				if ctx.Err() == nil {
					win.MW.Synchronize(func() {
						var p point
						getCursorPos.Call(uintptr(unsafe.Pointer(&p)))
						win.ShowAt(walk.Point{X: int(p.X), Y: int(p.Y)}, model.ViewState{Kind: model.ViewError, Message: e.Error()})
					})
				}
				continue
			}
			logger.Printf("capture succeeded source=%s", event.Source)
			if d, ok := gemini.Cached(sel); ok {
				logger.Printf("lookup cache hit")
				win.MW.Synchronize(func() {
					current = sel
					var p point
					getCursorPos.Call(uintptr(unsafe.Pointer(&p)))
					win.ShowAt(walk.Point{X: int(p.X), Y: int(p.Y)}, model.ViewState{Kind: model.ViewSuccess, Definition: d, Message: "缓存结果"})
					if cfg.AutoSpeak {
						_ = speaker.Speak(d.Term)
					}
				})
				continue
			}
			win.MW.Synchronize(func() {
				current = sel
				var p point
				getCursorPos.Call(uintptr(unsafe.Pointer(&p)))
				win.ShowAt(walk.Point{X: int(p.X), Y: int(p.Y)}, model.ViewState{Kind: model.ViewLoading, Selection: sel.Text})
			})
			startLookup(win, &cfg, speaker, logger, sel, &mu, &cancel)
		}
	}()
	logger.Print("application started")
	win.MW.Run()
}

func startLookup(win *popup.Window, cfg *settings.Config, speaker *speech.Service, logger *log.Logger, sel model.Selection, mu *sync.Mutex, cancel *context.CancelFunc) {
	mu.Lock()
	if *cancel != nil {
		(*cancel)()
	}
	ctx, c := context.WithTimeout(context.Background(), 20*time.Second)
	*cancel = c
	mu.Unlock()
	client := gemini.New(cfg.APIKey, cfg.Model)
	go func() {
		d, err := client.Lookup(ctx, sel)
		if ctx.Err() != nil {
			return
		}
		win.MW.Synchronize(func() {
			if err != nil {
				logger.Printf("lookup failed: %v", err)
				win.Update(model.ViewState{Kind: model.ViewError, Selection: sel.Text, Message: err.Error(), CanRetry: true})
				return
			}
			win.Update(model.ViewState{Kind: model.ViewSuccess, Definition: d})
			if cfg.AutoSpeak && speaker.Available() {
				if err := speaker.Speak(d.Term); err != nil {
					win.SetStatus(err.Error())
				}
			} else if cfg.AutoSpeak {
				win.SetStatus("Windows 美式英语语音不可用")
			}
		})
	}()
}
func addAction(n *walk.NotifyIcon, text string, fn func()) *walk.Action {
	a := walk.NewAction()
	a.SetText(text)
	a.Triggered().Attach(fn)
	_ = n.ContextMenu().Actions().Add(a)
	return a
}
func pauseActionText(n *walk.NotifyIcon, text string) {
	actions := n.ContextMenu().Actions()
	if actions.Len() > 1 {
		actions.At(1).SetText(text)
	}
}
func singleInstance() bool {
	name, _ := syscall.UTF16PtrFromString("Local\\TouchDict.SingleInstance")
	h, _, _ := createMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	return h != 0 && syscall.GetLastError() != syscall.Errno(183)
}
func init() { _ = fmt.Sprintf("") }
