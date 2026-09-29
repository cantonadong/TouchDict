//go:build windows

package trigger

import "unsafe"

func syscallPointer(v uintptr) unsafe.Pointer { return unsafe.Pointer(v) }
