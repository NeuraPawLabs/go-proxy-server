package main

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/models"
)

func migrateDatabase(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	// SQLite migrator cannot parse the partial index SQL we create for tunnel_routes
	// on subsequent AutoMigrate runs. Drop it before migration and recreate it after.
	if err := db.Exec("DROP INDEX IF EXISTS idx_tunnel_routes_assigned_port").Error; err != nil {
		return fmt.Errorf("drop tunnel route assigned port index before migration: %w", err)
	}

	if err := db.AutoMigrate(
		&models.User{},
		&models.Whitelist{},
		&models.ProxyConfig{},
		&models.SystemConfig{},
		&models.TunnelClient{},
		&models.TunnelRoute{},
		&models.MetricsSnapshot{},
		&models.AlertConfig{},
		&models.AlertHistory{},
		&models.AuditLog{},
		&models.EventLog{},
	); err != nil {
		return err
	}

	return models.EnsureTunnelConstraints(db)
}
