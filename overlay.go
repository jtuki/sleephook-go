package main

import (
	"syscall"
	"unsafe"
)

const (
	WS_POPUP         = 0x80000000
	WS_EX_TOPMOST    = 0x00000008
	WS_EX_LAYERED    = 0x00080000
	WS_EX_TOOLWINDOW = 0x00000080

	SW_SHOW = 5
	SW_HIDE = 0

	SWP_NOMOVE     = 0x0002
	SWP_NOSIZE     = 0x0001
	SWP_SHOWWINDOW = 0x0040

	LWA_ALPHA = 0x02

	SM_CXSCREEN        = 0
	SM_CYSCREEN        = 1
	SM_XVIRTUALSCREEN  = 76
	SM_YVIRTUALSCREEN  = 77
	SM_CXVIRTUALSCREEN = 78
	SM_CYVIRTUALSCREEN = 79

	IDC_ARROW = 32512

	WM_TIMER   = 0x0113
	WM_CLOSE   = 0x0010
	WM_DESTROY = 0x0002
	WM_COMMAND = 0x0111
	WM_QUIT    = 0x0012

	GENERIC_READ  = 0x80000000
	OPEN_EXISTING = 3
)

type WNDCLASSEX struct {
	Size          uint32
	Style         uint32
	LpfnWndProc   uintptr
	CntClsExtra   int32
	CntWndExtra   int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	pRegisterClassExW           = user32.NewProc("RegisterClassExW")
	pCreateWindowExW            = user32.NewProc("CreateWindowExW")
	pDefWindowProcW             = user32.NewProc("DefWindowProcW")
	pShowWindow                 = user32.NewProc("ShowWindow")
	pSetWindowPos               = user32.NewProc("SetWindowPos")
	pSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	pGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	pGetModuleHandleW           = user32.NewProc("GetModuleHandleW")
	pPostQuitMessage            = user32.NewProc("PostQuitMessage")
	pGetMessageW                = user32.NewProc("GetMessageW")
	pTranslateMessage           = user32.NewProc("TranslateMessage")
	pDispatchMessageW           = user32.NewProc("DispatchMessageW")
	pSetTimer                   = user32.NewProc("SetTimer")
	pKillTimer                  = user32.NewProc("KillTimer")
	pLoadCursorW                = user32.NewProc("LoadCursorW")
	pMessageBoxW                = user32.NewProc("MessageBoxW")

	pSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx      = user32.NewProc("CallNextHookEx")

	pCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")

	pCreateFileW = kernel32.NewProc("CreateFileW")
	pCloseHandle = kernel32.NewProc("CloseHandle")
)

var overlayWndProcCB uintptr

func createOverlayWindow() uintptr {
	hInst, _, _ := pGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("SleepHookOverlay")
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(0) // black

	overlayWndProcCB = syscall.NewCallback(overlayWndProc)

	wc := WNDCLASSEX{
		Size:          uint32(unsafe.Sizeof(WNDCLASSEX{})),
		LpfnWndProc:   overlayWndProcCB,
		HInstance:     hInst,
		HCursor:       cursor,
		HbrBackground: brush,
		LpszClassName: className,
	}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// Cover all monitors (virtual screen)
	x, _, _ := pGetSystemMetrics.Call(SM_XVIRTUALSCREEN)
	y, _, _ := pGetSystemMetrics.Call(SM_YVIRTUALSCREEN)
	cx, _, _ := pGetSystemMetrics.Call(SM_CXVIRTUALSCREEN)
	cy, _, _ := pGetSystemMetrics.Call(SM_CYVIRTUALSCREEN)

	if cx == 0 || cy == 0 {
		// fallback to primary screen
		cx, _, _ = pGetSystemMetrics.Call(SM_CXSCREEN)
		cy, _, _ = pGetSystemMetrics.Call(SM_CYSCREEN)
	}

	hwnd, _, _ := pCreateWindowExW.Call(
		WS_EX_TOPMOST|WS_EX_LAYERED|WS_EX_TOOLWINDOW,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(className)),
		WS_POPUP,
		x, y, cx, cy,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		showError("Failed to create overlay window")
		return 0
	}

	// 50% transparency
	pSetLayeredWindowAttributes.Call(hwnd, 0, 128, LWA_ALPHA)
	return hwnd
}

func overlayWndProc(hwnd uintptr, msg uint32, wparam uintptr, lparam uintptr) uintptr {
	switch msg {
	case WM_TIMER:
		checkAndToggle()
		return 0
	case WM_CLOSE:
		return 0
	case WM_COMMAND:
		if wparam == 1 { // tray menu: exit
			pPostQuitMessage.Call(0)
			return 0
		}
	case WM_TRAYICON:
		if lparam == WM_RBUTTONUP {
			showTrayMenu(hwnd)
		}
		return 0
	case WM_DESTROY:
		pPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

func showOverlay(hwnd uintptr) {
	hwndTopmost := ^uintptr(0) // HWND_TOPMOST = (HWND)-1
	pSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0,
		uintptr(SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW))
	pShowWindow.Call(hwnd, SW_SHOW)
}

func hideOverlay(hwnd uintptr) {
	pShowWindow.Call(hwnd, SW_HIDE)
}

func showError(text string) {
	t, _ := syscall.UTF16PtrFromString(text)
	title, _ := syscall.UTF16PtrFromString("SleepHook")
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(title)), 0x10)
}
