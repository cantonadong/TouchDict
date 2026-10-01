//go:build windows

package popup

import (
	"github.com/lxn/walk"
	"strings"
	"touchdict/internal/mainwindow"
)

func (w *Window) contentWidth() int {
	width := 0
	for _, label := range []*walk.TextLabel{w.term, w.pos, w.meaning, w.example, w.translation, w.status} {
		if label.Text() == "" {
			continue
		}
		for _, line := range strings.Split(label.Text(), "\n") {
			bounds := mainwindow.MeasureRenderedLabel(label, line, 100000, true)
			lineWidth := bounds.Width + 2
			if label == w.example || (label == w.term && w.retry.Visible()) {
				lineWidth += 84 + 12
			}
			width = max(width, lineWidth)
		}
	}
	return width
}

func (w *Window) contentLineHeight() int {
	bounds := mainwindow.MeasureRenderedLabel(w.pos, "Ag中文", 100000, true)
	return max(1, bounds.Height)
}
