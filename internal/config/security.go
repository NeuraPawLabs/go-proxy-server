package config

import (
	"fmt"
	"strconv"
	"sync/atomic"

	"gorm.io/gorm"
)

// System configuration keys for security settings
const (
	KeyAllowPrivateIPAccess = "security_allow_private_ip_access"
)

// Default security settings
const (
	DefaultAllowPrivateIPAccess = false
)

// Global security configuration (thread-safe with atomic operations)
var (
	globalAllowPrivateIPAccess atomic.Bool
)

func init() {
	// Set default value to prevent zero-value issues
	globalAllowPrivateIPAccess.Store(DefaultAllowPrivateIPAccess)
}

// InitSecurityConfig initializes the security configuration from database
func InitSecurityConfig(db *gorm.DB) error {
	// Load allow private IP access setting
	allowStr, err := GetSystemConfig(db, KeyAllowPrivateIPAccess)
	if err != nil {
		return fmt.Errorf("failed to load allow private IP access setting: %w", err)
	}

	var allow bool
	if allowStr == "" {
		// Not configured, use default
		allow = DefaultAllowPrivateIPAccess
		// Save default to database
		if err := SetSystemConfig(db, KeyAllowPrivateIPAccess, strconv.FormatBool(allow)); err != nil {
			return fmt.Errorf("failed to save default allow private IP access setting: %w", err)
		}
	} else {
		// Parse from database
		parsed, err := strconv.ParseBool(allowStr)
		if err != nil {
			return fmt.Errorf("invalid allow private IP access value: %w", err)
		}
		allow = parsed
	}

	// Set global configuration
	globalAllowPrivateIPAccess.Store(allow)

	return nil
}

// GetAllowPrivateIPAccess returns whether private IP access is allowed
// This function is lock-free and safe for concurrent use
func GetAllowPrivateIPAccess() bool {
	return globalAllowPrivateIPAccess.Load()
}

// UpdateAllowPrivateIPAccess updates the allow private IP access setting
// This updates both the database and in-memory configuration
func UpdateAllowPrivateIPAccess(db *gorm.DB, allow bool) error {
	// Save to database
	if err := SetSystemConfig(db, KeyAllowPrivateIPAccess, strconv.FormatBool(allow)); err != nil {
		return fmt.Errorf("failed to save allow private IP access setting: %w", err)
	}

	// Update in-memory configuration
	globalAllowPrivateIPAccess.Store(allow)

	return nil
}
