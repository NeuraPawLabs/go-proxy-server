package models

import (
	"fmt"

	"gorm.io/gorm"
)

func EnsureTunnelConstraints(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		return fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	if err := db.Exec("DROP INDEX IF EXISTS idx_tunnel_routes_assigned_port").Error; err != nil {
		return fmt.Errorf("drop tunnel route assigned port index: %w", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX idx_tunnel_routes_assigned_port
		ON tunnel_routes (protocol, assigned_public_port)
		WHERE assigned_public_port > 0 AND deleted_at IS NULL
	`).Error; err != nil {
		return fmt.Errorf("create tunnel route assigned port index: %w", err)
	}
	return nil
}
