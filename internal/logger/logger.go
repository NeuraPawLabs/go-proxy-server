package logger

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/apeming/go-proxy-server/internal/config"
)

// LogLevel represents the logging level
type LogLevel int32

const (
	// LevelDebug logs everything including debug messages
	LevelDebug LogLevel = iota
	// LevelInfo logs info, warn, and error messages (default)
	LevelInfo
	// LevelWarn logs warn and error messages only
	LevelWarn
	// LevelError logs error messages only
	LevelError
	// LevelNone disables all logging
	LevelNone
)

var (
	logFile     *os.File
	infoLogger  *log.Logger
	warnLogger  *log.Logger
	errLogger   *log.Logger
	debugLogger *log.Logger
	// Use atomic for thread-safe level changes
	currentLevel atomic.Int32
)

func init() {
	// Default to Info level
	currentLevel.Store(int32(LevelInfo))
}

// Init initializes logging to file and mirrors logs to stdout when available.
func Init() error {
	// Get data directory
	dataDir, err := config.GetDataDir()
	if err != nil {
		return err
	}

	// Create log file
	logPath := filepath.Join(dataDir, "app.log")
	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}

	// Write to the log file first, then mirror to stdout for CLI runs.
	// In GUI mode stdout may be discarded, but file logging still works.
	initLoggers(io.MultiWriter(logFile, os.Stdout))

	Debug("=== Go Proxy Server Started ===")
	Debug("Log file: %s", logPath)
	Debug("Working directory: %s", func() string {
		wd, _ := os.Getwd()
		return wd
	}())
	Debug("Executable: %s", os.Args[0])

	return nil
}

// InitStdout initializes logging to stdout (for CLI mode)
func InitStdout() {
	initLoggers(os.Stdout)
}

// initLoggers initializes all loggers with the given output
func initLoggers(output io.Writer) {
	flags := log.LstdFlags // Include timestamp
	debugLogger = log.New(output, "[DEBUG] ", flags)
	infoLogger = log.New(output, "[INFO] ", flags)
	warnLogger = log.New(output, "[WARN] ", flags)
	errLogger = log.New(output, "[ERROR] ", flags)
}

// SetLevel sets the current logging level (thread-safe)
func SetLevel(level LogLevel) {
	currentLevel.Store(int32(level))
}

// GetLevel returns the current logging level (thread-safe)
func GetLevel() LogLevel {
	return LogLevel(currentLevel.Load())
}

// Close closes the log file
func Close() {
	if logFile != nil {
		Debug("=== Go Proxy Server Stopped ===")
		logFile.Close()
	}
}

// Debug logs a debug message (only if level is Debug)
func Debug(format string, v ...interface{}) {
	if GetLevel() > LevelDebug {
		return
	}
	if debugLogger == nil {
		InitStdout()
	}
	debugLogger.Printf(format, v...)
}

// Info logs an info message (only if level is Info or lower)
func Info(format string, v ...interface{}) {
	if GetLevel() > LevelInfo {
		return
	}
	if infoLogger == nil {
		InitStdout()
	}
	infoLogger.Printf(format, v...)
}

// Warn logs a warning message (only if level is Warn or lower)
func Warn(format string, v ...interface{}) {
	if GetLevel() > LevelWarn {
		return
	}
	if warnLogger == nil {
		InitStdout()
	}
	warnLogger.Printf(format, v...)
}

// Error logs an error message (only if level is Error or lower)
func Error(format string, v ...interface{}) {
	if GetLevel() > LevelError {
		return
	}
	if errLogger == nil {
		InitStdout()
	}
	errLogger.Printf(format, v...)
}
