//go:build windows
// +build windows

package singleinstance

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	createMutexW         = kernel32.NewProc("CreateMutexW")
	releaseMutex         = kernel32.NewProc("ReleaseMutex")
	closeHandle          = kernel32.NewProc("CloseHandle")
	ERROR_ALREADY_EXISTS = syscall.Errno(183)
)

var globalMutex syscall.Handle

// Check checks if another instance is already running using Windows Mutex
// Returns true if this is the only instance, false if another instance exists
func Check(mutexName string) (bool, error) {
	mutexNamePtr, err := syscall.UTF16PtrFromString(mutexName)
	if err != nil {
		return false, fmt.Errorf("failed to convert mutex name: %w", err)
	}

	// Create a named mutex (not initially owned)
	handle, _, lastErr := createMutexW.Call(
		0,                                     // default security attributes
		0,                                     // not initially owned
		uintptr(unsafe.Pointer(mutexNamePtr)), // mutex name
	)

	if handle == 0 {
		return false, fmt.Errorf("failed to create mutex: %v", lastErr)
	}

	// Check if mutex already exists
	// If ERROR_ALREADY_EXISTS, another instance is running
	if lastErr == ERROR_ALREADY_EXISTS {
		closeHandle.Call(handle)
		return false, nil
	}

	// Store handle for cleanup
	globalMutex = syscall.Handle(handle)
	return true, nil
}

// Release releases the mutex
func Release() {
	if globalMutex != 0 {
		releaseMutex.Call(uintptr(globalMutex))
		closeHandle.Call(uintptr(globalMutex))
		globalMutex = 0
	}
}
