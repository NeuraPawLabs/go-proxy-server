//go:build !windows
// +build !windows

package singleinstance

// Check always returns true on non-Windows platforms (no single instance enforcement)
func Check(mutexName string) (bool, error) {
	return true, nil
}

// Release does nothing on non-Windows platforms
func Release() {
	// No-op on non-Windows platforms
}
