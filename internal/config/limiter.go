package config

import (
	"fmt"
	"strconv"
	"sync/atomic"

	"gorm.io/gorm"
)

// System configuration keys for connection limits
const (
	KeyMaxConcurrentConnections      = "max_concurrent_connections"
	KeyMaxConcurrentConnectionsPerIP = "max_concurrent_connections_per_ip"
)

// Default connection limits
const (
	DefaultMaxConcurrentConnections      = 100000
	DefaultMaxConcurrentConnectionsPerIP = 1000
)

// LimiterConfig holds the connection limiter configuration
type LimiterConfig struct {
	MaxConcurrentConnections      int32
	MaxConcurrentConnectionsPerIP int32
}

// Global limiter configuration (thread-safe with atomic operations)
var (
	globalMaxConnections      atomic.Int32
	globalMaxConnectionsPerIP atomic.Int32
)

func init() {
	// Set default values to prevent zero-value issues
	globalMaxConnections.Store(DefaultMaxConcurrentConnections)
	globalMaxConnectionsPerIP.Store(DefaultMaxConcurrentConnectionsPerIP)
}

// InitLimiterConfig initializes the connection limiter configuration from database
func InitLimiterConfig(db *gorm.DB) error {
	// Load max concurrent connections
	maxConnStr, err := GetSystemConfig(db, KeyMaxConcurrentConnections)
	if err != nil {
		return fmt.Errorf("failed to load max concurrent connections: %w", err)
	}

	var maxConn int32
	if maxConnStr == "" {
		// Not configured, use default
		maxConn = DefaultMaxConcurrentConnections
		// Save default to database
		if err := SetSystemConfig(db, KeyMaxConcurrentConnections, strconv.Itoa(int(maxConn))); err != nil {
			return fmt.Errorf("failed to save default max concurrent connections: %w", err)
		}
	} else {
		// Parse from database
		parsed, err := strconv.ParseInt(maxConnStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid max concurrent connections value: %w", err)
		}
		maxConn = int32(parsed)
		// Validate parsed value (0 means unlimited)
		if maxConn < 0 || maxConn > 1000000 {
			return fmt.Errorf("max concurrent connections must be between 0 (unlimited) and 1000000, got %d", maxConn)
		}
	}

	// Load max concurrent connections per IP
	maxConnPerIPStr, err := GetSystemConfig(db, KeyMaxConcurrentConnectionsPerIP)
	if err != nil {
		return fmt.Errorf("failed to load max concurrent connections per IP: %w", err)
	}

	var maxConnPerIP int32
	if maxConnPerIPStr == "" {
		// Not configured, use default
		maxConnPerIP = DefaultMaxConcurrentConnectionsPerIP
		// Save default to database
		if err := SetSystemConfig(db, KeyMaxConcurrentConnectionsPerIP, strconv.Itoa(int(maxConnPerIP))); err != nil {
			return fmt.Errorf("failed to save default max concurrent connections per IP: %w", err)
		}
	} else {
		// Parse from database
		parsed, err := strconv.ParseInt(maxConnPerIPStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid max concurrent connections per IP value: %w", err)
		}
		maxConnPerIP = int32(parsed)
		// Validate parsed value (0 means unlimited)
		if maxConnPerIP < 0 || maxConnPerIP > 100000 {
			return fmt.Errorf("max concurrent connections per IP must be between 0 (unlimited) and 100000, got %d", maxConnPerIP)
		}
	}

	// Set global configuration
	globalMaxConnections.Store(maxConn)
	globalMaxConnectionsPerIP.Store(maxConnPerIP)

	return nil
}

// GetLimiterConfig returns the current connection limiter configuration
func GetLimiterConfig() LimiterConfig {
	return LimiterConfig{
		MaxConcurrentConnections:      globalMaxConnections.Load(),
		MaxConcurrentConnectionsPerIP: globalMaxConnectionsPerIP.Load(),
	}
}

// UpdateLimiterConfig updates the connection limiter configuration
// Note: This only updates the in-memory configuration. Existing limiters need to be recreated.
func UpdateLimiterConfig(db *gorm.DB, maxConn, maxConnPerIP int32) error {
	// Validate values (0 means unlimited)
	if maxConn < 0 || maxConn > 1000000 {
		return fmt.Errorf("max concurrent connections must be between 0 (unlimited) and 1000000")
	}
	if maxConnPerIP < 0 || maxConnPerIP > 100000 {
		return fmt.Errorf("max concurrent connections per IP must be between 0 (unlimited) and 100000")
	}

	// Save to database
	if err := SetSystemConfig(db, KeyMaxConcurrentConnections, strconv.Itoa(int(maxConn))); err != nil {
		return fmt.Errorf("failed to save max concurrent connections: %w", err)
	}
	if err := SetSystemConfig(db, KeyMaxConcurrentConnectionsPerIP, strconv.Itoa(int(maxConnPerIP))); err != nil {
		return fmt.Errorf("failed to save max concurrent connections per IP: %w", err)
	}

	// Update in-memory configuration
	globalMaxConnections.Store(maxConn)
	globalMaxConnectionsPerIP.Store(maxConnPerIP)

	return nil
}
