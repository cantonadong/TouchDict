//go:build windows

package selection

import (
	"context"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// Older WPS editions expose no document TextPattern or automation application.
// Copy the paragraph using selection/navigation keys only. No typing, cut,
// paste, document-wide selection, or document edits are performed.
func readWPSContext(ctx context.Context, clip *walk.ClipboardService, original string) (sentence, diagnostic string) {
	hwnd := win.GetForegroundWindow()
	var processID uint32
	win.GetWindowThreadProcessId(hwnd, &processID)
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return "", ""
	}
	defer windows.CloseHandle(process)
	buffer := make([]uint16, 1024)
	size := uint32(len(buffer))
	if windows.QueryFullProcessImageName(process, 0, &buffer[0], &size) != nil || !strings.EqualFold(filepath.Base(windows.UTF16ToString(buffer[:size])), "wps.exe") {
		return "", ""
	}
	query := normalize(original)
	originalSteps := navigationLength(original)
	if query == "" || originalSteps < 0 || originalSteps > 300 || strings.ContainsAny(original, "\r\n\x07") {
		return "", "WPS paragraph fallback: unsupported selection"
	}
	// Verify the clipboard selection again: UIA ran in another process and
	// the user may have changed the active document or selection meanwhile.
	if normalize(copySelection(ctx, clip, 300*time.Millisecond)) != query || win.GetForegroundWindow() != hwnd {
		return "", "WPS paragraph fallback: selection changed"
	}
	if ctx.Err() != nil {
		return "", "WPS paragraph fallback: canceled"
	}
	// Once navigation begins, finish capture/restoration even if a newer
	// query cancels the original request. Each clipboard wait stays bounded.
	captureCtx, captureCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer captureCancel()
	focused := func() bool { return win.GetForegroundWindow() == hwnd }
	key := func(code byte, modifiers ...byte) bool {
		if !focused() {
			return false
		}
		for _, modifier := range modifiers {
			keybdEvent.Call(uintptr(modifier), 0, 0, 0)
		}
		keybdEvent.Call(uintptr(code), 0, 0, 0)
		keybdEvent.Call(uintptr(code), 0, keyUp, 0)
		for index := len(modifiers) - 1; index >= 0; index-- {
			keybdEvent.Call(uintptr(modifiers[index]), 0, keyUp, 0)
		}
		return true
	}
	const left, right, up, down, shift = 0x25, 0x27, 0x26, 0x28, 0x10
	// Left collapses the original selection to its start. Ctrl+Shift+Up
	// selects its prefix back to the paragraph start, keeping an exact
	// offset for restoring the original selection after the paragraph copy.
	if !key(left) || !key(up, vkCtrl, shift) {
		return "", "WPS paragraph fallback: focus changed"
	}
	prefix := copySelection(captureCtx, clip, 300*time.Millisecond)
	if !focused() {
		return "", "WPS paragraph fallback: focus changed"
	}
	prefixSteps := navigationLength(prefix)
	// Bound keyboard work for very long paragraphs. Right collapses the
	// prefix back to the original start; restore the selected word there.
	if prefixSteps < 0 || prefixSteps > 1200 {
		key(right)
		for count := 0; count < originalSteps; count++ {
			if !key(right, shift) {
				break
			}
		}
		return "", "WPS paragraph fallback: unsupported paragraph navigation"
	}
	// An empty prefix normally means the selection started at document start.
	// Ctrl+Shift+Down captures the paragraph from that position.
	if prefix != "" {
		if !key(left) {
			return "", "WPS paragraph fallback: focus changed"
		}
	}
	if !key(down, vkCtrl, shift) {
		return "", "WPS paragraph fallback: focus changed"
	}
	paragraph := copySelection(captureCtx, clip, 450*time.Millisecond)
	if paragraph != "" && len(prefix)+len(original) > len(paragraph) {
		// At a paragraph boundary Ctrl+Shift+Up can select the preceding
		// paragraph. Extend once more to include the actual selected word.
		if !key(down, vkCtrl, shift) {
			return "", "WPS paragraph fallback: focus changed"
		}
		paragraph = copySelection(captureCtx, clip, 450*time.Millisecond)
	}
	// Restore regardless of cancellation once navigation has begun. If the
	// user deliberately changes focus, never send keys to the new window.
	if !focused() {
		return "", "WPS paragraph fallback: focus changed"
	}
	if paragraph == "" {
		// The expanded range may still be selected even if copying failed.
		key(left)
		for count := 0; count < prefixSteps; count++ {
			if !key(right) {
				break
			}
		}
		for count := 0; count < originalSteps; count++ {
			if !key(right, shift) {
				break
			}
		}
		return "", "WPS paragraph fallback: paragraph copy failed"
	}
	key(left)
	for count := 0; count < prefixSteps; count++ {
		if !key(right) {
			return "", "WPS paragraph fallback: focus changed"
		}
	}
	for count := 0; count < originalSteps; count++ {
		if !key(right, shift) {
			return "", "WPS paragraph fallback: focus changed"
		}
	}
	restoreCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if normalize(copySelection(restoreCtx, clip, 400*time.Millisecond)) != query || !focused() {
		return "", "WPS paragraph fallback: selection restoration did not match"
	}
	if prefixSteps == 0 {
		// If prefix copying failed away from document start, use no sentence:
		// the expected text must still occur at the captured range's start.
		if !strings.HasPrefix(normalize(paragraph), query) {
			return "", "WPS paragraph fallback: missing prefix"
		}
	}
	offset := len(prefix)
	if offset+len(original) > len(paragraph) || normalize(paragraph[offset:offset+len(original)]) != query {
		return "", "WPS paragraph fallback: paragraph did not match selection"
	}
	sentence = normalize(sentenceAt(paragraph, offset, offset+len(original)))
	if sentence == query {
		return "", "WPS paragraph fallback: no surrounding text"
	}
	return sentence, "WPS paragraph fallback: restored selection, captured sentence"
}

func navigationLength(text string) int {
	// WPS treats a CRLF paragraph boundary as one caret movement.
	text = strings.ReplaceAll(text, "\r\n", "\r")
	for _, character := range text {
		// Grapheme clusters, surrogate pairs and table/control markers do
		// not have a reliable one-character-per-arrow movement in old WPS.
		if character > 0xffff || unicode.IsMark(character) || unicode.Is(unicode.Cf, character) ||
			unicode.IsControl(character) && character != '\r' && character != '\n' && character != '\t' {
			return -1
		}
	}
	return utf8.RuneCountInString(text)
}
