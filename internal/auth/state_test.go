package auth

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

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
	if err := db.AutoMigrate(&models.User{}, &models.Whitelist{}); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

func TestSyncStateReloadsCredentialsAndWhitelist(t *testing.T) {
	db := newTestDB(t)

	hash, err := HashPassword([]byte("secret123"))
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := db.Create(&models.User{Username: "alice", Password: hash}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&models.Whitelist{IP: "192.0.2.10"}).Error; err != nil {
		t.Fatalf("create whitelist: %v", err)
	}

	if err := SyncState(db); err != nil {
		t.Fatalf("sync state: %v", err)
	}

	if err := VerifyCredentials("alice", []byte("secret123")); err != nil {
		t.Fatalf("verify credentials: %v", err)
	}
	if !CheckIPWhitelist("192.0.2.10") {
		t.Fatal("expected whitelist IP to be loaded")
	}
}

func TestWriteWhitelistPrintsSortedIPs(t *testing.T) {
	ipWhitelistAtomic.Store(&whitelistMap{
		data: map[string]bool{
			"192.0.2.20": true,
			"192.0.2.10": true,
		},
	})

	var buf bytes.Buffer
	if err := WriteWhitelist(&buf); err != nil {
		t.Fatalf("write whitelist: %v", err)
	}

	output := strings.Split(strings.TrimSpace(buf.String()), "\n")
	expected := []string{"IP", "----------", "192.0.2.10", "192.0.2.20"}
	if len(output) != len(expected) {
		t.Fatalf("unexpected output length: got %d want %d; output=%q", len(output), len(expected), buf.String())
	}
	for i, line := range expected {
		if output[i] != line {
			t.Fatalf("unexpected line %d: got %q want %q", i, output[i], line)
		}
	}
}
