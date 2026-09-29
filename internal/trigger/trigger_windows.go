//go:build windows

package trigger

import (
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

type Event struct {
	At     time.Time
	Source string
}
type Listener struct {
	events             chan<- Event
	mouseHook, keyHook uintptr
	enabled            atomic.Bool
	last               atomic.Int64
}
type keyboardData struct {
	VkCode, ScanCode, Flags, Time uint32
	ExtraInfo                     uintptr
}

const (
	whMouseLL     = 14
	whKeyboardLL  = 13
	wmMButtonUp   = 0x0208
	wmMButtonDown = 0x0207
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmSysKeyDown  = 0x0104
	wmSysKeyUp    = 0x0105
	vkD           = 0x44
	vkControl     = 0x11
	vkMenu        = 0x12
	vkLMenu       = 0xA4
)

var user32 = syscall.NewLazyDLL("user32.dll")
var setWindowsHookEx = user32.NewProc("SetWindowsHookExW")
var unhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
var callNextHookEx = user32.NewProc("CallNextHookEx")
var getAsyncKeyState = user32.NewProc("GetAsyncKeyState")
var registerHotKey = user32.NewProc("RegisterHotKey")
var getMessage = user32.NewProc("GetMessageW")

func New(events chan<- Event) *Listener {
	l := &Listener{events: events}
	l.enabled.Store(true)
	return l
}
func (l *Listener) Start() error {
	var altStarted time.Time
	altCandidate := false
	keyCB := syscall.NewCallback(func(code int, wparam, lparam uintptr) uintptr {
		if code >= 0 && l.enabled.Load() {
			data := (*keyboardData)(syscallPointer(lparam))
			vk := data.VkCode
			if wparam == wmKeyDown || wparam == wmSysKeyDown {
				if vk == vkLMenu || vk == vkMenu {
					if !altCandidate {
						altStarted = time.Now()
						altCandidate = true
					}
				} else {
					if altCandidate {
						altCandidate = false
					}
					ctrl, _, _ := getAsyncKeyState.Call(vkControl)
					alt, _, _ := getAsyncKeyState.Call(vkMenu)
					if vk == vkD && ctrl&0x8000 != 0 && alt&0x8000 != 0 {
						l.emit("hotkey")
					}
				}
			} else if (wparam == wmKeyUp || wparam == wmSysKeyUp) && (vk == vkLMenu || vk == vkMenu) {
				if altCandidate {
					elapsed := time.Since(altStarted)
					// Precision touchpads can synthesize an Alt tap with down/up in the
					// same clock tick. Do not discard these zero-duration gestures.
					if elapsed <= 900*time.Millisecond {
						l.emit("alt")
					}
				}
				altCandidate = false
			}
		}
		r, _, _ := callNextHookEx.Call(0, uintptr(code), wparam, lparam)
		return r
	})
	l.keyHook, _, _ = setWindowsHookEx.Call(whKeyboardLL, keyCB, 0, 0)
	if l.keyHook == 0 {
		l.Close()
		return syscall.GetLastError()
	}
	go l.hotkeyLoop()
	return nil
}
func (l *Listener) hotkeyLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ok, _, _ := registerHotKey.Call(0, 1, 0x0001|0x0002|0x4000, vkD)
	if ok == 0 {
		return
	}
	var msg struct {
		Hwnd           uintptr
		Message        uint32
		Pad            uint32
		WParam, LParam uintptr
		Time           uint32
		PtX, PtY       int32
		Private        uint32
	}
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		if msg.Message == 0x0312 && msg.WParam == 1 && l.enabled.Load() {
			l.emit("hotkey")
		}
	}
}
func (l *Listener) emit(source string) {
	now := time.Now().UnixMilli()
	prev := l.last.Swap(now)
	if now-prev < 350 {
		return
	}
	select {
	case l.events <- Event{At: time.Now(), Source: source}:
	default:
	}
}
func (l *Listener) SetEnabled(v bool) { l.enabled.Store(v) }
func (l *Listener) Close() {
	if l.mouseHook != 0 {
		unhookWindowsHookEx.Call(l.mouseHook)
		l.mouseHook = 0
	}
	if l.keyHook != 0 {
		unhookWindowsHookEx.Call(l.keyHook)
		l.keyHook = 0
	}
}

// Isolated helper keeps unsafe use local to the hook structure pointer.
func unsafePointer(v uintptr) *uint32 { return (*uint32)(syscallPointer(v)) }
