package main

import (
	"syscall"
	"unsafe"
)

const (
	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004

	IDI_APPLICATION = 32512

	WM_TRAYICON    = 0x0401 // WM_USER + 1
	WM_RBUTTONUP   = 0x0205

	MF_STRING    = 0x00000000
	TPM_LEFTALIGN = 0x0000
)

type NOTIFYICONDATAW struct {
	CbSize           uint32
	Hwnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UTimeout         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

var (
	shell32 = syscall.NewLazyDLL("shell32.dll")

	pShellNotifyIconW   = shell32.NewProc("Shell_NotifyIconW")
	pLoadIconW          = user32.NewProc("LoadIconW")
	pCreatePopupMenu    = user32.NewProc("CreatePopupMenu")
	pAppendMenuW        = user32.NewProc("AppendMenuW")
	pTrackPopupMenu     = user32.NewProc("TrackPopupMenu")
	pDestroyMenu        = user32.NewProc("DestroyMenu")
	pGetCursorPos       = user32.NewProc("GetCursorPos")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

func addTrayIcon(hwnd uintptr) {
	icon, _, _ := pLoadIconW.Call(0, IDI_APPLICATION)

	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.Hwnd = hwnd
	nid.UID = 1
	nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.UCallbackMessage = WM_TRAYICON
	nid.HIcon = icon
	copyTip(&nid.SzTip, "SleepHook - 运行中")

	pShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))
}

func removeTrayIcon(hwnd uintptr) {
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.Hwnd = hwnd
	nid.UID = 1
	pShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
}

func updateTrayTooltip(hwnd uintptr, text string) {
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.Hwnd = hwnd
	nid.UID = 1
	nid.UFlags = NIF_TIP
	copyTip(&nid.SzTip, text)
	pShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := pCreatePopupMenu.Call()
	text, _ := syscall.UTF16PtrFromString("退出 SleepHook")
	pAppendMenuW.Call(menu, MF_STRING, 1, uintptr(unsafe.Pointer(text)))

	var pt struct{ X, Y int32 }
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hwnd)
	pTrackPopupMenu.Call(menu, uintptr(TPM_LEFTALIGN),
		uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	pDestroyMenu.Call(menu)
}

func copyTip(tip *[128]uint16, s string) {
	u16, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	n := len(u16)
	if n > 127 {
		n = 127
	}
	copy(tip[:n], u16[:n])
	if n < 128 {
		tip[n] = 0
	}
}
