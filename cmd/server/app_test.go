package main

import (
	"bytes"
	"errors"
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

func TestAppHandleListIPCommandWritesSortedWhitelist(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.Whitelist{IP: "192.0.2.20"}).Error; err != nil {
		t.Fatalf("create whitelist: %v", err)
	}
	if err := db.Create(&models.Whitelist{IP: "192.0.2.10"}).Error; err != nil {
		t.Fatalf("create whitelist: %v", err)
	}

	var out bytes.Buffer
	app := NewApp(db, &out, nil)
	if err := app.handleListIPCommand(nil); err != nil {
		t.Fatalf("list IPs: %v", err)
	}

	expected := "IP\n----------\n192.0.2.10\n192.0.2.20\n"
	if out.String() != expected {
		t.Fatalf("unexpected whitelist output: got %q want %q", out.String(), expected)
	}
}

func TestAppHandleAddAndDeleteIPCommands(t *testing.T) {
	db := newTestDB(t)
	var out bytes.Buffer
	app := NewApp(db, &out, nil)

	if err := app.handleAddIPCommand([]string{"-ip", "192.0.2.30"}); err != nil {
		t.Fatalf("add IP: %v", err)
	}
	if !strings.Contains(out.String(), "Whiteip added successfully!") {
		t.Fatalf("missing add success output: %q", out.String())
	}

	var count int64
	if err := db.Model(&models.Whitelist{}).Where("ip = ?", "192.0.2.30").Count(&count).Error; err != nil {
		t.Fatalf("count whitelist: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected IP to exist after add, count=%d", count)
	}

	out.Reset()
	if err := app.handleDeleteIPCommand([]string{"-ip", "192.0.2.30"}); err != nil {
		t.Fatalf("delete IP: %v", err)
	}
	if !strings.Contains(out.String(), "Whiteip deleted successfully!") {
		t.Fatalf("missing delete success output: %q", out.String())
	}

	if err := db.Model(&models.Whitelist{}).Where("ip = ?", "192.0.2.30").Count(&count).Error; err != nil {
		t.Fatalf("count whitelist: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected IP to be deleted, count=%d", count)
	}
}

func TestAppHandleListUserCommandWritesSortedUsers(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.User{Username: "charlie"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&models.User{Username: "alice"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	var out bytes.Buffer
	app := NewApp(db, &out, nil)
	if err := app.handleListUserCommand(nil); err != nil {
		t.Fatalf("list users: %v", err)
	}

	expected := "Username\n----------\nalice\ncharlie\n"
	if out.String() != expected {
		t.Fatalf("unexpected user output: got %q want %q", out.String(), expected)
	}
}

func TestAppRunUnknownCommandWritesUsageToStderr(t *testing.T) {
	db := newTestDB(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	app := NewApp(db, &stdout, &stderr)
	err := app.Run([]string{"unknown"})
	if err == nil {
		t.Fatal("expected unknown command error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout for unknown command: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage:\n") {
		t.Fatalf("expected usage on stderr, got %q", stderr.String())
	}
}

func TestAppRunHelpFlagWritesUsageToStdout(t *testing.T) {
	db := newTestDB(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	app := NewApp(db, &stdout, &stderr)
	if err := app.Run([]string{"--help"}); err != nil {
		t.Fatalf("help run returned error: %v", err)
	}

	if !strings.Contains(stdout.String(), "Usage:\n") {
		t.Fatalf("expected usage on stdout, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "--help") {
		t.Fatalf("expected global help flag in usage, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr for help: %q", stderr.String())
	}
}

func TestAppRunVersionFlagWritesVersionToStdout(t *testing.T) {
	db := newTestDB(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	originalVersion := version
	version = "test-version"
	defer func() {
		version = originalVersion
	}()

	app := NewApp(db, &stdout, &stderr)
	if err := app.Run([]string{"--version"}); err != nil {
		t.Fatalf("version run returned error: %v", err)
	}

	if got := stdout.String(); got != "test-version\n" {
		t.Fatalf("unexpected version output: got %q want %q", got, "test-version\n")
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr for version: %q", stderr.String())
	}
}

func TestAppRunDefaultModeWritesTrayFallbackToStdout(t *testing.T) {
	db := newTestDB(t)
	origGOOS := currentGOOS
	origStartTray := startTrayModeFn
	origStartWeb := startWebModeFn
	defer func() {
		currentGOOS = origGOOS
		startTrayModeFn = origStartTray
		startWebModeFn = origStartWeb
	}()

	currentGOOS = func() string { return "windows" }
	startTrayModeFn = func(db *gorm.DB, port int) error { return errors.New("tray failed") }
	startWebCalled := false
	startWebModeFn = func(db *gorm.DB, port int) error {
		startWebCalled = true
		return nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(db, &stdout, &stderr)
	if err := app.Run(nil); err != nil {
		t.Fatalf("run default mode: %v", err)
	}

	if !startWebCalled {
		t.Fatal("expected web fallback to run after tray failure")
	}
	if !strings.Contains(stdout.String(), "系统托盘启动失败，切换到Web服务器模式...") {
		t.Fatalf("missing fallback message on stdout: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr output: %q", stderr.String())
	}
}
