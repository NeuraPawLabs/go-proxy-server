package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/models"
	runtimecfg "github.com/apeming/go-proxy-server/internal/runtime"
	"github.com/apeming/go-proxy-server/internal/service"
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

func TestToProxyExitProbeConfigCopiesRuntimeSettings(t *testing.T) {
	got := toProxyExitProbeConfig(runtimecfg.ExitProbeConfig{
		Enabled:  true,
		ProbeURL: "https://example.test/ip",
		Timeout:  2 * time.Second,
	})

	if !got.Enabled {
		t.Fatalf("enabled = false, want true")
	}
	if got.ProbeURL != "https://example.test/ip" {
		t.Fatalf("probe URL = %q, want https://example.test/ip", got.ProbeURL)
	}
	if got.Timeout != 2*time.Second {
		t.Fatalf("timeout = %s, want 2s", got.Timeout)
	}
}

func TestAppRunSubcommandHelpWritesUsageToStdout(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantContains  []string
		disallowCalls func(t *testing.T) func()
	}{
		{
			name:         "socks help",
			args:         []string{"socks", "--help"},
			wantContains: []string{"Usage:\n", "go-proxy-server socks -port <port_number> [-bind-listen]", "-port int", "-h, --help"},
		},
		{
			name:         "listuser help",
			args:         []string{"listuser", "--help"},
			wantContains: []string{"Usage:\n", "go-proxy-server listuser", "-h, --help"},
		},
		{
			name:         "service help",
			args:         []string{"service", "--help"},
			wantContains: []string{"Usage:\n", "go-proxy-server service <install|uninstall|start|stop|status>", "go-proxy-server service install", "go-proxy-server service status"},
		},
		{
			name:         "service install help",
			args:         []string{"service", "install", "--help"},
			wantContains: []string{"Usage:\n", "go-proxy-server service install [-config <path>] [--name <service-name>]", "-config string", "-name string"},
			disallowCalls: func(t *testing.T) func() {
				origInstall := installServiceFn
				installServiceFn = func(spec service.ServiceSpec) error {
					t.Fatal("installServiceFn should not run for service install --help")
					return nil
				}
				return func() {
					installServiceFn = origInstall
				}
			},
		},
		{
			name:         "run help",
			args:         []string{"run", "--help"},
			wantContains: []string{"Usage:\n", "go-proxy-server run [-config <path>] [-web-port <port>] [-socks-port <port>] [-socks-bind-listen] [-http-port <port>] [-http-bind-listen]", "-config string", "-web-port int"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := func() {}
			if tt.disallowCalls != nil {
				cleanup = tt.disallowCalls(t)
			}
			defer cleanup()

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app := NewApp(newTestDB(t), &stdout, &stderr)
			if err := app.Run(tt.args); err != nil {
				t.Fatalf("Run(%v): %v", tt.args, err)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout missing %q, got %q", want, stdout.String())
				}
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr for %v: %q", tt.args, stderr.String())
			}
		})
	}
}

func TestAppRunNoArgsWritesUsageToStdoutOnNonWindows(t *testing.T) {
	db := newTestDB(t)
	origGOOS := currentGOOS
	defer func() {
		currentGOOS = origGOOS
	}()

	currentGOOS = func() string { return "linux" }

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(db, &stdout, &stderr)
	if err := app.Run(nil); err != nil {
		t.Fatalf("run with no args: %v", err)
	}

	if !strings.Contains(stdout.String(), "Usage:\n") {
		t.Fatalf("expected usage on stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr output: %q", stderr.String())
	}
}

func TestAppRunNoArgsKeepsWindowsDefaultMode(t *testing.T) {
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

func TestWriteUsageIncludesRunAndServiceCommands(t *testing.T) {
	var buf bytes.Buffer
	writeUsage(&buf)
	output := buf.String()

	if !strings.Contains(output, "run [-config <path>]") {
		t.Fatalf("usage output missing run command: %q", output)
	}
	if !strings.Contains(output, "service install") {
		t.Fatalf("usage output missing service command: %q", output)
	}
}

func TestAppHandleRunCommandLoadsConfigAndAppliesOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[web]\nenabled = true\nport = 8080\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origRun := runConfigRuntimeFn
	defer func() { runConfigRuntimeFn = origRun }()

	var captured runtimecfg.Config
	runConfigRuntimeFn = func(ctx context.Context, db *gorm.DB, cfg runtimecfg.Config) error {
		captured = cfg
		return nil
	}

	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	if err := app.handleRunCommand([]string{"-config", path, "--web-port", "9090"}); err != nil {
		t.Fatalf("handleRunCommand: %v", err)
	}
	if captured.Web.Port != 9090 {
		t.Fatalf("web port = %d, want 9090", captured.Web.Port)
	}
}

