package main

import (
	"os"
	"path/filepath"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const (
	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004

	WM_TRAYICON  = 0x0401 // WM_USER + 1
	WM_RBUTTONUP = 0x0205

	MF_STRING  = 0x00000000
	MF_SEPARATOR = 0x00000800
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

type GdiplusStartupInput struct {
	GdiplusVersion          uint32
	DebugEventCallback      uintptr
	SuppressBackgroundThread int32
	SuppressExternalCodecs  int32
}

var (
	shell32 = syscall.NewLazyDLL("shell32.dll")
	gdiplus = syscall.NewLazyDLL("gdiplus.dll")

	pShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenuW         = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")

	pGdiplusStartup         = gdiplus.NewProc("GdiplusStartup")
	pGdiplusShutdown        = gdiplus.NewProc("GdiplusShutdown")
	pGdipCreateBitmapFromFile = gdiplus.NewProc("GdipCreateBitmapFromFile")
	pGdipCreateHICONFromBitmap = gdiplus.NewProc("GdipCreateHICONFromBitmap")
	pGdipDisposeImage       = gdiplus.NewProc("GdipDisposeImage")
	pDestroyIcon            = user32.NewProc("DestroyIcon")
)

var gTrayIcon uintptr

func loadPNGIcon() uintptr {
	iconPath := iconFilePath()
	if _, err := os.Stat(iconPath); err != nil {
		logMsg("icon not found: %s, using default", iconPath)
		return 0
	}

	var token uintptr
	si := GdiplusStartupInput{GdiplusVersion: 1}
	ret, _, _ := pGdiplusStartup.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&si)), 0)
	if ret != 0 {
		logMsg("GdiplusStartup failed: %d", ret)
		return 0
	}
	defer pGdiplusShutdown.Call(token)

	pathPtr, _ := syscall.UTF16PtrFromString(iconPath)
	var bitmap uintptr
	ret, _, _ = pGdipCreateBitmapFromFile.Call(uintptr(unsafe.Pointer(pathPtr)), uintptr(unsafe.Pointer(&bitmap)))
	if ret != 0 || bitmap == 0 {
		logMsg("GdipCreateBitmapFromFile failed: %d", ret)
		return 0
	}
	defer pGdipDisposeImage.Call(bitmap)

	var hicon uintptr
	ret, _, _ = pGdipCreateHICONFromBitmap.Call(bitmap, uintptr(unsafe.Pointer(&hicon)))
	if ret != 0 || hicon == 0 {
		logMsg("GdipCreateHICONFromBitmap failed: %d", ret)
		return 0
	}

	logMsg("loaded icon from %s: hicon=0x%X", iconPath, hicon)
	return hicon
}

func iconFilePath() string {
	exe, err := os.Executable()
	if err == nil {
		p := filepath.Join(filepath.Dir(exe), "sleep-icon.png")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "sleep-icon.png"
}

func addTrayIcon(hwnd uintptr) {
	gTrayIcon = loadPNGIcon()

	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.Hwnd = hwnd
	nid.UID = 1
	nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.UCallbackMessage = WM_TRAYICON
	nid.HIcon = gTrayIcon
	copyTip(&nid.SzTip, "SleepHook - 运行中")

	ret, _, err := pShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))
	logMsg("Shell_NotifyIcon ADD: ret=%d err=%v", ret, err)
}

func removeTrayIcon(hwnd uintptr) {
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.Hwnd = hwnd
	nid.UID = 1
	pShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
	if gTrayIcon != 0 {
		pDestroyIcon.Call(gTrayIcon)
		gTrayIcon = 0
	}
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

	extendLabel := "延长 10 分钟"
	if gExtendUntil.After(time.Now()) {
		remaining := time.Until(gExtendUntil).Truncate(time.Second)
		extendLabel = fmt.Sprintf("延长 10 分钟 (剩余 %s)", remaining)
	}

	ext, _ := syscall.UTF16PtrFromString(extendLabel)
	pAppendMenuW.Call(menu, MF_STRING, 2, uintptr(unsafe.Pointer(ext)))

	sep, _ := syscall.UTF16PtrFromString("-")
	pAppendMenuW.Call(menu, MF_SEPARATOR, 0, uintptr(unsafe.Pointer(sep)))

	quit, _ := syscall.UTF16PtrFromString("退出 SleepHook")
	pAppendMenuW.Call(menu, MF_STRING, 1, uintptr(unsafe.Pointer(quit)))

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
