//go:build windows

package mainwindow

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"
	"syscall"
	"unsafe"
)

var getGlyphOutline = syscall.NewLazyDLL("gdi32.dll").NewProc("GetGlyphOutlineW")

func labelLineHeight(label *walk.TextLabel) int {
	return max(1, MeasureRenderedLabel(label, "Ag中文", 100000, true).Height)
}

type renderedText struct {
	text string
	font win.HGDIOBJ
}

func snapshotRenderedLabel(label *walk.TextLabel) renderedText {
	child := win.GetWindow(label.Handle(), win.GW_CHILD)
	return renderedText{text: label.Text(), font: win.HGDIOBJ(win.SendMessage(child, win.WM_GETFONT, 0, 0))}
}

// Use the native STATIC's existing HFONT, not a new font for the destination
// monitor DPI: cards deliberately retain their pixel typography across screens.
func MeasureRenderedLabel(label *walk.TextLabel, text string, width int, single bool) walk.Size {
	snapshot := snapshotRenderedLabel(label)
	snapshot.text = text
	return snapshot.measure(width, single)
}

func (text renderedText) measure(width int, single bool) walk.Size {
	if text.text == "" {
		return walk.Size{}
	}
	dc := win.GetDC(0)
	if dc == 0 {
		return walk.Size{Width: width, Height: 24}
	}
	defer win.ReleaseDC(0, dc)
	old := win.SelectObject(dc, text.font)
	defer win.SelectObject(dc, old)
	buffer, _ := syscall.UTF16FromString(text.text)
	rect := win.RECT{Right: int32(max(1, width))}
	flags := uint32(win.DT_CALCRECT | win.DT_NOPREFIX | win.DT_WORDBREAK)
	if single {
		flags = win.DT_CALCRECT | win.DT_NOPREFIX | win.DT_SINGLELINE
	}
	win.DrawTextEx(dc, &buffer[0], int32(len(buffer)-1), &rect, flags, nil)
	return walk.Size{Width: int(rect.Right - rect.Left), Height: int(rect.Bottom - rect.Top)}
}

type glyphMetrics struct {
	Width, Height      uint32
	Origin             win.POINT
	AdvanceX, AdvanceY int16
}
type fixed struct {
	Fraction uint16
	Value    int16
}
type matrix struct{ XX, XY, YX, YY fixed }

// Static labels include unused ascent above the actual first-line glyphs.
func textTopInset(label *walk.TextLabel) int {
	child := win.GetWindow(label.Handle(), win.GW_CHILD)
	if child == 0 {
		return 0
	}
	dc := win.GetDC(child)
	if dc == 0 {
		return 0
	}
	defer win.ReleaseDC(child, dc)
	font := win.HGDIOBJ(win.SendMessage(child, win.WM_GETFONT, 0, 0))
	if font == 0 {
		return 0
	}
	old := win.SelectObject(dc, font)
	defer win.SelectObject(dc, old)
	var tm win.TEXTMETRIC
	if !win.GetTextMetrics(dc, &tm) {
		return 0
	}
	identity := matrix{XX: fixed{Value: 1}, YY: fixed{Value: 1}}
	var highest int32
	for _, r := range label.Text() {
		if r == '\n' || r == '\r' {
			break
		}
		if r > 65535 {
			continue
		}
		var gm glyphMetrics
		result, _, _ := getGlyphOutline.Call(uintptr(dc), uintptr(r), 0, uintptr(unsafe.Pointer(&gm)), 0, 0, uintptr(unsafe.Pointer(&identity)))
		if uint32(result) != ^uint32(0) && gm.Height > 0 && gm.Origin.Y > highest {
			highest = gm.Origin.Y
		}
	}
	if highest == 0 {
		return 0
	}
	return max(0, int(tm.TmAscent-highest))
}
