package main

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCurrentMigrationCanRunTwiceOnFreshDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "repeat.db")

	openDB := func() *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		return db
	}

	if err := migrateDatabase(openDB()); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	if err := migrateDatabase(openDB()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
