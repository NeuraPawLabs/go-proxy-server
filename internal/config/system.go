package config

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/apeming/go-proxy-server/internal/models"
)

// System configuration keys
const (
	KeyAutoStart             = "autostart_enabled"
	KeyWebAdminPassword      = "web_admin_password_hash"
	KeyTunnelServerConfig    = "tunnel_server_config"
	KeyTunnelClientConfig    = "tunnel_client_config"
	KeyTunnelServerTLSAssets = "tunnel_server_tls_assets"
	KeyTunnelClientTLSAssets = "tunnel_client_tls_assets"
)

// GetSystemConfig gets a system configuration value
func GetSystemConfig(db *gorm.DB, key string) (string, error) {
	var config models.SystemConfig
	err := db.Where("key = ?", key).First(&config).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil // Not found, return empty string
		}
		return "", err
	}
	return config.Value, nil
}

// SetSystemConfig sets a system configuration value
func SetSystemConfig(db *gorm.DB, key, value string) error {
	var config models.SystemConfig
	err := db.Where("key = ?", key).First(&config).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create new config
		config = models.SystemConfig{
			Key:   key,
			Value: value,
		}
		return db.Create(&config).Error
	} else if err != nil {
		return err
	}

	// Update existing config
	config.Value = value
	return db.Save(&config).Error
}

// SetSystemConfigIfAbsent creates a configuration entry only if the key does not exist yet.
// It returns true when a new row is inserted and false when the key is already present.
func SetSystemConfigIfAbsent(db *gorm.DB, key, value string) (bool, error) {
	config := models.SystemConfig{
		Key:   key,
		Value: value,
	}
	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoNothing: true,
	}).Create(&config)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// DeleteSystemConfig deletes a system configuration
func DeleteSystemConfig(db *gorm.DB, key string) error {
	return db.Unscoped().Where("key = ?", key).Delete(&models.SystemConfig{}).Error
}
