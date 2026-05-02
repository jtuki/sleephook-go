package main

import (
	"fmt"
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
	initLog()
	defer closeLog()

	logMsg("initializing...")

	cfgPath := configPath()
	logMsg("config path: %s", cfgPath)

	var err error
	gCfg, err = loadConfig(cfgPath)
	if err != nil {
		logMsg("FATAL: loadConfig failed: %v", err)
		showError(err.Error())
		return
	}
	for i, tr := range gCfg {
		logMsg("  period[%d]: %02d:%02d:%02d - %02d:%02d:%02d",
			i, tr.StartSec/3600, tr.StartSec%3600/60, tr.StartSec%60,
			tr.StopSec/3600, tr.StopSec%3600/60, tr.StopSec%60)
	}
	gLastReload = time.Now()

	gHooks = newHookManager()
	gBlocker = newBlocker()

	logMsg("creating overlay window...")
	gHwnd = createOverlayWindow()
	if gHwnd == 0 {
		logMsg("FATAL: createOverlayWindow returned 0")
		return
	}
	logMsg("overlay window created: hwnd=0x%X", gHwnd)

	logMsg("adding tray icon...")
	addTrayIcon(gHwnd)

	logMsg("starting timer (1s)")
	pSetTimer.Call(gHwnd, 1, 1000, 0)
	defer removeTrayIcon(gHwnd)
	defer pKillTimer.Call(gHwnd, 1)
	defer gHooks.uninstall()
	defer gBlocker.unblock()

	logMsg("entering message loop")
	var m MSG
	for {
		ret, _, _ := pGetMessageW.Call(
			uintptr(unsafe.Pointer(&m)),
			0, 0, 0,
		)
		if int32(ret) <= 0 {
			logMsg("message loop ended (ret=%d)", int32(ret))
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
			logMsg("config reloaded: %d periods", len(gCfg))
		} else if err != nil {
			logMsg("config reload failed: %v (keeping current)", err)
		}
		gLastReload = now
	}

	wantLock := shouldLock(now, gCfg)

	if wantLock && !gLocked {
		logMsg(">>> LOCKING at %s", now.Format("15:04:05"))
		if err := gHooks.install(); err != nil {
			logMsg("ERROR: hook install failed: %v", err)
		}
		showOverlay(gHwnd)
		gBlocker.block()
		gLocked = true
		updateTrayTooltip(gHwnd, "SleepHook - 锁定中")
	} else if !wantLock && gLocked {
		logMsg("<<< UNLOCKING at %s", now.Format("15:04:05"))
		gHooks.uninstall()
		hideOverlay(gHwnd)
		gBlocker.unblock()
		gLocked = false
		updateTrayTooltip(gHwnd, "SleepHook - 运行中")
	}
}

func logLockedStatus() {
	now := time.Now()
	wantLock := shouldLock(now, gCfg)
	logMsg(fmt.Sprintf("time=%s locked=%v wantLock=%v", now.Format("15:04:05"), gLocked, wantLock))
}
