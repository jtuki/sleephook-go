package main

import (
	"fmt"
	"os"
	"runtime"
	"time"
	"unsafe"
)

var (
	gCfg          []TimeRange
	gMessage      string
	gHooks        *hookManager
	gBlocker      *taskmgrBlocker
	gNetGuard     *networkGuard
	gHwnd         uintptr
	gLocked       bool
	gLastReload   time.Time
	gSpeedVal     int
	gOpacityVal   int
	gNetworkCfg   networkCheckConfig
	gExtendUntil  time.Time
	gLastUITick   time.Time
	gLastLockTick time.Time
)

const (
	mainTimerIntervalMS  = 200
	uiTickInterval       = time.Second
	lockTickInterval     = time.Second
	configReloadInterval = time.Minute
)

func main() {
	runtime.LockOSThread()
	initLog()
	defer closeLog()
	defer recoverPanic()

	exe, _ := os.Executable()
	logMsg("exe: %s", exe)

	cfgPath := configPath()
	logMsg("config path: %s", cfgPath)

	var err error
	gCfg, gMessage, gSpeedVal, gOpacityVal, gNetworkCfg, err = loadConfig(cfgPath)
	if err != nil {
		logMsg("FATAL: loadConfig: %v", err)
		showError(err.Error())
		return
	}
	for i, tr := range gCfg {
		kind := "normal"
		hours := (tr.StopSec - tr.StartSec + 86400) % 86400 / 3600
		if tr.StartSec > tr.StopSec {
			kind = "overnight"
		}
		logMsg("  period[%d]: %02d:%02d-%02d:%02d (%s, ~%dh)",
			i, tr.StartSec/3600, tr.StartSec%3600/60,
			tr.StopSec/3600, tr.StopSec%3600/60,
			kind, hours)
	}
	logMsg("message: %s speed: %d opacity: %d network_check=%v allowed=%v providers=%v force_times=%v",
		gMessage, gSpeedVal, gOpacityVal, gNetworkCfg.Enabled, gNetworkCfg.AllowedCountries,
		gNetworkCfg.Providers, gNetworkCfg.ForceDisconnectTimes)
	gLastReload = time.Now()
	gOpacity = byte(gOpacityVal)

	gHooks = newHookManager()
	gBlocker = newBlocker()
	gNetGuard = newNetworkGuard(gNetworkCfg)

	logMsg("calling createOverlayWindows...")
	gHwnd = createOverlayWindows()
	if gHwnd == 0 {
		logMsg("FATAL: createOverlayWindows returned 0")
		return
	}
	logMsg("overlay window OK: hwnd=0x%X", gHwnd)

	logMsg("calling addTrayIcon...")
	addTrayIcon(gHwnd)

	logMsg("starting timer")
	pSetTimer.Call(gHwnd, 1, mainTimerIntervalMS, 0)
	defer removeTrayIcon(gHwnd)
	defer stopWebUI()
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

func extendLock(d time.Duration) {
	gExtendUntil = time.Now().Add(d)
	logMsg("extend: skip locking until %s", gExtendUntil.Format("15:04:05"))
	// If currently locked, unlock immediately
	if gLocked {
		pKillTimer.Call(gHwnd, 2)
		gHooks.uninstall()
		hideOverlay(gHwnd)
		gBlocker.unblock()
		gLocked = false
	}
	updateTooltip()
}

func updateTooltip() {
	now := time.Now()
	if gExtendUntil.After(now) {
		remaining := time.Until(gExtendUntil).Truncate(time.Second)
		updateTrayTooltip(gHwnd, fmt.Sprintf("SleepHook - 延长中 (剩余 %s)", remaining))
		return
	}
	if gNetGuard != nil {
		if remaining := gNetGuard.forceRemaining(now); remaining > 0 {
			updateTrayTooltip(gHwnd, fmt.Sprintf("SleepHook - 主动屏蔽网络中 (剩余 %s)", remaining))
			return
		}
		if remaining := gNetGuard.skipRemaining(now); remaining > 0 {
			updateTrayTooltip(gHwnd, fmt.Sprintf("SleepHook - 网络检查已屏蔽 (剩余 %s)", remaining))
			return
		}
	}
	if gLocked {
		updateTrayTooltip(gHwnd, "SleepHook - 锁定中")
	} else {
		updateTrayTooltip(gHwnd, "SleepHook - 运行中")
	}
}

func checkAndToggle() {
	now := time.Now()

	if gNetGuard != nil {
		gNetGuard.tick(now)
	}

	uiDue := gLastUITick.IsZero() || now.Sub(gLastUITick) >= uiTickInterval
	if uiDue {
		gLastUITick = now
		if gExtendUntil.After(now) {
			updateTooltip()
		} else if !gExtendUntil.IsZero() && !gLocked {
			gExtendUntil = time.Time{}
			updateTooltip()
		}
	}

	if now.Sub(gLastReload) >= configReloadInterval {
		if cfg, msg, speed, opacity, netCfg, err := loadConfig(configPath()); err == nil && len(cfg) > 0 {
			gCfg = cfg
			gMessage = msg
			gSpeedVal = speed
			gSpeed = int32(speed)
			gOpacityVal = opacity
			gOpacity = byte(opacity)
			gNetworkCfg = netCfg
			if gNetGuard != nil {
				gNetGuard.configure(netCfg)
			}
			logMsg("config reloaded: %d periods, message=%q speed=%d opacity=%d network_check=%v allowed=%v providers=%v force_times=%v",
				len(gCfg), gMessage, speed, opacity, netCfg.Enabled, netCfg.AllowedCountries,
				netCfg.Providers, netCfg.ForceDisconnectTimes)
		} else if err != nil {
			logMsg("config reload failed: %v", err)
		}
		gLastReload = now
	}

	if !gLastLockTick.IsZero() && now.Sub(gLastLockTick) < lockTickInterval {
		return
	}
	gLastLockTick = now

	wantLock := shouldLock(now, gCfg)

	// Skip locking during extension period
	if wantLock && gExtendUntil.After(now) {
		logMsg("extend active, skipping lock (until %s)", gExtendUntil.Format("15:04:05"))
		wantLock = false
	}

	if wantLock && !gLocked {
		logMsg(">>> LOCKING at %s (extendUntil=%s)", now.Format("15:04:05"), gExtendUntil.Format("15:04:05"))
		if err := gHooks.install(); err != nil {
			logMsg("ERROR: hook install: %v", err)
		}
		gSpeed = int32(gSpeedVal)
		gOpacity = byte(gOpacityVal)
		gTextMeasured = false
		showOverlay(gHwnd)
		pSetTimer.Call(gHwnd, 2, 50, 0)
		gBlocker.block()
		gLocked = true
		updateTrayTooltip(gHwnd, "SleepHook - 锁定中")
	} else if !wantLock && gLocked {
		logMsg("<<< UNLOCKING at %s", now.Format("15:04:05"))
		pKillTimer.Call(gHwnd, 2)
		gHooks.uninstall()
		hideOverlay(gHwnd)
		gBlocker.unblock()
		gLocked = false
		updateTooltip()
	}
}
