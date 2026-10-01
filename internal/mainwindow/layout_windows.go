//go:build windows

package mainwindow

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// ResultLayout shares the result geometry between the main window and card.
func ResultLayout(mode string, label *walk.TextLabel) walk.Layout {
	l := &widthPreservingLayout{BoxLayout: walk.NewVBoxLayout(), mode: mode}
	if mode == "column" {
		l.contentFont = label
	}
	if mode == "term" || mode == "example" {
		l.mode = "term"
		l.term = label
		l.trimTop = mode == "term"
		l.firstLine = mode == "example"
	}
	l.BoxLayout.SetMargins(walk.Margins{})
	return l
}

func ResultHeight(container walk.Container, width int) int {
	return walk.CreateLayoutItemsForContainer(container).(*widthPreservingLayoutItem).requiredSize(walk.Size{Width: width}).Height
}

func CardLayout(content *walk.Composite, fontLabel *walk.TextLabel) walk.Layout {
	l := &widthPreservingLayout{BoxLayout: walk.NewVBoxLayout(), mode: "card", cardContent: content, contentFont: fontLabel}
	l.BoxLayout.SetMargins(walk.Margins{})
	return l
}

// Content may request more height, but only the user chooses the window width.
type widthPreservingLayout struct {
	*walk.BoxLayout
	container   walk.Container
	mode        string
	term        *walk.TextLabel
	trimTop     bool
	firstLine   bool
	contentFont *walk.TextLabel
	cardContent *walk.Composite
}

// Keep the wrapper installed: BoxLayout.SetContainer would register the
// embedded BoxLayout itself and bypass our width constraint.
func (l *widthPreservingLayout) Container() walk.Container { return l.container }

func (l *widthPreservingLayout) SetContainer(container walk.Container) {
	l.container = container
	if container != nil {
		container.RequestLayout()
	}
}

func (l *widthPreservingLayout) CreateLayoutItem(ctx *walk.LayoutContext) walk.ContainerLayoutItem {
	item := &widthPreservingLayoutItem{ContainerLayoutItem: l.BoxLayout.CreateLayoutItem(ctx), mode: l.mode}
	item.renderedLabels = make(map[win.HWND]renderedText)
	if l.container != nil {
		for index := 0; index < l.container.Children().Len(); index++ {
			if label, ok := l.container.Children().At(index).(*walk.TextLabel); ok {
				item.renderedLabels[label.Handle()] = snapshotRenderedLabel(label)
			}
		}
	}
	if l.cardContent != nil {
		item.cardContent = walk.CreateLayoutItemsForContainerWithContext(l.cardContent, ctx)
	}
	if l.contentFont != nil {
		item.lineHeight = labelLineHeight(l.contentFont)
	}
	if l.firstLine {
		item.firstLineHeight = labelLineHeight(l.term)
	}
	if l.term != nil {
		item.termWidth = MeasureRenderedLabel(l.term, l.term.Text(), 100000, true).Width + 2
		if l.trimTop {
			item.topInset = textTopInset(l.term)
		}
	}
	return item
}

type widthPreservingLayoutItem struct {
	walk.ContainerLayoutItem
	mode            string
	termWidth       int
	topInset        int
	lineHeight      int
	firstLineHeight int
	cardContent     walk.ContainerLayoutItem
	renderedLabels  map[win.HWND]renderedText
}

func (li *widthPreservingLayoutItem) MinSizeForSize(size walk.Size) walk.Size {
	if li.mode == "root" || li.mode == "card" {
		return walk.Size{}
	}
	return li.requiredSize(size)
}

func (li *widthPreservingLayoutItem) requiredSize(size walk.Size) walk.Size {
	children := li.Children()
	gap := 12
	switch li.mode {
	case "root":
		if len(children) < 2 {
			return walk.Size{}
		}
		width := max(1, size.Width-2*gap)
		search := li.height(children[0], width)
		return walk.Size{Height: search + 3*gap + li.height(children[1], width)}
	case "search":
		return walk.Size{Height: 42}
	case "body":
		if len(children) < 2 {
			return walk.Size{}
		}
		return walk.Size{Height: li.height(children[1], max(1, size.Width-size.Width/3))}
	case "card":
		if len(children) < 2 {
			return walk.Size{}
		}
		return walk.Size{Height: li.height(li.cardContent, max(1, size.Width-2*gap)) + 42 + 2*gap + max(1, li.lineHeight)}
	case "result":
		if len(children) < 3 {
			return walk.Size{}
		}
		height := li.height(children[0], max(1, size.Width-2*gap)) + 42 + gap
		return walk.Size{Height: height}
	case "input":
		return walk.Size{Height: 42}
	case "term":
		if len(children) < 2 {
			return walk.Size{}
		}
		reserve := 0
		if children[1].Visible() {
			reserve = 84 + gap
		}
		width := max(1, min(li.termWidth, size.Width-reserve))
		height := max(0, li.height(children[0], width)-li.topInset)
		if li.firstLineHeight > 0 && children[1].Visible() {
			height += max(0, (42-li.firstLineHeight)/2)
		}
		if children[1].Visible() {
			height = max(42, height)
		}
		return walk.Size{Height: height}
	case "footer":
		return walk.Size{Height: 42}
	case "column":
		height := 0
		previous := -1
		for index, item := range children {
			if !item.Visible() {
				continue
			}
			width := size.Width
			h := li.height(item, max(1, width))
			if h == 0 {
				continue
			}
			if height > 0 {
				height += li.contentGap(previous)
			}
			height += h
			previous = index
		}
		return walk.Size{Height: height}
	}
	return walk.Size{}
}

