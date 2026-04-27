//go:build darwin

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinPlistIncludesRunCommandAndConfigPath(t *testing.T) {
	spec := ServiceSpec{
		Name:             "go-proxy-server",
		Description:      "Go Proxy Server",
		ExecPath:         "/usr/local/bin/go-proxy-server",
		Args:             []string{"run", "-config", "/Users/me/.config/go-proxy-server/config.toml"},
		WorkingDirectory: "/Users/me",
	}

	plist, err := renderLaunchAgentPlist(spec)
	if err != nil {
		t.Fatalf("renderLaunchAgentPlist: %v", err)
	}
	if !strings.Contains(plist, "<string>run</string>") || !strings.Contains(plist, "<string>-config</string>") {
		t.Fatalf("plist missing run args: %q", plist)
	}
}

func TestDarwinStartAfterStopBootstrapsUnloadedAgent(t *testing.T) {
	const name = "go-proxy-server"

	manager, logPath, plistPath := newTestDarwinManager(t, name)
	t.Setenv("FAKE_LAUNCHCTL_PRINT_MODE", "unloaded")

	if err := manager.Stop(name); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := manager.Start(name); err != nil {
		t.Fatalf("Start: %v", err)
	}

	got := readFakeLaunchctlLog(t, logPath)
	want := []string{
		fmt.Sprintf("bootout gui/%d %s", os.Getuid(), plistPath),
		fmt.Sprintf("print gui/%d/%s", os.Getuid(), name),
		fmt.Sprintf("bootstrap gui/%d %s", os.Getuid(), plistPath),
		fmt.Sprintf("kickstart -k gui/%d/%s", os.Getuid(), name),
	}
	if len(got) != len(want) {
		t.Fatalf("launchctl calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("launchctl call %d = %q, want %q (all calls: %v)", i, got[i], want[i], got)
		}
	}
}

func TestDarwinStatusTreatsMissingServiceAsUnloaded(t *testing.T) {
	const name = "go-proxy-server"

	manager, _, _ := newTestDarwinManager(t, name)
	t.Setenv("FAKE_LAUNCHCTL_PRINT_MODE", "unloaded")

	status, err := manager.Status(name)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != "unloaded" {
		t.Fatalf("status.State = %q, want unloaded", status.State)
	}
	if status.Running {
		t.Fatal("status.Running = true, want false")
	}
	if !strings.Contains(status.Detail, "Could not find service") {
		t.Fatalf("status.Detail = %q, want missing-service detail", status.Detail)
	}
}

func TestDarwinStatusReturnsRealPrintFailures(t *testing.T) {
	const name = "go-proxy-server"

	manager, _, _ := newTestDarwinManager(t, name)
	t.Setenv("FAKE_LAUNCHCTL_PRINT_MODE", "error")

	_, err := manager.Status(name)
	if err == nil {
		t.Fatal("Status error = nil, want launchctl print failure")
	}
	if !strings.Contains(err.Error(), "launchctl print") {
		t.Fatalf("Status error = %q, want launchctl print context", err)
	}
	if !strings.Contains(err.Error(), "print failed") {
		t.Fatalf("Status error = %q, want launchctl output detail", err)
	}
}

func newTestDarwinManager(t *testing.T, name string) (darwinManager, string, string) {
	t.Helper()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "launchctl.log")
	if err := os.WriteFile(filepath.Join(tmpDir, "launchctl"), []byte(fakeLaunchctlScript), 0o755); err != nil {
		t.Fatalf("write fake launchctl: %v", err)
	}
	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LAUNCHCTL_LOG", logPath)

	launchAgentsDir := filepath.Join(tmpDir, "LaunchAgents")
	if err := os.MkdirAll(launchAgentsDir, 0o755); err != nil {
		t.Fatalf("create LaunchAgents directory: %v", err)
	}
	plistPath := filepath.Join(launchAgentsDir, name+".plist")
	if err := os.WriteFile(plistPath, []byte("<plist/>"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	return darwinManager{launchAgentsDir: launchAgentsDir}, logPath, plistPath
}

func readFakeLaunchctlLog(t *testing.T, logPath string) []string {
	t.Helper()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake launchctl log: %v", err)
	}

	raw := strings.Split(strings.TrimSpace(string(data)), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

const fakeLaunchctlScript = `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_LAUNCHCTL_LOG"

case "$1" in
print)
	case "$FAKE_LAUNCHCTL_PRINT_MODE" in
	unloaded)
		echo "Could not find service \"$2\"" >&2
		exit 113
		;;
	error)
		echo "print failed for $2" >&2
		exit 1
		;;
	esac
	echo "state = running"
	exit 0
	;;
esac

exit 0
`
