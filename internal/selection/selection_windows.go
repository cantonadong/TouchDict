//go:build windows

package selection

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"touchdict/internal/model"
)

type Reader struct{ Diagnostics func(string) }

func New() *Reader { return &Reader{} }

var selUser32 = syscall.NewLazyDLL("user32.dll")
var keybdEvent = selUser32.NewProc("keybd_event")
var getClipboardSequenceNumber = selUser32.NewProc("GetClipboardSequenceNumber")
var mouseEvent = selUser32.NewProc("mouse_event")
var getAsyncKeyState = selUser32.NewProc("GetAsyncKeyState")
var sendInput = selUser32.NewProc("SendInput")

const (
	vkCtrl   = 0x11
	vkC      = 0x43
	keyUp    = 0x0002
	leftDown = 0x0002
	leftUp   = 0x0004
	vkAlt    = 0x12
)

type mouseInput struct {
	Dx, Dy                 int32
	MouseData, Flags, Time uint32
	ExtraInfo              uintptr
}
type input struct {
	Type, Padding uint32
	Mouse         mouseInput
}

func (r *Reader) Read(ctx context.Context, hoverMode bool, points ...walk.Point) (model.Selection, error) {
	var pointer win.POINT
	win.GetCursorPos(&pointer)
	if len(points) > 0 {
		pointer.X, pointer.Y = int32(points[0].X), int32(points[0].Y)
	}
	clip := walk.Clipboard()
	old, oldErr := clip.Text()
	if oldErr == nil {
		defer func() { _ = clip.SetText(old) }()
	}
	waitModifiersReleased(ctx)
	var text string
	if hoverMode {
		// Preserve a selection made by dragging the mouse. A precision
		// touchpad's three-finger Alt gesture must not replace it with the
		// single word under the pointer.
		text = copySelection(ctx, clip, 160*time.Millisecond)
		if text == "" {
			doubleClick()
			time.Sleep(90 * time.Millisecond)
			text = copySelection(ctx, clip, 900*time.Millisecond)
			if text == "" {
				return model.Selection{}, errors.New("未能从 Chrome 读取选中的英文内容")
			}
		}
	} else {
		text = copySelection(ctx, clip, 420*time.Millisecond)
	}
	if text == "" {
		// A double click selects the word under the pointer in browsers and
		// most native text controls. Existing manual selections are tried first.
		doubleClick()
		time.Sleep(90 * time.Millisecond)
		text = copySelection(ctx, clip, 800*time.Millisecond)
	}
	text = normalize(text)
	if text == "" {
		return model.Selection{}, errors.New("鼠标下没有可读取的英文单词")
	}
	if !hasLatin(text) {
		return model.Selection{}, errors.New("请选择英文单词或短语")
	}
	if len([]rune(text)) > 300 {
		text = string([]rune(text)[:300])
	}
	bounds, sentence, diagnostic := readSelectionDetails(ctx, text, pointer)
	if r.Diagnostics != nil {
		r.Diagnostics("context capture: " + diagnostic)
	}
	return model.Selection{Text: text, Context: sentence, Multiword: len(strings.Fields(text)) > 1, Bounds: bounds}, nil
}

func doubleClick() {
	if !sendClick() {
		// Legacy fallback for restricted desktops where SendInput is filtered.
		mouseEvent.Call(leftDown, 0, 0, 0, 0)
		mouseEvent.Call(leftUp, 0, 0, 0, 0)
		time.Sleep(70 * time.Millisecond)
		mouseEvent.Call(leftDown, 0, 0, 0, 0)
		mouseEvent.Call(leftUp, 0, 0, 0, 0)
		return
	}
	time.Sleep(70 * time.Millisecond)
	_ = sendClick()
}

func sendClick() bool {
	inputs := []input{{Mouse: mouseInput{Flags: leftDown}}, {Mouse: mouseInput{Flags: leftUp}}}
	r, _, _ := sendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	return r == uintptr(len(inputs))
}

func copySelection(ctx context.Context, clip *walk.ClipboardService, timeout time.Duration) string {
	before, _, _ := getClipboardSequenceNumber.Call()
	keybdEvent.Call(vkCtrl, 0, 0, 0)
	keybdEvent.Call(vkC, 0, 0, 0)
	keybdEvent.Call(vkC, 0, keyUp, 0)
	keybdEvent.Call(vkCtrl, 0, keyUp, 0)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(20 * time.Millisecond):
		}
		v, e := clip.Text()
		sequence, _, _ := getClipboardSequenceNumber.Call()
		if sequence != before && e == nil && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func waitModifiersReleased(ctx context.Context) {
	deadline := time.Now().Add(600 * time.Millisecond)
	for time.Now().Before(deadline) {
		ctrl, _, _ := getAsyncKeyState.Call(vkCtrl)
		alt, _, _ := getAsyncKeyState.Call(vkAlt)
		if ctrl&0x8000 == 0 && alt&0x8000 == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
}
func normalize(s string) string { return strings.Join(strings.Fields(strings.TrimSpace(s)), " ") }
func hasLatin(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Latin) && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
