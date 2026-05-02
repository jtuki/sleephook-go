package main

import (
	"syscall"
	"unsafe"
)

type taskmgrBlocker struct {
	handle uintptr
}

func newBlocker() *taskmgrBlocker {
	return &taskmgrBlocker{}
}

func (b *taskmgrBlocker) block() {
	if b.handle != 0 {
		return
	}
	path, _ := syscall.UTF16PtrFromString(`C:\Windows\System32\Taskmgr.exe`)
	h, _, _ := pCreateFileW.Call(
		uintptr(unsafe.Pointer(path)),
		GENERIC_READ,
		0, // exclusive, no sharing
		0,
		OPEN_EXISTING,
		0x80, // FILE_ATTRIBUTE_NORMAL
		0,
	)
	if h != ^uintptr(0) { // not INVALID_HANDLE_VALUE
		b.handle = h
	}
}

func (b *taskmgrBlocker) unblock() {
	if b.handle != 0 {
		pCloseHandle.Call(b.handle)
		b.handle = 0
	}
}
