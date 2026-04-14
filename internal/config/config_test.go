package config

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/models"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.SystemConfig{}); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

func TestLoadTimeoutFromDBLoadsExtendedFields(t *testing.T) {
	db := newTestDB(t)

	expected := TimeoutConfig{
		Connect:          11 * time.Second,
		IdleRead:         22 * time.Second,
		IdleWrite:        33 * time.Second,
		MaxConnectionAge: 44 * time.Second,
		CleanupTimeout:   55 * time.Second,
	}
	if err := SaveTimeoutToDB(db, expected); err != nil {
		t.Fatalf("save timeout: %v", err)
	}

	if err := LoadTimeoutFromDB(db); err != nil {
		t.Fatalf("load timeout: %v", err)
	}

	got := GetTimeout()
	if got != expected {
		t.Fatalf("unexpected timeout config: got %+v want %+v", got, expected)
	}
}

func TestSetSystemConfigIfAbsent(t *testing.T) {
	db := newTestDB(t)

	inserted, err := SetSystemConfigIfAbsent(db, KeyWebAdminPassword, "hash-1")
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if !inserted {
		t.Fatal("expected first insert to succeed")
	}

	inserted, err = SetSystemConfigIfAbsent(db, KeyWebAdminPassword, "hash-2")
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if inserted {
		t.Fatal("expected second insert to be ignored")
	}

	value, err := GetSystemConfig(db, KeyWebAdminPassword)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if value != "hash-1" {
		t.Fatalf("unexpected stored value: got %q want %q", value, "hash-1")
	}
}
