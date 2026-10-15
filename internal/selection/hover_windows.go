//go:build windows

package selection

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/lxn/win"
	"touchdict/internal/model"
)

// Alt lookup must never synthesize keys or clicks in the source application.
// Freeze video pixels first, then preserve an existing UIA selection if any.
func (r *Reader) readHover(ctx context.Context, pointer win.POINT) (model.Selection, error) {
	path, area, captureErr := captureOCR(pointer)
	if captureErr == nil {
		defer os.Remove(path)
	} else if r.Diagnostics != nil {
		r.Diagnostics("OCR capture: " + captureErr.Error())
	}
	if ctx.Err() != nil {
		return model.Selection{}, ctx.Err()
	}
	details := readNativeDetails(ctx, "", pointer, true)
	if r.Diagnostics != nil {
		r.Diagnostics("passive Alt selection: " + details.Diagnostic)
	}
	text := normalize(details.Text)
	if text != "" && hasLatin(text) {
		var bounds *model.SelectionBounds
		if details.Right > details.Left && details.Bottom > details.Top {
			bounds = &details.SelectionBounds
		}
		return model.Selection{Text: text, Context: normalize(details.Context), Bounds: bounds, Multiword: len(strings.Fields(text)) > 1}, nil
	}
	if captureErr == nil && ctx.Err() == nil {
		selected, err := readOCR(ctx, path, area, pointer)
		if err == nil {
			if r.Diagnostics != nil {
				r.Diagnostics("passive Alt selection: Windows OCR")
			}
			return selected, nil
		}
		if r.Diagnostics != nil {
			r.Diagnostics("OCR fallback: " + err.Error())
		}
	}
	if ctx.Err() != nil {
		return model.Selection{}, ctx.Err()
	}
	return model.Selection{}, errors.New("未读到英文内容，请输入单词或短语后按 Enter 查询")
}
