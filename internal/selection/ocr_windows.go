//go:build windows

package selection

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/lxn/win"
	"touchdict/internal/model"
)

//go:embed ocr.ps1
var ocrScript string

// Freeze the pixels before copy attempts or synthetic clicks change the video.
func captureOCR(pointer win.POINT) (string, image.Rectangle, error) {
	left := int(win.GetSystemMetrics(76))
	top := int(win.GetSystemMetrics(77))
	screen := image.Rect(left, top, left+int(win.GetSystemMetrics(78)), top+int(win.GetSystemMetrics(79)))
	area := image.Rect(int(pointer.X)-600, int(pointer.Y)-100, int(pointer.X)+600, int(pointer.Y)+100).Intersect(screen)
	if area.Empty() {
		return "", area, fmt.Errorf("pointer outside desktop")
	}
	dc := win.GetDC(0)
	if dc == 0 {
		return "", area, fmt.Errorf("desktop DC unavailable")
	}
	defer win.ReleaseDC(0, dc)
	mem := win.CreateCompatibleDC(dc)
	if mem == 0 {
		return "", area, fmt.Errorf("capture DC unavailable")
	}
	defer win.DeleteDC(mem)
	bitmap := win.CreateCompatibleBitmap(dc, int32(area.Dx()), int32(area.Dy()))
	if bitmap == 0 {
		return "", area, fmt.Errorf("capture bitmap unavailable")
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	old := win.SelectObject(mem, win.HGDIOBJ(bitmap))
	ok := win.BitBlt(mem, 0, 0, int32(area.Dx()), int32(area.Dy()), dc, int32(area.Min.X), int32(area.Min.Y), win.SRCCOPY|0x40000000)
	win.SelectObject(mem, old)
	if !ok {
		return "", area, fmt.Errorf("desktop capture failed")
	}
	pixels := make([]byte, area.Dx()*area.Dy()*4)
	info := win.BITMAPINFO{BmiHeader: win.BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})), BiWidth: int32(area.Dx()), BiHeight: -int32(area.Dy()), BiPlanes: 1, BiBitCount: 32}}
	if win.GetDIBits(dc, bitmap, 0, uint32(area.Dy()), &pixels[0], &info, win.DIB_RGB_COLORS) == 0 {
		return "", area, fmt.Errorf("capture pixels unavailable")
	}
	for i := 0; i < len(pixels); i += 4 {
		pixels[i], pixels[i+2], pixels[i+3] = pixels[i+2], pixels[i], 255
	}
	file, err := os.CreateTemp("", "touchdict-ocr-*.png")
	if err != nil {
		return "", area, err
	}
	err = png.Encode(file, &image.RGBA{Pix: pixels, Stride: area.Dx() * 4, Rect: image.Rect(0, 0, area.Dx(), area.Dy())})
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(file.Name())
		return "", area, err
	}
	return file.Name(), area, nil
}

func readOCR(parent context.Context, path string, area image.Rectangle, pointer win.POINT) (model.Selection, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	encoded := utf16.Encode([]rune(ocrScript))
	bytes := make([]byte, len(encoded)*2)
	for i, v := range encoded {
		bytes[i*2], bytes[i*2+1] = byte(v), byte(v>>8)
	}
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(bytes))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.Env = append(os.Environ(), "TOUCHDICT_OCR_IMAGE="+path)
	output, err := cmd.Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			return model.Selection{}, fmt.Errorf("Windows OCR: %w: %s", err, strings.TrimSpace(string(failure.Stderr)))
		}
		return model.Selection{}, fmt.Errorf("Windows OCR: %w", err)
	}
	var lines []struct {
		Text  string
		Words []struct {
			Text       string
			X, Y, W, H float64
		}
	}
	if err = json.Unmarshal(output, &lines); err != nil {
		return model.Selection{}, err
	}
	for _, line := range lines {
		for _, word := range line.Words {
			bounds := model.SelectionBounds{Left: area.Min.X + int(word.X), Top: area.Min.Y + int(word.Y), Right: area.Min.X + int(word.X+word.W+0.999), Bottom: area.Min.Y + int(word.Y+word.H+0.999)}
			// Only accept a word directly under the pointer, never a nearby subtitle.
			if int(pointer.X) < bounds.Left-3 || int(pointer.X) > bounds.Right+3 || int(pointer.Y) < bounds.Top-3 || int(pointer.Y) > bounds.Bottom+3 {
				continue
			}
			text := strings.Trim(word.Text, ".,!?;:\"()[]{}“”")
			if !hasLatin(text) {
				continue
			}
			return model.Selection{Text: text, Context: normalize(line.Text), Bounds: &bounds, Multiword: len(strings.Fields(text)) > 1}, nil
		}
	}
	return model.Selection{}, fmt.Errorf("OCR found no English word under pointer")
}
