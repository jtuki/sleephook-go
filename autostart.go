package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	HKEY_CURRENT_USER = 0x80000001

	KEY_ALL_ACCESS = 0xF003F
	KEY_READ       = 0x20019

	REG_SZ = 1

	MF_CHECKED = 0x00000008
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	pRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	pRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValueW = advapi32.NewProc("RegDeleteValueW")
	pRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	pRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

var runKeyStr, _ = syscall.UTF16PtrFromString(`SOFTWARE\Microsoft\Windows\CurrentVersion\Run`)
var valueNameStr, _ = syscall.UTF16PtrFromString("SleepHook")

func isAutoStartEnabled() bool {
	var hKey uintptr
	ret, _, _ := pRegOpenKeyExW.Call(
		HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(runKeyStr)),
		0, KEY_READ,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if ret != 0 {
		return false
	}
	defer pRegCloseKey.Call(hKey)

	var bufSize uint32
	ret, _, _ = pRegQueryValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(valueNameStr)),
		0, 0, 0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	return ret == 0 && bufSize > 0
}

func setAutoStart(enable bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	var hKey uintptr
	ret, _, _ := pRegOpenKeyExW.Call(
		HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(runKeyStr)),
		0, KEY_ALL_ACCESS,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if ret != 0 {
		logMsg("RegOpenKeyEx failed: %d", ret)
		return errReg(ret)
	}
	defer pRegCloseKey.Call(hKey)

	if enable {
		u16, err := syscall.UTF16FromString(`"` + exe + `"`)
		if err != nil {
			return err
		}
		size := uint32(len(u16) * 2)
		ret, _, _ = pRegSetValueExW.Call(
			hKey,
			uintptr(unsafe.Pointer(valueNameStr)),
			0, REG_SZ,
			uintptr(unsafe.Pointer(&u16[0])),
			uintptr(size),
		)
		if ret != 0 {
			logMsg("RegSetValueEx failed: %d", ret)
			return errReg(ret)
		}
		logMsg("autostart enabled: %s", exe)
	} else {
		ret, _, _ = pRegDeleteValueW.Call(
			hKey,
			uintptr(unsafe.Pointer(valueNameStr)),
		)
		if ret != 0 {
			logMsg("RegDeleteValue failed: %d", ret)
			return errReg(ret)
		}
		logMsg("autostart disabled")
	}
	return nil
}

func toggleAutoStart() {
	enabled := isAutoStartEnabled()
	if err := setAutoStart(!enabled); err != nil {
		logMsg("toggle autostart failed: %v", err)
		showError(err.Error())
	}
}

type errReg uintptr

func (e errReg) Error() string { return fmt.Sprintf("registry error %d", e) }
