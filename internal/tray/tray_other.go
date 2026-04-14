//go:build !windows
// +build !windows

package tray

import (
	"fmt"

	"gorm.io/gorm"
)

// Start is a stub for non-Windows platforms
func Start(db *gorm.DB, webPort int) error {
	fmt.Println("System tray is only supported on Windows.")
	fmt.Println("Please use the 'web' command to start the web management interface.")
	return fmt.Errorf("system tray is only supported on Windows")
}