func (li *widthPreservingLayoutItem) MinSize() walk.Size {
	return li.MinSizeForSize(li.Geometry().ClientSize)
}
func (li *widthPreservingLayoutItem) HeightForWidth(width int) int {
	return li.MinSizeForSize(walk.Size{Width: width}).Height
}
func (li *widthPreservingLayoutItem) HasHeightForWidth() bool { return true }

func (li *widthPreservingLayoutItem) height(item walk.LayoutItem, width int) int {
	if text, ok := li.renderedLabels[item.Handle()]; ok {
		return text.measure(width, false).Height
	}
	if hfw, ok := item.(walk.HeightForWidther); ok && hfw.HasHeightForWidth() {
		return hfw.HeightForWidth(width)
	}
	return li.MinSizeEffectiveForChild(item).Height
}

func (li *widthPreservingLayoutItem) PerformLayout() []walk.LayoutResultItem {
	children := li.Children()
	size := li.Geometry().ClientSize
	gap := 12
	if len(children) == 0 {
		return nil
	}
	place := func(item walk.LayoutItem, x, y, width, height int) walk.LayoutResultItem {
		return walk.LayoutResultItem{Item: item, Bounds: walk.Rectangle{X: x, Y: y, Width: max(1, width), Height: max(0, height)}}
	}
	switch li.mode {
	case "root":
		if len(children) < 2 {
			return nil
		}
		width := size.Width - 2*gap
		height := li.height(children[0], width)
		return []walk.LayoutResultItem{place(children[0], gap, gap, width, height), place(children[1], gap, 2*gap+height, width, size.Height-height-3*gap)}
	case "search":
		if len(children) < 2 {
			return nil
		}
		return []walk.LayoutResultItem{place(children[0], 0, 0, size.Width-84-gap, 42), place(children[1], size.Width-84, 0, 84, 42)}
	case "input":
		return []walk.LayoutResultItem{place(children[0], 1, 1, size.Width-2, size.Height-2)}
	case "body":
		if len(children) < 2 {
			return nil
		}
		width := size.Width / 3
		return []walk.LayoutResultItem{place(children[0], 0, 0, width, size.Height), place(children[1], width, 0, size.Width-width, size.Height)}
	case "card":
		if len(children) < 2 {
			return nil
		}
		lineGap := max(1, li.lineHeight)
		contentHeight := li.height(li.cardContent, max(1, size.Width-2*gap))
		contentHeight = min(contentHeight, max(0, size.Height-2*gap-lineGap-42))
		bottom := gap + contentHeight + lineGap
		results := []walk.LayoutResultItem{
			place(children[0], gap, gap, size.Width-2*gap, contentHeight),
			place(children[1], gap, bottom, size.Width-2*gap, 42),
		}
		if len(children) > 2 {
			results = append(results, place(children[2], (size.Width-120)/2, bottom, 120, 42))
		}
		return results
	case "result":
		if len(children) < 3 {
			return nil
		}
		labelWidth := walk.IntFrom96DPI(100, li.Context().DPI())
		labelHeight := max(1, li.height(children[1], labelWidth))
		top := 0
		if li.mode == "card" {
			top = gap
			size.Height -= gap
		}
		results := []walk.LayoutResultItem{place(children[0], gap, top, size.Width-2*gap, max(0, size.Height-top-42-gap)), place(children[1], size.Width-labelWidth-gap, max(0, size.Height-labelHeight), labelWidth, labelHeight), place(children[2], gap, max(0, size.Height-42), size.Width-labelWidth-3*gap, 42)}
		if len(children) > 3 {
			results = append(results, place(children[3], (size.Width-120)/2, max(0, size.Height-42), 120, 42))
		}
		return results
	case "term":
		if len(children) < 2 {
			return nil
		}
		reserve := 0
		if children[1].Visible() {
			reserve = 84 + gap
		}
		width := max(1, min(li.termWidth, size.Width-reserve))
		height := li.height(children[0], width)
		textY := -li.topInset
		buttonY := max(0, (height-li.topInset-42)/2)
		if li.firstLineHeight > 0 {
			textY = max(0, (42-li.firstLineHeight)/2)
			buttonY = max(0, (li.firstLineHeight-42)/2)
		}
		results := []walk.LayoutResultItem{place(children[0], 0, textY, width, height)}
		if children[1].Visible() {
			results = append(results, place(children[1], width+gap, buttonY, 84, 42))
		}
		return results
	case "footer":
		var results []walk.LayoutResultItem
		x := 0
		for _, item := range children {
			width := 84
			if item.Geometry().MinSize.Width > 84 {
				width = item.Geometry().MinSize.Width
			}
			results = append(results, place(item, x, 0, width, 42))
			x += width + gap
		}
		return results
	case "column":
		var results []walk.LayoutResultItem
		y := 0
		previous := -1
		for index, item := range children {
			if !item.Visible() {
				continue
			}
			width := size.Width
			height := li.height(item, max(1, width))
			if height == 0 {
				continue
			}
			if y > 0 {
				y += li.contentGap(previous)
			}
			results = append(results, place(item, 0, y, width, height))
			y += height
			previous = index
		}
		return results
	}
	return nil
}

func (li *widthPreservingLayoutItem) contentGap(previous int) int {
	if previous == 0 {
		return max(1, li.lineHeight)
	}
	return max(1, li.lineHeight/2)
}
