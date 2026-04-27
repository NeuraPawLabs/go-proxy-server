package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/constants"
	"github.com/apeming/go-proxy-server/internal/models"
)

// TimeoutConfig defines timeout settings for proxy connections
type TimeoutConfig struct {
	Connect          time.Duration // Connection establishment timeout
	IdleRead         time.Duration // Idle read timeout (no data received)
	IdleWrite        time.Duration // Idle write timeout (no data sent)
	MaxConnectionAge time.Duration // Maximum connection lifetime
	CleanupTimeout   time.Duration // Timeout for graceful connection cleanup
}

// DefaultTimeout provides default timeout values
// - Connect: 30 seconds for establishing connections
// - IdleRead: 300 seconds (5 minutes) for idle read operations
// - IdleWrite: 120 seconds (2 minutes) for idle write operations
// - MaxConnectionAge: 2 hours for maximum connection lifetime
// - CleanupTimeout: 5 seconds for graceful connection cleanup
var DefaultTimeout = TimeoutConfig{
	Connect:          30 * time.Second,
	IdleRead:         300 * time.Second,
	IdleWrite:        120 * time.Second,
	MaxConnectionAge: 2 * time.Hour,
	CleanupTimeout:   5 * time.Second,
}

// Global timeout configuration with thread-safe access
var (
	currentTimeout TimeoutConfig
	timeoutMu      sync.RWMutex
)

func setCurrentTimeout(timeout TimeoutConfig) {
	timeoutMu.Lock()
	defer timeoutMu.Unlock()
	currentTimeout = timeout
}

// GetTimeout returns the current timeout configuration
func GetTimeout() TimeoutConfig {
	timeoutMu.RLock()
	defer timeoutMu.RUnlock()
	return currentTimeout
}

// LoadTimeoutFromDB loads timeout configuration from database
// If not found in database, uses default values and saves them
func LoadTimeoutFromDB(db *gorm.DB) error {
	// Try to load from database
	var configs []models.SystemConfig
	err := db.Where("key IN ?", []string{
		"timeout_connect",
		"timeout_idle_read",
		"timeout_idle_write",
		"timeout_max_connection_age",
		"timeout_cleanup",
	}).Find(&configs).Error
	if err != nil {
		return err
	}

	// Create a map for easy lookup
	configMap := make(map[string]string)
	for _, cfg := range configs {
		configMap[cfg.Key] = cfg.Value
	}

	// Parse timeout values or use defaults
	connectSec := parseTimeoutOrDefault(configMap["timeout_connect"], 30)
	idleReadSec := parseTimeoutOrDefault(configMap["timeout_idle_read"], 300)
	idleWriteSec := parseTimeoutOrDefault(configMap["timeout_idle_write"], 120)
	maxConnectionAgeSec := parseTimeoutOrDefault(configMap["timeout_max_connection_age"], 7200) // 2 hours
	cleanupSec := parseTimeoutOrDefault(configMap["timeout_cleanup"], 5)

	timeout := TimeoutConfig{
		Connect:          time.Duration(connectSec) * time.Second,
		IdleRead:         time.Duration(idleReadSec) * time.Second,
		IdleWrite:        time.Duration(idleWriteSec) * time.Second,
		MaxConnectionAge: time.Duration(maxConnectionAgeSec) * time.Second,
		CleanupTimeout:   time.Duration(cleanupSec) * time.Second,
	}

	// If not found in database, save default values
	if len(configs) < 5 {
		if err := SaveTimeoutToDB(db, timeout); err != nil {
			return err
		}
	}

	setCurrentTimeout(timeout)
	return nil
}

