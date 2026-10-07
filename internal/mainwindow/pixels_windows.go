//go:build windows

package mainwindow

import (
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

var logicalToPhysicalPoint = syscall.NewLazyDLL("user32.dll").NewProc("LogicalToPhysicalPointForPerMonitorDPI")

// Windows compatibility scaling can virtualize even Walk's SizePixels.
// Measure both coordinate spaces so preferences remain screen pixels.
func screenHeight(window *walk.MainWindow) int {
	height := window.SizePixels().Height
	var client win.RECT
	if !win.GetClientRect(window.Handle(), &client) || client.Bottom < 4 {
		return height
	}
	// Use points inside the client area, including for hidden windows.
	top := win.POINT{X: 1, Y: 1}
	bottom := win.POINT{X: 1, Y: client.Bottom - 1}
	if !win.ClientToScreen(window.Handle(), &top) || !win.ClientToScreen(window.Handle(), &bottom) {
		return height
	}
	logicalSpan := int(bottom.Y - top.Y)
	if logicalToPhysicalPoint.Find() != nil {
		return height
	}
	okTop, _, _ := logicalToPhysicalPoint.Call(uintptr(window.Handle()), uintptr(unsafe.Pointer(&top)))
	okBottom, _, _ := logicalToPhysicalPoint.Call(uintptr(window.Handle()), uintptr(unsafe.Pointer(&bottom)))
	if okTop == 0 || okBottom == 0 || bottom.Y <= top.Y {
		return height
	}
	return (height*int(bottom.Y-top.Y) + logicalSpan/2) / logicalSpan
}

func windowHeightForScreenPixels(window *walk.MainWindow, height int) int {
	native := window.SizePixels().Height
	physical := screenHeight(window)
	if native <= 0 || physical <= 0 {
		return height
	}
	return (height*native + physical/2) / physical
}
