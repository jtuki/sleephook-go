package main

import (
	"runtime"
	"time"
	"unsafe"
)

var (
	gCfg        []TimeRange
	gHooks      *hookManager
	gBlocker    *taskmgrBlocker
	gHwnd       uintptr
	gLocked     bool
	gLastReload time.Time
)

func main() {
	runtime.LockOSThread()

	var err error
	gCfg, err = loadConfig(configPath())
	if err != nil {
		showError(err.Error())
		return
	}
	gLastReload = time.Now()

	gHooks = newHookManager()
	gBlocker = newBlocker()
	gHwnd = createOverlayWindow()
	if gHwnd == 0 {
		return
	}

	addTrayIcon(gHwnd)
	pSetTimer.Call(gHwnd, 1, 1000, 0)
	defer removeTrayIcon(gHwnd)
	defer pKillTimer.Call(gHwnd, 1)
	defer gHooks.uninstall()
	defer gBlocker.unblock()

	// Windows message loop — required for hook callbacks and timer
	var m MSG
	for {
		ret, _, _ := pGetMessageW.Call(
			uintptr(unsafe.Pointer(&m)),
			0, 0, 0,
		)
		if int32(ret) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func checkAndToggle() {
	now := time.Now()

	// Reload config every 60 seconds; on failure keep current config
	if now.Sub(gLastReload) >= 60*time.Second {
		if cfg, err := loadConfig(configPath()); err == nil && len(cfg) > 0 {
			gCfg = cfg
		}
		gLastReload = now
	}

	wantLock := shouldLock(now, gCfg)

	if wantLock && !gLocked {
		gHooks.install()
		showOverlay(gHwnd)
		gBlocker.block()
		gLocked = true
		updateTrayTooltip(gHwnd, "SleepHook - 锁定中")
	} else if !wantLock && gLocked {
		gHooks.uninstall()
		hideOverlay(gHwnd)
		gBlocker.unblock()
		gLocked = false
		updateTrayTooltip(gHwnd, "SleepHook - 运行中")
	}
}
