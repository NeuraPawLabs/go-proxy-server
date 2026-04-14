package config

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/models"
)

// LoadProxyConfig loads proxy configuration from database by type
func LoadProxyConfig(db *gorm.DB, proxyType string) (*models.ProxyConfig, error) {
	var config models.ProxyConfig
	err := db.Where("type = ?", proxyType).First(&config).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // No config found, not an error
		}
		return nil, err
	}
	return &config, nil
}

// SaveProxyConfig saves proxy configuration to database
func SaveProxyConfig(db *gorm.DB, config *models.ProxyConfig) error {
	if config.Type != "socks5" && config.Type != "http" {
		return fmt.Errorf("invalid proxy type: %s", config.Type)
	}

	// Check if config already exists
	var existing models.ProxyConfig
	err := db.Where("type = ?", config.Type).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create new config
		return db.Create(config).Error
	} else if err != nil {
		return err
	}

	// Update existing config
	config.ID = existing.ID
	return db.Save(config).Error
}

// DeleteProxyConfig deletes proxy configuration from database
func DeleteProxyConfig(db *gorm.DB, proxyType string) error {
	// Use Unscoped to permanently delete the record (hard delete)
	return db.Unscoped().Where("type = ?", proxyType).Delete(&models.ProxyConfig{}).Error
}

// UpdateProxyAutoStart updates only the AutoStart field for a proxy type
func UpdateProxyAutoStart(db *gorm.DB, proxyType string, autoStart bool) error {
	return db.Model(&models.ProxyConfig{}).Where("type = ?", proxyType).Update("auto_start", autoStart).Error
}
