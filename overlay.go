package main

import (
	"math/rand"
	"syscall"
	"time"
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

	MB_YESNO         = 0x00000004
	MB_ICONWARNING   = 0x00000030
	MB_SYSTEMMODAL   = 0x00001000
	MB_SETFOREGROUND = 0x00010000
	MB_TOPMOST       = 0x00040000
	IDNO             = 7

	GENERIC_READ  = 0x80000000
	OPEN_EXISTING = 3

	MONITORINFOF_PRIMARY = 0x00000001

	// GDI text drawing
	TRANSPARENT       = 1
	DT_LEFT           = 0x00000000
	DT_TOP            = 0x00000000
	DT_SINGLELINE     = 0x00000020
	DEFAULT_CHARSET   = 1
	FW_BOLD           = 700
	CLEARTYPE_QUALITY = 5
	DEFAULT_PITCH     = 0
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

type SIZE struct {
	CX, CY int32
}

type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type MONITORINFO struct {
	Size   uint32
	Rect   RECT
	RcWork RECT
	Flags  uint32
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
	pMessageBoxTimeoutW         = user32.NewProc("MessageBoxTimeoutW")

	pSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx      = user32.NewProc("CallNextHookEx")

	pCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	pCreateFileW      = kernel32.NewProc("CreateFileW")
	pCloseHandle      = kernel32.NewProc("CloseHandle")

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

	pGetTextExtentPoint32W = gdi32.NewProc("GetTextExtentPoint32W")
	pInvalidateRect        = user32.NewProc("InvalidateRect")
	pFillRect              = user32.NewProc("FillRect")

	pEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	pGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
)

var overlayWndProcCB uintptr

var (
	gTextX, gTextY     int32
	gTextDX, gTextDY   int32
	gSpeed             int32 = 2
	gOpacity           byte  = 240
	gTextW, gTextH     int32
	gScreenW, gScreenH int32
	gTextMeasured      bool
)

// Per-monitor overlay windows
var gOverlays []uintptr

// Collected during EnumDisplayMonitors callback
var gEnumMonitors []struct {
	Rect    RECT
	Primary bool
}

func monitorEnumProc(hMonitor uintptr, hdc uintptr, lprc uintptr, lParam uintptr) uintptr {
	var mi MONITORINFO
	mi.Size = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&mi)))
	gEnumMonitors = append(gEnumMonitors, struct {
		Rect    RECT
		Primary bool
	}{mi.Rect, mi.Flags&MONITORINFOF_PRIMARY != 0})
	return 1
}

func createOverlayWindows() uintptr {
	hInst, _, _ := pGetModuleHandleW.Call(0)
	logMsg("GetModuleHandle: hInst=0x%X", hInst)

	className, _ := syscall.UTF16PtrFromString("SleepHookOverlay")
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(0)
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

	gEnumMonitors = nil
	enumCB := syscall.NewCallback(monitorEnumProc)
	ret, _, _ := pEnumDisplayMonitors.Call(0, 0, enumCB, 0)
	logMsg("EnumDisplayMonitors: ret=%d count=%d", ret, len(gEnumMonitors))

	if len(gEnumMonitors) == 0 {
		// Fallback: single window covering virtual screen
		logMsg("no monitors enumerated, falling back to virtual screen")
		return createFallbackWindow(hInst, className)
	}

	var primaryHwnd uintptr

	for i, mr := range gEnumMonitors {
		w := mr.Rect.Right - mr.Rect.Left
		h := mr.Rect.Bottom - mr.Rect.Top
		logMsg("monitor[%d]: %d,%d %dx%d primary=%v", i,
			mr.Rect.Left, mr.Rect.Top, w, h, mr.Primary)

		hwnd, _, createErr := pCreateWindowExW.Call(
			WS_EX_TOPMOST|WS_EX_LAYERED|WS_EX_TOOLWINDOW,
			uintptr(unsafe.Pointer(className)),
			uintptr(unsafe.Pointer(className)),
			WS_POPUP,
			uintptr(mr.Rect.Left), uintptr(mr.Rect.Top),
			uintptr(w), uintptr(h),
			0, 0, hInst, 0,
		)
		if hwnd == 0 {
			logMsg("FATAL: CreateWindowEx failed for monitor %d: %v", i, createErr)
			continue
		}

		pSetLayeredWindowAttributes.Call(hwnd, 0, uintptr(gOpacity), LWA_ALPHA)
		gOverlays = append(gOverlays, hwnd)
		logMsg("overlay[%d] hwnd=0x%X", i, hwnd)

		if mr.Primary {
			primaryHwnd = hwnd
		}
	}

	if primaryHwnd == 0 && len(gOverlays) > 0 {
		primaryHwnd = gOverlays[0]
	}

	if primaryHwnd == 0 {
		showError("Failed to create overlay windows")
	}

	return primaryHwnd
}

