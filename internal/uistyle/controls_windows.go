//go:build windows

package uistyle

import "github.com/lxn/walk"

// All buttons use the user's requested physical pixel size, independent of DPI.
func FitButton(button *walk.PushButton) {
	if font, err := walk.NewFont("Microsoft YaHei UI", 12, 0); err == nil {
		button.SetFont(font)
	}
	LockButtonSize(button)
}

// Call after the owning window has finished processing WM_DPICHANGED,
// never inside child SizeChanged (which runs during layout application).
func LockButtonSize(button *walk.PushButton) {
	size := walk.Size{Width: 84, Height: 42}
	if button.Text() == "取消固顶" {
		size.Width = 126
	}
	if button.MinSizePixels() != size || button.MaxSizePixels() != size {
		_ = button.SetMinMaxSizePixels(size, size)
	}
}
