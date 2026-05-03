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

	WM_PAINT   = 0x000F
	WM_TIMER   = 0x0113
	WM_CLOSE   = 0x0010
	WM_DESTROY = 0x0002
	WM_COMMAND = 0x0111
	WM_QUIT    = 0x0012

	GENERIC_READ  = 0x80000000
	OPEN_EXISTING = 3

	// GDI text drawing
	TRANSPARENT      = 1
	DT_CENTER        = 0x00000001
	DT_VCENTER       = 0x00000004
	DT_SINGLELINE    = 0x00000020
	DEFAULT_CHARSET  = 1
	FW_BOLD          = 700
	CLEARTYPE_QUALITY = 5
	DEFAULT_PITCH    = 0
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

type RECT struct {
	Left, Top, Right, Bottom int32
}

type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
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
	pGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
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
	pCreateFileW      = kernel32.NewProc("CreateFileW")
	pCloseHandle      = kernel32.NewProc("CloseHandle")

	// GDI text drawing
	pBeginPaint    = user32.NewProc("BeginPaint")
	pEndPaint      = user32.NewProc("EndPaint")
	pGetClientRect = user32.NewProc("GetClientRect")
	pDrawTextW     = user32.NewProc("DrawTextW")
	pCreateFontW   = gdi32.NewProc("CreateFontW")
	pSetTextColor  = gdi32.NewProc("SetTextColor")
	pSetBkMode     = gdi32.NewProc("SetBkMode")
	pSelectObject  = gdi32.NewProc("SelectObject")
	pDeleteObject  = gdi32.NewProc("DeleteObject")
	pUpdateWindow  = user32.NewProc("UpdateWindow")
)

var overlayWndProcCB uintptr

func createOverlayWindow() uintptr {
	hInst, _, _ := pGetModuleHandleW.Call(0)
	logMsg("GetModuleHandle: hInst=0x%X", hInst)

	className, _ := syscall.UTF16PtrFromString("SleepHookOverlay")
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(0) // black
	logMsg("cursor=0x%X brush=0x%X", cursor, brush)

	overlayWndProcCB = syscall.NewCallback(overlayWndProc)

	wc := WNDCLASSEX{
		Size:          uint32(unsafe.Sizeof(WNDCLASSEX{})),
		LpfnWndProc:   overlayWndProcCB,
		HInstance:     hInst,
		HCursor:       cursor,
		HbrBackground: brush,
		LpszClassName: className,
	}
	atom, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	logMsg("RegisterClassEx: atom=%d err=%v", atom, err)

	x, _, _ := pGetSystemMetrics.Call(SM_XVIRTUALSCREEN)
	y, _, _ := pGetSystemMetrics.Call(SM_YVIRTUALSCREEN)
	cx, _, _ := pGetSystemMetrics.Call(SM_CXVIRTUALSCREEN)
	cy, _, _ := pGetSystemMetrics.Call(SM_CYVIRTUALSCREEN)
	logMsg("virtual screen: x=%d y=%d cx=%d cy=%d", x, y, cx, cy)

	if cx == 0 || cy == 0 {
		cx, _, _ = pGetSystemMetrics.Call(SM_CXSCREEN)
		cy, _, _ = pGetSystemMetrics.Call(SM_CYSCREEN)
		logMsg("fallback primary screen: cx=%d cy=%d", cx, cy)
	}

	hwnd, _, err := pCreateWindowExW.Call(
		WS_EX_TOPMOST|WS_EX_LAYERED|WS_EX_TOOLWINDOW,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(className)),
		WS_POPUP,
		x, y, cx, cy,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		logMsg("FATAL: CreateWindowEx failed: %v", err)
		showError("Failed to create overlay window")
		return 0
	}

	ret, _, err := pSetLayeredWindowAttributes.Call(hwnd, 0, 255, LWA_ALPHA)
	logMsg("SetLayeredWindowAttributes: ret=%d err=%v", ret, err)

	return hwnd
}

func overlayWndProc(hwnd uintptr, msg uint32, wparam uintptr, lparam uintptr) uintptr {
	switch msg {
	case WM_PAINT:
		var ps PAINTSTRUCT
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		drawOverlayText(hwnd, hdc)
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case WM_TIMER:
		checkAndToggle()
		return 0
	case WM_CLOSE:
		return 0
	case WM_COMMAND:
		if wparam == 1 {
			logMsg("tray menu: exit selected")
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

func drawOverlayText(hwnd uintptr, hdc uintptr) {
	if gMessage == "" {
		return
	}
	logMsg("drawOverlayText: hdc=0x%X msg=%q", hdc, gMessage)

	fontName, _ := syscall.UTF16PtrFromString("Microsoft YaHei")
	font, _, _ := pCreateFontW.Call(
		uintptr(^uint32(72-1)),
		0, 0, 0,
		FW_BOLD, 0, 0, 0,
		DEFAULT_CHARSET, 0, 0, CLEARTYPE_QUALITY, DEFAULT_PITCH,
		uintptr(unsafe.Pointer(fontName)),
	)
	logMsg("CreateFont: font=0x%X", font)
	defer pDeleteObject.Call(font)

	oldFont, _, _ := pSelectObject.Call(hdc, font)
	defer pSelectObject.Call(hdc, oldFont)

	pSetTextColor.Call(hdc, 0x00C8C8C8)
	pSetBkMode.Call(hdc, TRANSPARENT)

	var rect RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	logMsg("client rect: %d,%d,%d,%d", rect.Left, rect.Top, rect.Right, rect.Bottom)

	text, _ := syscall.UTF16PtrFromString(gMessage)
	ret, _, _ := pDrawTextW.Call(
		hdc,
		uintptr(unsafe.Pointer(text)),
		^uintptr(0),
		uintptr(unsafe.Pointer(&rect)),
		DT_CENTER|DT_VCENTER|DT_SINGLELINE,
	)
	logMsg("DrawText: ret=%d", ret)
}

func showOverlay(hwnd uintptr) {
	hwndTopmost := ^uintptr(0)
	pSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0,
		uintptr(SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW))
	pShowWindow.Call(hwnd, SW_SHOW)
	pUpdateWindow.Call(hwnd) // force immediate WM_PAINT
}

func hideOverlay(hwnd uintptr) {
	pShowWindow.Call(hwnd, SW_HIDE)
}

func showError(text string) {
	t, _ := syscall.UTF16PtrFromString(text)
	title, _ := syscall.UTF16PtrFromString("SleepHook")
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(title)), 0x10)
}
