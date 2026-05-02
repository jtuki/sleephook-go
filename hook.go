package main

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

const (
	WH_KEYBOARD_LL = 13
	WH_MOUSE_LL    = 14

	WM_KEYDOWN    = 0x0100
	WM_SYSKEYDOWN = 0x0104
	WM_MOUSEMOVE  = 0x0200

	VK_F7 = 0x76
)

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type hookManager struct {
	mu     sync.Mutex
	kbHook uintptr
	msHook uintptr
	kbCB   uintptr
	msCB   uintptr
	active bool
}

func newHookManager() *hookManager {
	hm := &hookManager{}
	hm.kbCB = syscall.NewCallback(hm.keyboardProc)
	hm.msCB = syscall.NewCallback(hm.mouseProc)
	return hm
}

func (h *hookManager) keyboardProc(nCode uintptr, wParam uintptr, lParam uintptr) uintptr {
	if int32(nCode) >= 0 && (wParam == WM_KEYDOWN || wParam == WM_SYSKEYDOWN) {
		kb := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		if kb.VkCode == VK_F7 {
			ret, _, _ := pCallNextHookEx.Call(0, nCode, wParam, lParam)
			return ret
		}
	}
	return 1
}

func (h *hookManager) mouseProc(nCode uintptr, wParam uintptr, lParam uintptr) uintptr {
	if int32(nCode) >= 0 && wParam == WM_MOUSEMOVE {
		ret, _, _ := pCallNextHookEx.Call(0, nCode, wParam, lParam)
		return ret
	}
	return 1
}

func (h *hookManager) install() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.active {
		return nil
	}

	kbHook, _, err := pSetWindowsHookExW.Call(WH_KEYBOARD_LL, h.kbCB, 0, 0)
	if kbHook == 0 {
		return fmt.Errorf("keyboard hook: %w", err)
	}

	msHook, _, err := pSetWindowsHookExW.Call(WH_MOUSE_LL, h.msCB, 0, 0)
	if msHook == 0 {
		pUnhookWindowsHookEx.Call(kbHook)
		return fmt.Errorf("mouse hook: %w", err)
	}

	h.kbHook = kbHook
	h.msHook = msHook
	h.active = true
	return nil
}

func (h *hookManager) uninstall() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.active {
		return
	}
	if h.kbHook != 0 {
		pUnhookWindowsHookEx.Call(h.kbHook)
		h.kbHook = 0
	}
	if h.msHook != 0 {
		pUnhookWindowsHookEx.Call(h.msHook)
		h.msHook = 0
	}
	h.active = false
}