func createFallbackWindow(hInst uintptr, className *uint16) uintptr {
	x, _, _ := pGetSystemMetrics.Call(SM_XVIRTUALSCREEN)
	y, _, _ := pGetSystemMetrics.Call(SM_YVIRTUALSCREEN)
	cx, _, _ := pGetSystemMetrics.Call(SM_CXVIRTUALSCREEN)
	cy, _, _ := pGetSystemMetrics.Call(SM_CYVIRTUALSCREEN)
	if cx == 0 || cy == 0 {
		cx, _, _ = pGetSystemMetrics.Call(SM_CXSCREEN)
		cy, _, _ = pGetSystemMetrics.Call(SM_CYSCREEN)
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
		logMsg("FATAL: fallback CreateWindowEx failed: %v", err)
		showError("Failed to create overlay window")
		return 0
	}
	pSetLayeredWindowAttributes.Call(hwnd, 0, uintptr(gOpacity), LWA_ALPHA)
	gOverlays = append(gOverlays, hwnd)
	return hwnd
}

func overlayWndProc(hwnd uintptr, msg uint32, wparam uintptr, lparam uintptr) uintptr {
	switch msg {
	case WM_PAINT:
		var ps PAINTSTRUCT
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if hwnd == gHwnd {
			drawOverlayText(hwnd, hdc)
		}
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case WM_TIMER:
		if wparam == 2 {
			animateText(hwnd)
			return 0
		}
		checkAndToggle()
		return 0
	case WM_CLOSE:
		return 0
	case WM_COMMAND:
		if wparam == 1 {
			logMsg("tray menu: exit selected")
			pPostQuitMessage.Call(0)
			return 0
		} else if wparam == 2 {
			extendLock(10 * time.Minute)
			return 0
		} else if wparam == 3 {
			toggleAutoStart()
			return 0
		} else if wparam == 4 {
			openConfigUI()
			return 0
		} else if wparam == 5 {
			if gNetGuard != nil {
				gNetGuard.skip(3 * time.Minute)
			}
			return 0
		} else if wparam == 6 {
			if gNetGuard != nil {
				gNetGuard.skip(5 * time.Minute)
			}
			return 0
		} else if wparam == 7 {
			if gNetGuard != nil {
				gNetGuard.skip(10 * time.Minute)
			}
			return 0
		} else if wparam == 9 {
			if gNetGuard != nil {
				gNetGuard.cancelActiveBlock()
			}
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

	fontName, _ := syscall.UTF16PtrFromString("Microsoft YaHei")
	font, _, _ := pCreateFontW.Call(
		uintptr(^uint32(72-1)),
		0, 0, 0,
		FW_BOLD, 0, 0, 0,
		DEFAULT_CHARSET, 0, 0, CLEARTYPE_QUALITY, DEFAULT_PITCH,
		uintptr(unsafe.Pointer(fontName)),
	)
	defer pDeleteObject.Call(font)

	oldFont, _, _ := pSelectObject.Call(hdc, font)
	defer pSelectObject.Call(hdc, oldFont)

	pSetTextColor.Call(hdc, 0x00C8C8C8)
	pSetBkMode.Call(hdc, TRANSPARENT)

	if !gTextMeasured {
		text, _ := syscall.UTF16PtrFromString(gMessage)
		var sz SIZE
		pGetTextExtentPoint32W.Call(hdc,
			uintptr(unsafe.Pointer(text)),
			uintptr(len([]rune(gMessage))),
			uintptr(unsafe.Pointer(&sz)),
		)
		gTextW = sz.CX
		gTextH = sz.CY
		var cr RECT
		pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
		gScreenW = cr.Right
		gScreenH = cr.Bottom
		gTextX = (gScreenW - gTextW) / 2
		gTextY = (gScreenH - gTextH) / 2
		gTextDX = gSpeed
		gTextDY = gSpeed
		gTextMeasured = true
		logMsg("text measured: %dx%d client: %dx%d pos: %d,%d",
			gTextW, gTextH, gScreenW, gScreenH, gTextX, gTextY)
	}

	rect := RECT{Left: gTextX, Top: gTextY, Right: gTextX + gTextW, Bottom: gTextY + gTextH}
	text, _ := syscall.UTF16PtrFromString(gMessage)
	pDrawTextW.Call(
		hdc,
		uintptr(unsafe.Pointer(text)),
		^uintptr(0),
		uintptr(unsafe.Pointer(&rect)),
		DT_LEFT|DT_TOP|DT_SINGLELINE,
	)
}

func animateText(hwnd uintptr) {
	if !gTextMeasured || gScreenW == 0 || gScreenH == 0 {
		return
	}
	gTextX += gTextDX
	gTextY += gTextDY
	if gTextX < 0 {
		gTextX = 0
		gTextDX = gSpeed
		perturbBounce()
	} else if gTextX+gTextW > gScreenW {
		gTextX = gScreenW - gTextW
		gTextDX = -gSpeed
		perturbBounce()
	}
	if gTextY < 0 {
		gTextY = 0
		gTextDY = gSpeed
		perturbBounce()
	} else if gTextY+gTextH > gScreenH {
		gTextY = gScreenH - gTextH
		gTextDY = -gSpeed
		perturbBounce()
	}
	pInvalidateRect.Call(hwnd, 0, 1)
}

func perturbBounce() {
	dxSign, dySign := int32(1), int32(1)
	if gTextDX < 0 {
		dxSign = -1
	}
	if gTextDY < 0 {
		dySign = -1
	}
	dxMag := gSpeed + int32(rand.Intn(3)) - 1
	dyMag := gSpeed + int32(rand.Intn(3)) - 1
	if dxMag < 1 {
		dxMag = 1
	}
	if dyMag < 1 {
		dyMag = 1
	}
	gTextDX = dxSign * dxMag
	gTextDY = dySign * dyMag
}

func showOverlay(hwnd uintptr) {
	hwndTopmost := ^uintptr(0)
	for _, h := range gOverlays {
		pSetLayeredWindowAttributes.Call(h, 0, uintptr(gOpacity), LWA_ALPHA)
		pSetWindowPos.Call(h, hwndTopmost, 0, 0, 0, 0,
			uintptr(SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW))
		pShowWindow.Call(h, SW_SHOW)
		pUpdateWindow.Call(h)
	}
}

func hideOverlay(hwnd uintptr) {
	for _, h := range gOverlays {
		pShowWindow.Call(h, SW_HIDE)
	}
}

func showError(text string) {
	t, _ := syscall.UTF16PtrFromString(text)
	title, _ := syscall.UTF16PtrFromString("SleepHook")
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(title)), 0x10)
}

func confirmScheduledDisconnect(timeout time.Duration) bool {
	text := "已到定点网络处置时刻。\n\n选择“是”：立即执行配置的网络处置动作，并在接下来的 120 分钟持续执行。\n选择“否”：我会手动处理，本次不再强制。\n\n30 秒无操作将自动执行。"
	t, _ := syscall.UTF16PtrFromString(text)
	title, _ := syscall.UTF16PtrFromString("SleepHook 定点网络处置")
	flags := uintptr(MB_YESNO | MB_ICONWARNING | MB_SYSTEMMODAL | MB_SETFOREGROUND | MB_TOPMOST)
	ret, _, err := pMessageBoxTimeoutW.Call(
		0,
		uintptr(unsafe.Pointer(t)),
		uintptr(unsafe.Pointer(title)),
		flags,
		0,
		uintptr(timeout/time.Millisecond),
	)
	if ret == 0 {
		logMsg("scheduled network action prompt failed or timed out without result: %v", err)
		return true
	}
	return ret != IDNO
}
