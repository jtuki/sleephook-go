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
	logger  *log.Logger
	logMu   sync.Mutex
)

func initLog() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	path := filepath.Join(filepath.Dir(exe), "sleephook.log")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	logFile = f
	logger = log.New(f, "", 0)
	logMsg("=== SleepHook started %s ===", time.Now().Format("2006-01-02 15:04:05"))
}

func logMsg(format string, args ...interface{}) {
	logMu.Lock()
	defer logMu.Unlock()
	msg := fmt.Sprintf(format, args...)
	if logger != nil {
		logger.Printf("%s %s", time.Now().Format("15:04:05"), msg)
	}
	// Also print to stderr for debugging with console
	fmt.Fprintf(os.Stderr, "%s\n", msg)
}

func closeLog() {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		logMsg("=== SleepHook stopped ===")
		logFile.Close()
	}
}
