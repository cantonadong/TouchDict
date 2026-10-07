//go:build windows && amd64

package selection

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sys/windows"
	"math"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"touchdict/internal/model"
	"unicode/utf16"
	"unsafe"
)

type contextDetails struct {
	model.SelectionBounds
	Context, Diagnostic string
}

var (
	uiaOle32         = syscall.NewLazyDLL("ole32.dll")
	uiaOleAuto       = syscall.NewLazyDLL("oleaut32.dll")
	uiaInitialize    = uiaOle32.NewProc("CoInitializeEx")
	uiaUninitialize  = uiaOle32.NewProc("CoUninitialize")
	uiaCreate        = uiaOle32.NewProc("CoCreateInstance")
	uiaFreeString    = uiaOleAuto.NewProc("SysFreeString")
	uiaStringLength  = uiaOleAuto.NewProc("SysStringLen")
	uiaAllocString   = uiaOleAuto.NewProc("SysAllocString")
	uiaArrayDestroy  = uiaOleAuto.NewProc("SafeArrayDestroy")
	uiaArrayAccess   = uiaOleAuto.NewProc("SafeArrayAccessData")
	uiaArrayUnaccess = uiaOleAuto.NewProc("SafeArrayUnaccessData")
	uiaArrayLower    = uiaOleAuto.NewProc("SafeArrayGetLBound")
	uiaArrayUpper    = uiaOleAuto.NewProc("SafeArrayGetUBound")
	uiaArrayType     = uiaOleAuto.NewProc("SafeArrayGetVartype")
	uiaClass         = windows.GUID{Data1: 0xff48dba4, Data2: 0x60ef, Data3: 0x4201, Data4: [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
	uiaIID           = windows.GUID{Data1: 0x30cbe57d, Data2: 0xd9d0, Data3: 0x452a, Data4: [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
	uiaTextIID       = windows.GUID{Data1: 0x32eba289, Data2: 0x3583, Data3: 0x42c9, Data4: [8]byte{0x9c, 0x59, 0x3b, 0x6d, 0x9a, 0x1e, 0x9b, 0x6a}}
	sentenceBoundary = regexp.MustCompile(`[.!?。！？]["'”’)]*(?:\s+|$)|[\r\n]+`)
)

// Slots and argument types follow Microsoft's UIAutomationClient.h.
// Returned BSTRs and SAFEARRAYs belong to the caller.
type uiaObject struct{ table *[85]uintptr }

//go:uintptrescapes
func (o *uiaObject) call(slot int, args ...uintptr) uintptr {
	if o == nil {
		return 0x80004003
	}
	params := append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	hr, _, _ := syscall.SyscallN(o.table[slot], params...)
	runtime.KeepAlive(o)
	return hr
}
func uiaOK(hr uintptr) bool { return int32(hr) >= 0 }
func (o *uiaObject) release() {
	if o != nil {
		o.call(2)
	}
}
func (o *uiaObject) object(slot int, args ...uintptr) *uiaObject {
	var result *uiaObject
	var pin runtime.Pinner
	pin.Pin(&result)
	defer pin.Unpin()
	args = append(args, uintptr(unsafe.Pointer(&result)))
	if !uiaOK(o.call(slot, args...)) {
		return nil
	}
	return result
}
func (o *uiaObject) text(slot int, args ...uintptr) string {
	var value *uint16
	var pin runtime.Pinner
	pin.Pin(&value)
	defer pin.Unpin()
	args = append(args, uintptr(unsafe.Pointer(&value)))
	hr := o.call(slot, args...)
	if value == nil {
		return ""
	}
	defer uiaFreeString.Call(uintptr(unsafe.Pointer(value)))
	if !uiaOK(hr) {
		return ""
	}
	length, _, _ := uiaStringLength.Call(uintptr(unsafe.Pointer(value)))
	if length > 1000000 {
		return ""
	}
	return string(utf16.Decode(unsafe.Slice(value, int(length))))
}

// RunContextHelper runs before singleton checks and UI initialization. Its
// stdout pipe is supplied by the parent, including for a Windows GUI build.
func RunContextHelper() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	details := contextDetails{Diagnostic: "native UIA: no text provider"}
	hr, _, _ := uiaInitialize.Call(0, 0) // COINIT_MULTITHREADED
	if !uiaOK(hr) {
		details.Diagnostic = fmt.Sprintf("native UIA initialization: 0x%08x", uint32(hr))
	} else {
		defer uiaUninitialize.Call()
		x, _ := strconv.ParseInt(os.Getenv("TOUCHDICT_POINTER_X"), 10, 32)
		y, _ := strconv.ParseInt(os.Getenv("TOUCHDICT_POINTER_Y"), 10, 32)
		details = captureNative(os.Getenv("TOUCHDICT_SELECTION"), int32(x), int32(y))
	}
	_ = json.NewEncoder(os.Stdout).Encode(details)
}

func captureNative(expected string, x, y int32) contextDetails {
	details := contextDetails{Diagnostic: "native UIA: no text provider"}
	if strings.TrimSpace(expected) == "" {
		details.Diagnostic = "native UIA: empty selection"
		return details
	}
	// Match in the original string: Unicode case conversion can change byte
	// lengths, making a lowercased-string index invalid in the original name.
	nameMatch := regexp.MustCompile("(?i)" + regexp.QuoteMeta(expected))
	var automation *uiaObject
	hr, _, _ := uiaCreate.Call(uintptr(unsafe.Pointer(&uiaClass)), 0, 1, uintptr(unsafe.Pointer(&uiaIID)), uintptr(unsafe.Pointer(&automation)))
	if !uiaOK(hr) || automation == nil {
		details.Diagnostic = fmt.Sprintf("native UIA creation: 0x%08x", uint32(hr))
		return details
	}
	defer automation.release()
	walker := automation.object(16)
	if walker == nil {
		details.Diagnostic = "native UIA: no tree walker"
		return details
	}
	defer walker.release()
	packedPoint := uintptr(uint64(uint32(x)) | uint64(uint32(y))<<32)
	roots := []*uiaObject{automation.object(7, packedPoint), automation.object(8)}
	var candidates []*uiaObject
	defer func() {
		for _, element := range candidates {
			element.release()
		}
	}()
	for _, root := range roots {
		for element, depth := root, 0; element != nil && depth < 12; depth++ {
			candidates = append(candidates, element)
			if depth == 11 {
				break
			}
			element = walker.object(3, uintptr(unsafe.Pointer(element)))
		}
	}
	deadline := time.Now().Add(2400 * time.Millisecond)
	providers, visited := 0, 0
	for index := 0; index < len(candidates) && index < 160 && time.Now().Before(deadline); index++ {
		element := candidates[index]
		if element.text(30) == "#32769" {
			continue
		}
		visited++
		// Read before traversing children, so a large tree cannot exhaust the
		// budget before the selected document's provider has been consulted.
		pattern := element.object(14, 10014, uintptr(unsafe.Pointer(&uiaTextIID)))
		if pattern != nil {
			providers++
			matched := capturePattern(pattern, expected, packedPoint, &details)
			pattern.release()
			if matched {
				return details
			}
		}
		if details.Context == "" {
			var bounds struct{ Left, Top, Right, Bottom int32 }
			if uiaOK(element.call(43, uintptr(unsafe.Pointer(&bounds)))) && x >= bounds.Left && x < bounds.Right && y >= bounds.Top && y < bounds.Bottom {
				name := element.text(23)
				if len(name) > len(expected) && len(name) <= 24000 {
					if match := nameMatch.FindStringIndex(name); match != nil {
						details.Context = sentenceAt(name, match[0], match[1])
						details.Diagnostic = "native UIA: text node"
					}
				}
			}
		}
		child := walker.object(4, uintptr(unsafe.Pointer(element)))
		for count := 0; child != nil; count++ {
			if count >= 24 || len(candidates) >= 160 {
				child.release()
				break
			}
			candidates = append(candidates, child)
			child = walker.object(6, uintptr(unsafe.Pointer(child)))
		}
	}
	if details.Context == "" {
		details.Diagnostic = fmt.Sprintf("native UIA: visited=%d providers=%d, no matching sentence", visited, providers)
	}
	return details
}

func capturePattern(pattern *uiaObject, expected string, packedPoint uintptr, details *contextDetails) bool {
	var ranges []*uiaObject
	defer func() {
		for _, r := range ranges {
			r.release()
		}
	}()
	array := pattern.object(5)
	if array != nil {
		var count int32
		if uiaOK(array.call(3, uintptr(unsafe.Pointer(&count)))) {
			for i := int32(0); i < count && i < 16; i++ {
				if r := array.object(4, uintptr(i)); r != nil {
					ranges = append(ranges, r)
				}
			}
		}
		array.release()
	}
	if r := pattern.object(3, packedPoint); r != nil {
		if uiaOK(r.call(6, 2)) {
			ranges = append(ranges, r)
		} else {
			r.release()
		}
	}
	for _, r := range ranges {
		if captureRange(r, expected, details) {
			return true
		}
	}
	// Without a selection, only accept a unique document occurrence.
	document := pattern.object(7)
	if document == nil {
		return false
	}
	defer document.release()
	query, err := windows.UTF16FromString(expected)
	if err != nil {
		return false
	}
	bstr, _, _ := uiaAllocString.Call(uintptr(unsafe.Pointer(&query[0])))
	if bstr == 0 {
		return false
	}
	defer uiaFreeString.Call(bstr)
	match := document.object(8, bstr, 0, 1)
	if match == nil {
		return false
	}
	defer match.release()
	remaining := document.object(3)
	if remaining == nil {
		return false
	}
	defer remaining.release()
	if !uiaOK(remaining.call(15, 0, uintptr(unsafe.Pointer(match)), 1)) {
		return false
	}
	var other *uiaObject
	findResult := remaining.call(8, bstr, 0, 1, uintptr(unsafe.Pointer(&other)))
	if other != nil {
		other.release()
		return false
	}
	if !uiaOK(findResult) {
		return false
	}
	return captureRange(match, expected, details)
}

func captureRange(r *uiaObject, expected string, details *contextDetails) bool {
	selected := r.text(12, 4096)
	if !strings.EqualFold(normalize(selected), normalize(expected)) {
		return false
	}
	if bounds := rangeBounds(r); bounds != nil {
		details.SelectionBounds = *bounds
	}
	expanded := r.object(3)
	if expanded == nil {
		return false
	}
	defer expanded.release()
	if !uiaOK(expanded.call(6, 4)) {
		return false
	} // TextUnit_Paragraph
	var comparison int32
	if uiaOK(expanded.call(5, 1, uintptr(unsafe.Pointer(r)), 1, uintptr(unsafe.Pointer(&comparison)))) && comparison < 0 {
		if !uiaOK(expanded.call(15, 1, uintptr(unsafe.Pointer(r)), 1)) {
			return false
		}
	}
	prefix := expanded.object(3)
	if prefix == nil {
		return false
	}
	defer prefix.release()
	if !uiaOK(prefix.call(15, 1, uintptr(unsafe.Pointer(r)), 0)) {
		return false
	}
	offset := len(prefix.text(12, 32768))
	paragraph := expanded.text(12, 32768)
	if offset > len(paragraph) || offset+len(selected) > len(paragraph) {
		return false
	}
	sentence := sentenceAt(paragraph, offset, offset+len(selected))
	if !strings.Contains(strings.ToLower(normalize(sentence)), strings.ToLower(normalize(expected))) || strings.EqualFold(normalize(sentence), normalize(expected)) {
		return false
	}
	details.Context, details.Diagnostic = sentence, "native UIA: text range"
	return true
}

func sentenceAt(text string, offset, selectedEnd int) string {
	start := 0
	for _, boundary := range sentenceBoundary.FindAllStringIndex(text, -1) {
		if boundary[1] <= offset {
			start = boundary[1]
			continue
		}
		if boundary[1] >= selectedEnd {
			return strings.TrimSpace(text[start:boundary[1]])
		}
	}
	return strings.TrimSpace(text[start:])
}

func rangeBounds(r *uiaObject) *model.SelectionBounds {
	var array uintptr
	if !uiaOK(r.call(10, uintptr(unsafe.Pointer(&array)))) || array == 0 {
		return nil
	}
	defer uiaArrayDestroy.Call(array)
	var lower, upper int32
	var kind uint16
	hr, _, _ := uiaArrayType.Call(array, uintptr(unsafe.Pointer(&kind)))
	if !uiaOK(hr) || kind != 5 {
		return nil
	} // VT_R8
	hr, _, _ = uiaArrayLower.Call(array, 1, uintptr(unsafe.Pointer(&lower)))
	if !uiaOK(hr) {
		return nil
	}
	hr, _, _ = uiaArrayUpper.Call(array, 1, uintptr(unsafe.Pointer(&upper)))
	count := int(upper) - int(lower) + 1
	if !uiaOK(hr) || count <= 0 || count > 4096 || count%4 != 0 {
		return nil
	}
	var data *float64
	hr, _, _ = uiaArrayAccess.Call(array, uintptr(unsafe.Pointer(&data)))
	if !uiaOK(hr) || data == nil {
		return nil
	}
	defer uiaArrayUnaccess.Call(array)
	values := unsafe.Slice(data, count)
	var result *model.SelectionBounds
	for i := 0; i < count; i += 4 {
		x, y, width, height := values[i], values[i+1], values[i+2], values[i+3]
		if width <= 0 || height <= 0 || math.IsNaN(x+y+width+height) || math.IsInf(x+y+width+height, 0) {
			continue
		}
		b := model.SelectionBounds{Left: int(math.Floor(x)), Top: int(math.Floor(y)), Right: int(math.Ceil(x + width)), Bottom: int(math.Ceil(y + height))}
		if result == nil {
			result = &b
		} else {
			result.Left = min(result.Left, b.Left)
			result.Top = min(result.Top, b.Top)
			result.Right = max(result.Right, b.Right)
			result.Bottom = max(result.Bottom, b.Bottom)
		}
	}
	return result
}