func TestAppHandleRunCommandTreatsZeroPortOverridesAsExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[web]\nenabled = true\nport = 8080\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origRun := runConfigRuntimeFn
	defer func() { runConfigRuntimeFn = origRun }()

	var captured runtimecfg.Config
	runConfigRuntimeFn = func(ctx context.Context, db *gorm.DB, cfg runtimecfg.Config) error {
		captured = cfg
		return nil
	}

	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	if err := app.handleRunCommand([]string{"-config", path, "--web-port", "0", "--socks-port", "0", "--http-port", "0"}); err != nil {
		t.Fatalf("handleRunCommand: %v", err)
	}
	if captured.Web.Port != 0 || !captured.Web.Enabled {
		t.Fatalf("expected explicit web port 0 to be applied, got %+v", captured.Web)
	}
	if captured.Socks.Port != 0 || !captured.Socks.Enabled {
		t.Fatalf("expected explicit socks port 0 to be applied, got %+v", captured.Socks)
	}
	if captured.HTTP.Port != 0 || !captured.HTTP.Enabled {
		t.Fatalf("expected explicit http port 0 to be applied, got %+v", captured.HTTP)
	}
}

func TestAppHandleRunCommandReturnsClearMissingConfigError(t *testing.T) {
	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	err := app.handleRunCommand([]string{"-config", filepath.Join(t.TempDir(), "missing.toml")})
	if err == nil || !strings.Contains(err.Error(), "runtime config not found") {
		t.Fatalf("expected missing config error, got %v", err)
	}
}

func TestAppHandleRunCommandRejectsInvalidTunnelServerConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := "[tunnel_server]\nenabled = true\nallow_insecure = true\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origRun := runConfigRuntimeFn
	defer func() { runConfigRuntimeFn = origRun }()
	runConfigRuntimeFn = func(context.Context, *gorm.DB, runtimecfg.Config) error {
		t.Fatal("runConfigRuntimeFn should not be called when runtime validation fails")
		return nil
	}

	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	err := app.handleRunCommand([]string{"-config", path})
	if err == nil || !strings.Contains(err.Error(), "tunnel token is required") {
		t.Fatalf("expected tunnel validation error, got %v", err)
	}
}

func TestAppHandleRunCommandTreatsShutdownSignalAsCleanExit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[web]\nenabled = true\nport = 8080\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origRun := runConfigRuntimeFn
	defer func() { runConfigRuntimeFn = origRun }()

	defaultHandlerCalled := false
	restoreDefault := setShutdownSignalHandler(func() {
		defaultHandlerCalled = true
	})
	defer restoreDefault()

	runConfigRuntimeFn = func(ctx context.Context, db *gorm.DB, cfg runtimecfg.Config) error {
		invokeShutdownSignalHandler()
		<-ctx.Done()
		return ctx.Err()
	}

	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	if err := app.handleRunCommand([]string{"-config", path}); err != nil {
		t.Fatalf("handleRunCommand: %v", err)
	}

	invokeShutdownSignalHandler()
	if !defaultHandlerCalled {
		t.Fatal("expected shutdown handler to be restored after run command exits")
	}
}

