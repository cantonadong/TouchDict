//go:build windows && amd64

package edgeview

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WebView2's stable COM interfaces use the slot order in WebView2.h.
// Only this file deals with raw COM calls; callers use View's typed methods.
type object struct{ table *[96]uintptr }

//go:uintptrescapes
func (o *object) call(slot int, args ...uintptr) uintptr {
	if o == nil {
		return 0x80004003 // E_POINTER
	}
	params := append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	hr, _, _ := syscall.SyscallN(o.table[slot], params...)
	runtime.KeepAlive(o)
	return hr
}

func (o *object) release() {
	if o != nil {
		o.call(2)
	}
}

func result(hr uintptr, operation string) error {
	if int32(hr) < 0 {
		return fmt.Errorf("%s失败（0x%08X）", operation, uint32(hr))
	}
	return nil
}

func (o *object) string(slot int) (string, error) {
	var text *uint16
	err := result(o.call(slot, uintptr(unsafe.Pointer(&text))), "读取 WebView2 信息")
	if text != nil {
		defer windows.CoTaskMemFree(unsafe.Pointer(text))
	}
	return windows.UTF16PtrToString(text), err
}

type handler struct {
	table  *[4]uintptr
	iid    windows.GUID
	refs   atomic.Uint32
	pin    runtime.Pinner
	invoke func(uintptr, uintptr) uintptr
}

// Own a Go reference for as long as COM owns a reference, including asynchronous
// completions arriving after the window was closed.
var handlers sync.Map
var unknownIID = windows.GUID{Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
var handlerTable = [4]uintptr{
	syscall.NewCallback(func(h *handler, iid *windows.GUID, out *uintptr) uintptr {
		if out == nil {
			return 0x80004003
		}
		*out = 0
		if iid == nil || (*iid != unknownIID && *iid != h.iid) {
			return 0x80004002
		}
		*out = uintptr(unsafe.Pointer(h))
		h.refs.Add(1)
		return 0
	}),
	syscall.NewCallback(func(h *handler) uintptr { return uintptr(h.refs.Add(1)) }),
	syscall.NewCallback(func(h *handler) uintptr { return h.release() }),
	syscall.NewCallback(func(h *handler, first, second uintptr) uintptr { return h.invoke(first, second) }),
}

func newHandler(iid string, invoke func(uintptr, uintptr) uintptr) *handler {
	guid, err := windows.GUIDFromString(iid)
	if err != nil {
		panic(err)
	} // Compile-time interface identifiers only.
	h := &handler{table: &handlerTable, iid: guid, invoke: invoke}
	h.refs.Store(1)
	h.pin.Pin(h)
	handlers.Store(h, h)
	return h
}

func (h *handler) release() uintptr {
	refs := h.refs.Add(^uint32(0))
	if refs == 0 {
		handlers.Delete(h)
		h.pin.Unpin()
	}
	return uintptr(refs)
}

func (h *handler) address() uintptr { return uintptr(unsafe.Pointer(h)) }

func asObject(pointer uintptr) *object { return (*object)(unsafe.Pointer(pointer)) }

type registration struct {
	object  *object
	remove  int
	token   int64
	handler *handler
}

func (v *View) listen(o *object, add int, iid string, fn func(uintptr, uintptr) uintptr) error {
	h := newHandler(iid, fn)
	var token int64
	err := result(o.call(add, h.address(), uintptr(unsafe.Pointer(&token))), "注册 WebView2 事件")
	if err != nil {
		h.release()
		return err
	}
	v.events = append(v.events, registration{o, add + 1, token, h})
	return nil
}
