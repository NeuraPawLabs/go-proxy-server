package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"

	applogger "github.com/apeming/go-proxy-server/internal/logger"
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
	if !strings.Contains(output, "--help") {
		t.Fatalf("usage output missing global help flag: %q", output)
	}
	if !strings.Contains(output, "--version") {
		t.Fatalf("usage output missing global version flag: %q", output)
	}
}

func TestBootstrapLogLevelForCLIArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int32
	}{
		{name: "default mode keeps service logs", args: nil, want: int32(1)},
		{name: "web keeps service logs", args: []string{"web"}, want: int32(1)},
		{name: "socks keeps service logs", args: []string{"socks"}, want: int32(1)},
		{name: "tunnel server keeps service logs", args: []string{"tunnel-server"}, want: int32(1)},
		{name: "listuser is quiet", args: []string{"listuser"}, want: int32(4)},
		{name: "adduser is quiet", args: []string{"adduser"}, want: int32(4)},
		{name: "listip is quiet", args: []string{"listip"}, want: int32(4)},
		{name: "help is quiet", args: []string{"--help"}, want: int32(4)},
		{name: "version is quiet", args: []string{"--version"}, want: int32(4)},
		{name: "unknown command is quiet", args: []string{"wat"}, want: int32(4)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := int32(bootstrapLogLevel(tt.args)); got != tt.want {
				t.Fatalf("bootstrapLogLevel(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func TestNewBootstrapHandlesHelpWithoutInitialization(t *testing.T) {
	origEnsure := ensureSingleInstanceFn
	origLoadDotEnv := loadDotEnvFn
	origLoadConfig := loadConfigFn
	origInitLogger := initLoggerFn
	origSetupCleanup := setupCleanupHandlerFn
	origInitDB := initializeDatabaseFn
	origArgs := currentArgsFn
	defer func() {
		ensureSingleInstanceFn = origEnsure
		loadDotEnvFn = origLoadDotEnv
		loadConfigFn = origLoadConfig
		initLoggerFn = origInitLogger
		setupCleanupHandlerFn = origSetupCleanup
		initializeDatabaseFn = origInitDB
		currentArgsFn = origArgs
	}()

	ensureSingleInstanceFn = func(stdout, stderr io.Writer) (func(), bool) {
		t.Fatal("ensureSingleInstance should not run for --help")
		return nil, false
	}
	loadDotEnvFn = func() error {
		t.Fatal("loadDotEnv should not run for --help")
		return nil
	}
	loadConfigFn = func() error {
		t.Fatal("loadConfig should not run for --help")
		return nil
	}
	initLoggerFn = func() error {
		t.Fatal("initLogger should not run for --help")
		return nil
	}
	setupCleanupHandlerFn = func() {
		t.Fatal("setupCleanupHandler should not run for --help")
	}
	initializeDatabaseFn = func() (*gorm.DB, error) {
		t.Fatal("initializeDatabase should not run for --help")
		return nil, nil
	}
	currentArgsFn = func() []string { return []string{"server", "--help"} }

	var stdout bytes.Buffer
	bootstrap, handled, err := NewBootstrap(&stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewBootstrap: %v", err)
	}
	if !handled {
		t.Fatal("expected help to be handled during bootstrap")
	}
	if bootstrap != nil {
		t.Fatal("expected no bootstrap when help is handled")
	}
	if !strings.Contains(stdout.String(), "Usage:\n") {
		t.Fatalf("expected usage output, got %q", stdout.String())
	}
}

func TestNewBootstrapHandlesVersionWithoutInitialization(t *testing.T) {
	origEnsure := ensureSingleInstanceFn
	origLoadDotEnv := loadDotEnvFn
	origLoadConfig := loadConfigFn
	origInitLogger := initLoggerFn
	origSetupCleanup := setupCleanupHandlerFn
	origInitDB := initializeDatabaseFn
	origArgs := currentArgsFn
	defer func() {
		ensureSingleInstanceFn = origEnsure
		loadDotEnvFn = origLoadDotEnv
		loadConfigFn = origLoadConfig
		initLoggerFn = origInitLogger
		setupCleanupHandlerFn = origSetupCleanup
		initializeDatabaseFn = origInitDB
		currentArgsFn = origArgs
		version = "dev"
	}()

	ensureSingleInstanceFn = func(stdout, stderr io.Writer) (func(), bool) {
		t.Fatal("ensureSingleInstance should not run for --version")
		return nil, false
	}
	loadDotEnvFn = func() error {
		t.Fatal("loadDotEnv should not run for --version")
		return nil
	}
	loadConfigFn = func() error {
		t.Fatal("loadConfig should not run for --version")
		return nil
	}
	initLoggerFn = func() error {
		t.Fatal("initLogger should not run for --version")
		return nil
	}
	setupCleanupHandlerFn = func() {
		t.Fatal("setupCleanupHandler should not run for --version")
	}
	initializeDatabaseFn = func() (*gorm.DB, error) {
		t.Fatal("initializeDatabase should not run for --version")
		return nil, nil
	}
	currentArgsFn = func() []string { return []string{"server", "--version"} }
	version = "test-version"

	var stdout bytes.Buffer
	bootstrap, handled, err := NewBootstrap(&stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewBootstrap: %v", err)
	}
	if !handled {
		t.Fatal("expected version to be handled during bootstrap")
	}
	if bootstrap != nil {
		t.Fatal("expected no bootstrap when version is handled")
	}
	if got := stdout.String(); got != "test-version\n" {
		t.Fatalf("unexpected version output: got %q want %q", got, "test-version\n")
	}
}

func TestReportBootstrapFailureWritesStderr(t *testing.T) {
	var stderr bytes.Buffer

	reportBootstrapFailure(&stderr, errors.New("boom"))

	if got := stderr.String(); !strings.Contains(got, "Error: boom") {
		t.Fatalf("expected bootstrap error on stderr, got %q", got)
	}
}

func TestShutdownServiceLogsSingleConciseLine(t *testing.T) {
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = origStdout
	}()

	origCloseAll := closeAllTransportsFn
	origExit := exitProcessFn
	defer func() {
		closeAllTransportsFn = origCloseAll
		exitProcessFn = origExit
	}()

	closeCalled := false
	exitCode := -1
	closeAllTransportsFn = func() {
		closeCalled = true
	}
	exitProcessFn = func(code int) {
		exitCode = code
	}

	applogger.InitStdout()
	applogger.SetLevel(applogger.LevelInfo)

	shutdownService()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	var out bytes.Buffer
	if _, err := io.Copy(&out, r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}

	if !closeCalled {
		t.Fatal("expected transports to be closed")
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !strings.Contains(out.String(), "Shutting down...") {
		t.Fatalf("expected concise shutdown log, got %q", out.String())
	}
	if strings.Contains(out.String(), "All transport connections closed") {
		t.Fatalf("unexpected verbose shutdown log: %q", out.String())
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