func TestAppHandleServiceInstallBuildsRunSpec(t *testing.T) {
	origInstall := installServiceFn
	origGOOS := currentGOOS
	origResolve := resolveServiceInstallConfigPathFn
	origEnsureConfig := ensureDefaultConfigFileFn
	defer func() { installServiceFn = origInstall }()
	defer func() { currentGOOS = origGOOS }()
	defer func() { resolveServiceInstallConfigPathFn = origResolve }()
	defer func() { ensureDefaultConfigFileFn = origEnsureConfig }()

	currentGOOS = func() string { return "darwin" }
	resolveServiceInstallConfigPathFn = func(goos string) (string, error) {
		t.Fatal("resolveServiceInstallConfigPathFn should not be called when -config is provided")
		return "", nil
	}
	ensureDefaultConfigFileFn = func(path string) error {
		if path != "/etc/go-proxy-server/config.toml" {
			t.Fatalf("ensureDefaultConfigFileFn path = %q, want /etc/go-proxy-server/config.toml", path)
		}
		return nil
	}

	var captured service.ServiceSpec
	installServiceFn = func(spec service.ServiceSpec) error {
		captured = spec
		return nil
	}

	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	if err := app.handleServiceCommand([]string{"install", "-config", "/etc/go-proxy-server/config.toml"}); err != nil {
		t.Fatalf("handleServiceCommand: %v", err)
	}
	if len(captured.Args) != 3 || captured.Args[0] != "run" || captured.Args[1] != "-config" {
		t.Fatalf("unexpected service args: %v", captured.Args)
	}
	if captured.Name != service.DefaultServiceName {
		t.Fatalf("service name = %q, want %q", captured.Name, service.DefaultServiceName)
	}
}

func TestAppHandleServiceInstallConvertsRelativeConfigPathToAbsolute(t *testing.T) {
	origInstall := installServiceFn
	origGOOS := currentGOOS
	origResolve := resolveServiceInstallConfigPathFn
	origEnsureConfig := ensureDefaultConfigFileFn
	defer func() { installServiceFn = origInstall }()
	defer func() { currentGOOS = origGOOS }()
	defer func() { resolveServiceInstallConfigPathFn = origResolve }()
	defer func() { ensureDefaultConfigFileFn = origEnsureConfig }()

	currentGOOS = func() string { return "darwin" }
	resolveServiceInstallConfigPathFn = func(goos string) (string, error) {
		t.Fatal("resolveServiceInstallConfigPathFn should not be called when -config is provided")
		return "", nil
	}

	workDir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	wantConfigPath := filepath.Join(workDir, "config.toml")
	ensureDefaultConfigFileFn = func(path string) error {
		if path != wantConfigPath {
			t.Fatalf("ensureDefaultConfigFileFn path = %q, want %q", path, wantConfigPath)
		}
		return nil
	}

	var captured service.ServiceSpec
	installServiceFn = func(spec service.ServiceSpec) error {
		captured = spec
		return nil
	}

	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	if err := app.handleServiceCommand([]string{"install", "-config", "config.toml"}); err != nil {
		t.Fatalf("handleServiceCommand: %v", err)
	}
	if len(captured.Args) != 3 || captured.Args[0] != "run" || captured.Args[1] != "-config" || captured.Args[2] != wantConfigPath {
		t.Fatalf("unexpected service args: %v", captured.Args)
	}
	if captured.WorkingDirectory != workDir {
		t.Fatalf("service working directory = %q, want %q", captured.WorkingDirectory, workDir)
	}
}

func TestAppHandleServiceInstallLinuxInstallsExecutableToSystemPath(t *testing.T) {
	origInstall := installServiceFn
	origGOOS := currentGOOS
	origResolve := resolveServiceInstallConfigPathFn
	origEnsureConfig := ensureDefaultConfigFileFn
	origInstallExecutable := installServiceExecutableFn
	defer func() { installServiceFn = origInstall }()
	defer func() { currentGOOS = origGOOS }()
	defer func() { resolveServiceInstallConfigPathFn = origResolve }()
	defer func() { ensureDefaultConfigFileFn = origEnsureConfig }()
	defer func() { installServiceExecutableFn = origInstallExecutable }()

	currentGOOS = func() string { return "linux" }
	resolveServiceInstallConfigPathFn = func(goos string) (string, error) {
		t.Fatal("resolveServiceInstallConfigPathFn should not be called when -config is provided")
		return "", nil
	}
	ensureDefaultConfigFileFn = func(path string) error { return nil }
	installServiceExecutableFn = func(execPath string) (string, error) {
		if strings.TrimSpace(execPath) == "" {
			t.Fatal("installServiceExecutableFn got empty executable path")
		}
		return "/usr/local/bin/go-proxy-server", nil
	}

	var captured service.ServiceSpec
	installServiceFn = func(spec service.ServiceSpec) error {
		captured = spec
		return nil
	}

	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	if err := app.handleServiceCommand([]string{"install", "-config", "/etc/go-proxy-server/config.toml"}); err != nil {
		t.Fatalf("handleServiceCommand: %v", err)
	}
	if captured.ExecPath != "/usr/local/bin/go-proxy-server" {
		t.Fatalf("service exec path = %q, want /usr/local/bin/go-proxy-server", captured.ExecPath)
	}
	if captured.WorkingDirectory != "/etc/go-proxy-server" {
		t.Fatalf("service working directory = %q, want /etc/go-proxy-server", captured.WorkingDirectory)
	}
}

