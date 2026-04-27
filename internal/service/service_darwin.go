//go:build darwin

package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type darwinManager struct {
	launchAgentsDir string
}

func newManagerForRuntimeOS(_ string) Manager {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return darwinManager{launchAgentsDir: filepath.Join(home, "Library", "LaunchAgents")}
}

func (m darwinManager) Install(spec ServiceSpec) error {
	if err := validateServiceSpec(spec); err != nil {
		return err
	}

	if err := os.MkdirAll(m.launchAgentsDir, 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	plistPath := m.plistPath(spec.Name)
	if err := os.WriteFile(plistPath, []byte(renderLaunchAgentPlistOrPanic(spec)), 0o644); err != nil {
		return fmt.Errorf("write launchd plist: %w", err)
	}
	if err := m.launchctl("bootout", m.bootstrapTarget(spec.Name), plistPath); err != nil {
		if !isLaunchctlUnloadedError(err) {
			return err
		}
	}
	if err := m.launchctl("bootstrap", m.bootstrapTarget(spec.Name), plistPath); err != nil {
		return err
	}
	return m.launchctl("kickstart", "-k", m.bootstrapTarget(spec.Name)+"/"+spec.Name)
}

func (m darwinManager) Uninstall(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	plistPath := m.plistPath(name)
	if _, err := os.Stat(plistPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service %q is not installed", name)
		}
		return fmt.Errorf("check launchd plist: %w", err)
	}
	if err := m.launchctl("bootout", m.bootstrapTarget(name), plistPath); err != nil && !isLaunchctlUnloadedError(err) {
		return err
	}
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove launchd plist: %w", err)
	}
	return nil
}

func (m darwinManager) Start(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := m.ensureInstalled(name); err != nil {
		return err
	}
	if err := m.ensureLoaded(name); err != nil {
		return err
	}
	return m.launchctl("kickstart", "-k", m.bootstrapTarget(name)+"/"+name)
}

func (m darwinManager) Stop(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := m.ensureInstalled(name); err != nil {
		return err
	}
	return m.launchctl("bootout", m.bootstrapTarget(name), m.plistPath(name))
}

func (m darwinManager) Status(name string) (Status, error) {
	if err := ValidateName(name); err != nil {
		return Status{}, err
	}
	if err := m.ensureInstalled(name); err != nil {
		return Status{}, err
	}
	output, err := m.launchctlOutput("print", m.bootstrapTarget(name)+"/"+name)
	if err != nil {
		if !isLaunchctlUnloadedError(err) {
			return Status{}, err
		}
		return Status{
			Name:    name,
			State:   "unloaded",
			Enabled: true,
			Running: false,
			Detail:  compactDetail(output),
		}, nil
	}
	running := strings.Contains(output, "state = running")
	state := "loaded"
	if running {
		state = "running"
	}
	return Status{
		Name:    name,
		State:   state,
		Enabled: true,
		Running: running,
		Detail:  compactDetail(output),
	}, nil
}

func (m darwinManager) ensureLoaded(name string) error {
	_, err := m.launchctlOutput("print", m.bootstrapTarget(name)+"/"+name)
	if err == nil {
		return nil
	}
	if !isLaunchctlUnloadedError(err) {
		return err
	}
	return m.launchctl("bootstrap", m.bootstrapTarget(name), m.plistPath(name))
}

func (m darwinManager) ensureInstalled(name string) error {
	if _, err := os.Stat(m.plistPath(name)); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service %q is not installed", name)
		}
		return fmt.Errorf("check launchd plist: %w", err)
	}
	return nil
}

func (m darwinManager) plistPath(name string) string {
	return filepath.Join(m.launchAgentsDir, name+".plist")
}

func (m darwinManager) bootstrapTarget(name string) string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func (m darwinManager) launchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, trimmed)
		}
		return fmt.Errorf("launchctl %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (m darwinManager) launchctlOutput(args ...string) (string, error) {
	cmd := exec.Command("launchctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return string(output), fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, trimmed)
		}
		return string(output), fmt.Errorf("launchctl %s: %w", strings.Join(args, " "), err)
	}
	return string(output), nil
}

func renderLaunchAgentPlist(spec ServiceSpec) (string, error) {
	if err := validateServiceSpec(spec); err != nil {
		return "", err
	}
	args := append([]string{spec.ExecPath}, spec.Args...)

	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("  <key>Label</key>\n  <string>")
	b.WriteString(xmlEscape(spec.Name))
	b.WriteString("</string>\n")
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, arg := range args {
		b.WriteString("    <string>")
		b.WriteString(xmlEscape(arg))
		b.WriteString("</string>\n")
	}
	b.WriteString("  </array>\n")
	if spec.WorkingDirectory != "" {
		b.WriteString("  <key>WorkingDirectory</key>\n  <string>")
		b.WriteString(xmlEscape(spec.WorkingDirectory))
		b.WriteString("</string>\n")
	}
	b.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	b.WriteString("  <key>KeepAlive</key>\n  <true/>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String(), nil
}

func renderLaunchAgentPlistOrPanic(spec ServiceSpec) string {
	plist, err := renderLaunchAgentPlist(spec)
	if err != nil {
		panic(err)
	}
	return plist
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

func xmlEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}

func isLaunchctlUnloadedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "could not find service") || strings.Contains(msg, "no such process")
}