// SaveTimeoutToDB saves timeout configuration to database with transaction
func SaveTimeoutToDB(db *gorm.DB, timeout TimeoutConfig) error {
	configs := []models.SystemConfig{
		{Key: "timeout_connect", Value: fmt.Sprintf("%d", int(timeout.Connect.Seconds()))},
		{Key: "timeout_idle_read", Value: fmt.Sprintf("%d", int(timeout.IdleRead.Seconds()))},
		{Key: "timeout_idle_write", Value: fmt.Sprintf("%d", int(timeout.IdleWrite.Seconds()))},
		{Key: "timeout_max_connection_age", Value: fmt.Sprintf("%d", int(timeout.MaxConnectionAge.Seconds()))},
		{Key: "timeout_cleanup", Value: fmt.Sprintf("%d", int(timeout.CleanupTimeout.Seconds()))},
	}

	// Use transaction to ensure all configs are saved atomically
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, cfg := range configs {
			// Use GORM's Save which will update if exists or create if not
			var existing models.SystemConfig
			err := tx.Where("key = ?", cfg.Key).First(&existing).Error
			if err == gorm.ErrRecordNotFound {
				// Create new record
				if err := tx.Create(&cfg).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else {
				// Update existing record
				existing.Value = cfg.Value
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

// parseTimeoutOrDefault parses timeout string or returns default value
func parseTimeoutOrDefault(value string, defaultValue int) int {
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

// InitTimeout initializes timeout configuration
func InitTimeout(db *gorm.DB) error {
	// Initialize with default values first
	setCurrentTimeout(DefaultTimeout)

	// Try to load from database
	return LoadTimeoutFromDB(db)
}

// UpdateTimeout persists timeout configuration and updates the in-memory copy.
func UpdateTimeout(db *gorm.DB, timeout TimeoutConfig) error {
	if err := SaveTimeoutToDB(db, timeout); err != nil {
		return err
	}
	setCurrentTimeout(timeout)
	return nil
}

// StartTimeoutReloader starts a background goroutine to reload timeout configuration periodically
func StartTimeoutReloader(db *gorm.DB) {
	go func() {
		ticker := time.NewTicker(constants.TimeoutReloadInterval)
		defer ticker.Stop()

		for range ticker.C {
			if err := LoadTimeoutFromDB(db); err != nil {
				// Log error but don't stop the reloader
				// Note: We can't use logger here to avoid circular dependency
				// The error will be logged by the caller if needed
			}
		}
	}()
}

// GetDataDir returns the user data directory for the application
func GetDataDir() (string, error) {
	var dataDir string

	// Get user home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Determine data directory based on OS
	switch runtime.GOOS {
	case "windows":
		// Windows: use %APPDATA%\go-proxy-server or fallback to home directory
		appData := os.Getenv("APPDATA")
		if appData != "" {
			dataDir = filepath.Join(appData, "go-proxy-server")
		} else {
			dataDir = filepath.Join(homeDir, "go-proxy-server")
		}
	case "darwin":
		// macOS: use ~/Library/Application Support/go-proxy-server
		dataDir = filepath.Join(homeDir, "Library", "Application Support", "go-proxy-server")
	default:
		// Linux/Unix: use XDG or ~/.local/share/go-proxy-server
		if os.Getenv("XDG_DATA_HOME") != "" {
			dataDir = filepath.Join(os.Getenv("XDG_DATA_HOME"), "go-proxy-server")
		} else {
			dataDir = filepath.Join(homeDir, ".local", "share", "go-proxy-server")
		}
	}

	// Create directory if it doesn't exist
	err = os.MkdirAll(dataDir, 0755)
	if err != nil {
		return "", err
	}

	return dataDir, nil
}

// ConfigDirForOS returns the user config directory for the application without creating it.
func ConfigDirForOS(goos string) (string, error) {
	switch goos {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, "go-proxy-server"), nil
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, "go-proxy-server"), nil
	case "darwin":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, "Library", "Application Support", "go-proxy-server"), nil
	default:
		if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
			return filepath.Join(xdgConfigHome, "go-proxy-server"), nil
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, ".config", "go-proxy-server"), nil
	}
}

// GetConfigDir returns the user config directory for the application.
func GetConfigDir() (string, error) {
	configDir, err := ConfigDirForOS(runtime.GOOS)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", err
	}

	return configDir, nil
}

// GetDbPath returns the database file path
func GetDbPath() (string, error) {
	dataDir, err := GetDataDir()
	if err != nil {
		return "", fmt.Errorf("failed to get data directory: %v", err)
	}
	return filepath.Join(dataDir, "data.db"), nil
}

// Load initializes the configuration (ensures data directory exists)
func Load() error {
	_, err := GetDataDir()
	if err != nil {
		return fmt.Errorf("failed to initialize data directory: %v", err)
	}
	return nil
}