func TestInstallExecutableToPathCopiesExecutableWithMode(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source-bin")
	target := filepath.Join(dir, "usr", "local", "bin", "go-proxy-server")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write source executable: %v", err)
	}

	got, err := installExecutableToPath(source, target)
	if err != nil {
		t.Fatalf("installExecutableToPath: %v", err)
	}
	if got != target {
		t.Fatalf("installed path = %q, want %q", got, target)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read installed executable: %v", err)
	}
	if string(data) != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("installed executable content = %q", string(data))
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat installed executable: %v", err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o755 {
		t.Fatalf("installed executable mode = %v, want 0755", gotMode)
	}
}

func TestAppHandleServiceInstallCreatesMissingRuntimeConfig(t *testing.T) {
	origInstall := installServiceFn
	origGOOS := currentGOOS
	defer func() { installServiceFn = origInstall }()
	defer func() { currentGOOS = origGOOS }()

	currentGOOS = func() string { return "darwin" }
	installServiceFn = func(spec service.ServiceSpec) error {
		return nil
	}

	configPath := filepath.Join(t.TempDir(), "go-proxy-server", "config.toml")
	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	if err := app.handleServiceCommand([]string{"install", "-config", configPath}); err != nil {
		t.Fatalf("handleServiceCommand: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	text := string(data)
	for _, want := range []string{"[web]", "enabled = false", "[socks]", "enabled = true", "port = 1080"} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated config missing %q:\n%s", want, text)
		}
	}
}

func TestAppHandleServiceInstallWithoutConfigBuildsResolvedLinuxRunSpec(t *testing.T) {
	origInstall := installServiceFn
	origGOOS := currentGOOS
	origResolve := resolveServiceInstallConfigPathFn
	origEnsureConfig := ensureDefaultConfigFileFn
	origInstallExecutable := installServiceExecutableFn
	defer func() { installServiceFn = origInstall }()
	defer func() { currentGOOS = origGOOS }()
	defer func() { resolveServiceInstallConfigPathFn = origResolve }()
	defer func() { ensureDefaultConfigFileFn = origEnsureConfig }()
	defer func() { installServiceExecutableFn = origInstallExecutable }()

	var captured service.ServiceSpec
	installServiceFn = func(spec service.ServiceSpec) error {
		captured = spec
		return nil
	}
	currentGOOS = func() string { return "linux" }
	t.Setenv("XDG_CONFIG_HOME", "/home/test/.config")
	t.Setenv("SUDO_USER", "test")
	ensureDefaultConfigFileFn = func(path string) error {
		if path != "/root/.config/go-proxy-server/config.toml" {
			t.Fatalf("ensureDefaultConfigFileFn path = %q, want /root/.config/go-proxy-server/config.toml", path)
		}
		return nil
	}
	installServiceExecutableFn = func(execPath string) (string, error) {
		return "/usr/local/bin/go-proxy-server", nil
	}

	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	if err := app.handleServiceCommand([]string{"install"}); err != nil {
		t.Fatalf("handleServiceCommand: %v", err)
	}
	if len(captured.Args) != 3 || captured.Args[0] != "run" || captured.Args[1] != "-config" || captured.Args[2] != "/root/.config/go-proxy-server/config.toml" {
		t.Fatalf("unexpected service args: %v", captured.Args)
	}
	if captured.ExecPath != "/usr/local/bin/go-proxy-server" {
		t.Fatalf("service exec path = %q, want /usr/local/bin/go-proxy-server", captured.ExecPath)
	}
	if captured.WorkingDirectory != "/root/.config/go-proxy-server" {
		t.Fatalf("service working directory = %q, want /root/.config/go-proxy-server", captured.WorkingDirectory)
	}
}

