//go:build !windows
// +build !windows

package autostart

import "errors"

// IsEnabled checks if autostart is enabled (not supported on non-Windows)
func IsEnabled() (bool, error) {
	return false, errors.New("autostart is only supported on Windows")
}

// Enable enables autostart (not supported on non-Windows)
func Enable() error {
	return errors.New("autostart is only supported on Windows")
}

// Disable disables autostart (not supported on non-Windows)
func Disable() error {
	return errors.New("autostart is only supported on Windows")
}
