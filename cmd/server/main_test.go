package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestCommandRegistryCoversUsageEntries(t *testing.T) {
	if len(commandRegistry) != len(commandSpecs) {
		t.Fatalf("registry/spec count mismatch: got %d want %d", len(commandRegistry), len(commandSpecs))
	}

	for _, spec := range commandSpecs {
		registered, ok := commandRegistry[spec.name]
		if !ok {
			t.Fatalf("missing command registration for %q", spec.name)
		}
		if registered.run == nil {
			t.Fatalf("missing handler for %q", spec.name)
		}
		if registered.usage != spec.usage {
			t.Fatalf("usage mismatch for %q: got %q want %q", spec.name, registered.usage, spec.usage)
		}
	}
}

func TestWriteUsageIncludesAllCommands(t *testing.T) {
	var buf bytes.Buffer
	writeUsage(&buf)
	output := buf.String()

	if !strings.HasPrefix(output, "Usage:\n") {
		t.Fatalf("usage header missing: %q", output)
	}

	for _, spec := range commandSpecs {
		if !strings.Contains(output, spec.usage) {
			t.Fatalf("usage output missing %q", spec.usage)
		}
	}
}

func TestNewBootstrapReturnsConfigError(t *testing.T) {
	origEnsure := ensureSingleInstanceFn
	origLoadDotEnv := loadDotEnvFn
	origLoadConfig := loadConfigFn
	origInitLogger := initLoggerFn
	origSetupCleanup := setupCleanupHandlerFn
	origInitDB := initializeDatabaseFn
	defer func() {
		ensureSingleInstanceFn = origEnsure
		loadDotEnvFn = origLoadDotEnv
		loadConfigFn = origLoadConfig
		initLoggerFn = origInitLogger
		setupCleanupHandlerFn = origSetupCleanup
		initializeDatabaseFn = origInitDB
	}()

	ensureSingleInstanceFn = func(stdout, stderr io.Writer) (func(), bool) {
		return nil, true
	}
	loadDotEnvFn = func() error { return nil }
	loadConfigFn = func() error { return errors.New("load failed") }
	initLoggerFn = func() error { return nil }
	setupCleanupHandlerFn = func() {}
	initializeDatabaseFn = func() (*gorm.DB, error) {
		t.Fatal("initializeDatabase should not be called when config load fails")
		return nil, nil
	}

	bootstrap, handled, err := NewBootstrap(&bytes.Buffer{}, &bytes.Buffer{})
	if handled {
		t.Fatal("bootstrap should not be marked handled")
	}
	if bootstrap != nil {
		t.Fatal("bootstrap should be nil on config error")
	}
	if err == nil || !strings.Contains(err.Error(), "config error: load failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewBootstrapReportsLoggerInitWarningAndReleasesOnDBFailure(t *testing.T) {
	origEnsure := ensureSingleInstanceFn
	origLoadDotEnv := loadDotEnvFn
	origLoadConfig := loadConfigFn
	origInitLogger := initLoggerFn
	origSetupCleanup := setupCleanupHandlerFn
	origInitDB := initializeDatabaseFn
	defer func() {
		ensureSingleInstanceFn = origEnsure
		loadDotEnvFn = origLoadDotEnv
		loadConfigFn = origLoadConfig
		initLoggerFn = origInitLogger
		setupCleanupHandlerFn = origSetupCleanup
		initializeDatabaseFn = origInitDB
	}()

	released := false
	ensureSingleInstanceFn = func(stdout, stderr io.Writer) (func(), bool) {
		return func() { released = true }, true
	}
	loadDotEnvFn = func() error { return nil }
	loadConfigFn = func() error { return nil }
	initLoggerFn = func() error { return errors.New("logger init failed") }
	setupCleanupHandlerFn = func() {}
	initializeDatabaseFn = func() (*gorm.DB, error) {
		return nil, errors.New("db failed")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	bootstrap, handled, err := NewBootstrap(&stdout, &stderr)
	if handled {
		t.Fatal("bootstrap should not be marked handled")
	}
	if bootstrap != nil {
		t.Fatal("bootstrap should be nil on db init error")
	}
	if err == nil || !strings.Contains(err.Error(), "initialization failed: db failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !released {
		t.Fatal("expected release callback to run on initialization failure")
	}
	if !strings.Contains(stderr.String(), "Failed to initialize logger: logger init failed") {
		t.Fatalf("missing logger init warning on stderr: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout output: %q", stdout.String())
	}
}

func TestNewBootstrapLoadsDotEnvBeforeConfig(t *testing.T) {
	origEnsure := ensureSingleInstanceFn
	origLoadDotEnv := loadDotEnvFn
	origLoadConfig := loadConfigFn
	origInitLogger := initLoggerFn
	origSetupCleanup := setupCleanupHandlerFn
	origInitDB := initializeDatabaseFn
	defer func() {
		ensureSingleInstanceFn = origEnsure
		loadDotEnvFn = origLoadDotEnv
		loadConfigFn = origLoadConfig
		initLoggerFn = origInitLogger
		setupCleanupHandlerFn = origSetupCleanup
		initializeDatabaseFn = origInitDB
	}()

	if err := os.Unsetenv("GEETEST_ID"); err != nil {
		t.Fatalf("unset GEETEST_ID: %v", err)
	}

	ensureSingleInstanceFn = func(stdout, stderr io.Writer) (func(), bool) {
		return nil, true
	}
	loadDotEnvFn = func() error {
		return os.Setenv("GEETEST_ID", "dotenv-value")
	}
	loadConfigFn = func() error {
		if got := os.Getenv("GEETEST_ID"); got != "dotenv-value" {
			t.Fatalf("expected dotenv to load before config, got %q", got)
		}
		return nil
	}
	initLoggerFn = func() error { return nil }
	setupCleanupHandlerFn = func() {}
	initializeDatabaseFn = func() (*gorm.DB, error) { return newTestDB(t), nil }

	bootstrap, handled, err := NewBootstrap(&bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewBootstrap: %v", err)
	}
	if handled {
		t.Fatal("bootstrap should not be marked handled")
	}
	if bootstrap == nil {
		t.Fatal("expected bootstrap")
	}
	bootstrap.Close()
}

func TestEnsureSingleInstanceAlreadyRunningWritesPromptAndWaits(t *testing.T) {
	origGOOS := currentGOOS
	origCheck := singleInstanceCheckFn
	origArgs := currentArgsFn
	origWait := waitForExitAckFn
	defer func() {
		currentGOOS = origGOOS
		singleInstanceCheckFn = origCheck
		currentArgsFn = origArgs
		waitForExitAckFn = origWait
	}()

	currentGOOS = func() string { return "windows" }
	singleInstanceCheckFn = func(name string) (bool, error) { return false, nil }
	currentArgsFn = func() []string { return []string{"server"} }
	waited := false
	waitForExitAckFn = func() { waited = true }

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	release, ok := ensureSingleInstance(&stdout, &stderr)
	if ok {
		t.Fatal("expected ensureSingleInstance to report handled=false when another instance is running")
	}
	if release != nil {
		t.Fatal("expected no release callback when instance is already running")
	}
	if !waited {
		t.Fatal("expected waitForExitAckFn to be called")
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr output: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Another instance is already running!") {
		t.Fatalf("missing already-running prompt: %q", stdout.String())
	}
}
