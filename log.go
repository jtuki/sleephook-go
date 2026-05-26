package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logFile *os.File
	logMu   sync.Mutex
)

func initLog() {
	exe, err := os.Executable()
	if err == nil {
		path := filepath.Join(filepath.Dir(exe), "sleephook.log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			logFile = f
		}
	}
	if logFile == nil {
		// fallback: try current directory
		f, err := os.OpenFile("sleephook.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			logFile = f
		}
	}
	if logFile != nil {
		logger = log.New(logFile, "", 0)
	}
	writeLog("=== SleepHook started %s ===", time.Now().Format("2006-01-02 15:04:05"))
}

var logger *log.Logger

func logMsg(format string, args ...interface{}) {
	writeLog(format, args...)
}

func writeLog(format string, args ...interface{}) {
	logMu.Lock()
	defer logMu.Unlock()
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %s\n", time.Now().Format("15:04:05.000"), msg)
	if logFile != nil {
		logFile.WriteString(line)
		logFile.Sync()
	}
	// Also write to stderr for console debugging
	os.Stderr.WriteString(line)
}

func closeLog() {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		line := fmt.Sprintf("%s === SleepHook stopped ===\n", time.Now().Format("15:04:05.000"))
		logFile.WriteString(line)
		logFile.Sync()
		os.Stderr.WriteString(line)
		logFile.Close()
		logFile = nil
	}
}

func recoverPanic() {
	if r := recover(); r != nil {
		writeLog("PANIC: %v", r)
		closeLog()
		showError(fmt.Sprintf("SleepHook crashed: %v", r))
	}
}
