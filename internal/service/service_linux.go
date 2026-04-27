//go:build linux

package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type linuxManager struct {
	unitDir string
}

func newManagerForRuntimeOS(_ string) Manager {
	return linuxManager{unitDir: "/etc/systemd/system"}
}

func (m linuxManager) Install(spec ServiceSpec) error {
	if os.Geteuid() != 0 {
		return errors.New("systemd service installation requires root privileges")
	}
	if err := validateServiceSpec(spec); err != nil {
		return err
	}

	unitPath := filepath.Join(m.unitDir, spec.Name+".service")
	if err := os.WriteFile(unitPath, []byte(renderSystemdUnit(spec)), 0o644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if err := m.systemctl("daemon-reload"); err != nil {
		return err
	}
	return m.systemctl("enable", "--now", spec.Name)
}

func (m linuxManager) Uninstall(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return errors.New("systemd service removal requires root privileges")
	}
	unitPath := m.unitPath(name)
	if _, err := os.Stat(unitPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service %q is not installed", name)
		}
		return fmt.Errorf("check systemd unit: %w", err)
	}

	if err := m.systemctl("disable", "--now", name); err != nil {
		return err
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove systemd unit: %w", err)
	}
	return m.systemctl("daemon-reload")
}

func (m linuxManager) Start(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := m.ensureInstalled(name); err != nil {
		return err
	}
	return m.systemctl("start", name)
}

func (m linuxManager) Stop(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := m.ensureInstalled(name); err != nil {
		return err
	}
	return m.systemctl("stop", name)
}

func (m linuxManager) Status(name string) (Status, error) {
	if err := ValidateName(name); err != nil {
		return Status{}, err
	}
	if err := m.ensureInstalled(name); err != nil {
		return Status{}, err
	}

	enabled, err := m.systemctlState("is-enabled", name, map[string]bool{
		"enabled":   true,
		"disabled":  true,
		"static":    true,
		"masked":    true,
		"indirect":  true,
		"generated": true,
		"transient": true,
	})
	if err != nil {
		return Status{}, err
	}
	running, err := m.systemctlState("is-active", name, map[string]bool{
		"active":       true,
		"inactive":     true,
		"failed":       true,
		"activating":   true,
		"deactivating": true,
		"unknown":      true,
	})
	if err != nil {
		return Status{}, err
	}

	return Status{
		Name:    name,
		State:   running,
		Enabled: enabled == "enabled" || enabled == "static" || enabled == "indirect",
		Running: running == "active",
	}, nil
}

func (m linuxManager) ensureInstalled(name string) error {
	if _, err := os.Stat(m.unitPath(name)); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service %q is not installed", name)
		}
		return fmt.Errorf("check systemd unit: %w", err)
	}
	return nil
}

func (m linuxManager) unitPath(name string) string {
	return filepath.Join(m.unitDir, name+".service")
}

func (m linuxManager) systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, trimmed)
		}
		return fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (m linuxManager) systemctlState(command, name string, allowed map[string]bool) (string, error) {
	cmd := exec.Command("systemctl", command, name)
	output, err := cmd.CombinedOutput()
	state := strings.TrimSpace(string(output))
	if state == "" {
		return "", fmt.Errorf("systemctl %s %s returned no state", command, name)
	}
	if allowed[state] {
		return state, nil
	}
	if err != nil {
		return "", fmt.Errorf("systemctl %s %s: %w: %s", command, name, err, state)
	}
	return "", fmt.Errorf("systemctl %s %s returned unexpected state %q", command, name, state)
}

func validateServiceSpec(spec ServiceSpec) error {
	if err := ValidateName(spec.Name); err != nil {
		return err
	}
	if spec.Name == "" {
		return errors.New("service name is required")
	}
	if spec.ExecPath == "" {
		return errors.New("service executable path is required")
	}
	return nil
}

func renderSystemdUnit(spec ServiceSpec) string {
	args := append([]string{spec.ExecPath}, spec.Args...)
	workingDir := spec.WorkingDirectory
	if workingDir == "" {
		workingDir = "/"
	}

	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=")
	b.WriteString(spec.Description)
	b.WriteString("\nAfter=network.target\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	b.WriteString("WorkingDirectory=")
	b.WriteString(escapeSystemdValue(workingDir))
	b.WriteString("\nExecStart=")
	b.WriteString(joinSystemdArgs(args))
	b.WriteString("\nRestart=always\nRestartSec=5\n")
	b.WriteString("TimeoutStopSec=10\nKillMode=mixed\nKillSignal=SIGTERM\n")
	b.WriteString("NoNewPrivileges=true\nPrivateTmp=true\n")
	b.WriteString("LimitNOFILE=65535\n\n")
	b.WriteString("[Install]\nWantedBy=multi-user.target\n")
	return b.String()
}

func joinSystemdArgs(args []string) string {
	rendered := make([]string, 0, len(args))
	for _, arg := range args {
		rendered = append(rendered, escapeSystemdValue(arg))
	}
	return strings.Join(rendered, " ")
}

func escapeSystemdValue(value string) string {
	if value == "" {
		return "\"\""
	}
	if strings.ContainsAny(value, " \t\"'\\") {
		replaced := strings.ReplaceAll(value, `\`, `\\`)
		replaced = strings.ReplaceAll(replaced, `"`, `\"`)
		return `"` + replaced + `"`
	}
	return value
}