func TestAppHandleServiceInstallWithoutConfigCreatesDefaultRunSpecOnDarwin(t *testing.T) {
	origInstall := installServiceFn
	origGOOS := currentGOOS
	origResolve := resolveServiceInstallConfigPathFn
	origEnsureConfig := ensureDefaultConfigFileFn
	defer func() { installServiceFn = origInstall }()
	defer func() { currentGOOS = origGOOS }()
	defer func() { resolveServiceInstallConfigPathFn = origResolve }()
	defer func() { ensureDefaultConfigFileFn = origEnsureConfig }()

	var captured service.ServiceSpec
	installServiceFn = func(spec service.ServiceSpec) error {
		captured = spec
		return nil
	}
	currentGOOS = func() string { return "darwin" }
	resolveServiceInstallConfigPathFn = func(goos string) (string, error) {
		if goos != "darwin" {
			t.Fatalf("resolveServiceInstallConfigPathFn goos = %q, want darwin", goos)
		}
		return "/Users/test/Library/Application Support/go-proxy-server/config.toml", nil
	}
	ensureDefaultConfigFileFn = func(path string) error {
		if path != "/Users/test/Library/Application Support/go-proxy-server/config.toml" {
			t.Fatalf("ensureDefaultConfigFileFn path = %q, want darwin default config path", path)
		}
		return nil
	}

	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	if err := app.handleServiceCommand([]string{"install"}); err != nil {
		t.Fatalf("handleServiceCommand: %v", err)
	}
	if len(captured.Args) != 3 || captured.Args[0] != "run" || captured.Args[1] != "-config" || captured.Args[2] != "/Users/test/Library/Application Support/go-proxy-server/config.toml" {
		t.Fatalf("unexpected service args: %v", captured.Args)
	}
	if captured.WorkingDirectory != "/Users/test/Library/Application Support/go-proxy-server" {
		t.Fatalf("service working directory = %q, want darwin default config dir", captured.WorkingDirectory)
	}
}

func TestResolveServiceInstallConfigPathDarwinUsesDefaultConfigPath(t *testing.T) {
	t.Setenv("HOME", "/Users/test")

	got, err := resolveServiceInstallConfigPath("darwin")
	if err != nil {
		t.Fatalf("resolveServiceInstallConfigPath: %v", err)
	}
	want := filepath.Join("/Users/test", "Library", "Application Support", "go-proxy-server", "config.toml")
	if got != want {
		t.Fatalf("config path = %q, want %q", got, want)
	}
}

func TestResolveServiceInstallConfigPathLinuxUsesRootConfigPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	t.Setenv("SUDO_USER", "alice")

	got, err := resolveServiceInstallConfigPath("linux")
	if err != nil {
		t.Fatalf("resolveServiceInstallConfigPath: %v", err)
	}
	want := "/root/.config/go-proxy-server/config.toml"
	if got != want {
		t.Fatalf("config path = %q, want %q", got, want)
	}
}

func TestResolveServiceInstallConfigPathLinuxIgnoresSudoUserHomeFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/root")
	t.Setenv("SUDO_USER", "alice")

	got, err := resolveServiceInstallConfigPath("linux")
	if err != nil {
		t.Fatalf("resolveServiceInstallConfigPath: %v", err)
	}
	want := "/root/.config/go-proxy-server/config.toml"
	if got != want {
		t.Fatalf("config path = %q, want %q", got, want)
	}
}

func TestAppHandleServiceCommandRejectsUnsafeName(t *testing.T) {
	app := NewApp(newTestDB(t), io.Discard, io.Discard)
	if err := app.handleServiceCommand([]string{"start", "--name", "../evil"}); err == nil {
		t.Fatal("expected unsafe service name to be rejected")
	}
}

func TestAppHandleServiceStatusPrintsSingleLineDetail(t *testing.T) {
	origStatus := serviceStatusFn
	defer func() { serviceStatusFn = origStatus }()

	serviceStatusFn = func(name string) (service.Status, error) {
		return service.Status{
			Name:    name,
			State:   "running",
			Enabled: true,
			Running: true,
			Detail:  "line one\nline two\nline three\nline four",
		}, nil
	}

	var out bytes.Buffer
	app := NewApp(newTestDB(t), &out, io.Discard)
	if err := app.handleServiceCommand([]string{"status"}); err != nil {
		t.Fatalf("handleServiceCommand(status): %v", err)
	}
	if strings.Contains(out.String(), "\nline two") {
		t.Fatalf("status output is not single-line: %q", out.String())
	}
	if !strings.Contains(out.String(), "detail=line one | line two | line three | ...") {
		t.Fatalf("status output did not compact detail: %q", out.String())
	}
}
