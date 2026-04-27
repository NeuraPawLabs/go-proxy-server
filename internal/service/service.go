package service

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"
)

// DefaultServiceName is the installed service name used by the CLI.
const DefaultServiceName = "go-proxy-server"

var serviceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ServiceSpec describes an OS service definition.
type ServiceSpec struct {
	Name             string
	Description      string
	ExecPath         string
	Args             []string
	WorkingDirectory string
}

// Status reports the current service state.
type Status struct {
	Name    string
	State   string
	Enabled bool
	Running bool
	Detail  string
}

// Manager manages the host service registration for the current platform.
type Manager interface {
	Install(ServiceSpec) error
	Uninstall(name string) error
	Start(name string) error
	Stop(name string) error
	Status(name string) (Status, error)
}

// BuildRunSpec builds a service spec that always launches `go-proxy-server run`.
func BuildRunSpec(execPath, workDir, configPath string) ServiceSpec {
	args := []string{"run"}
	if strings.TrimSpace(configPath) != "" {
		args = append(args, "-config", configPath)
	}
	return ServiceSpec{
		Name:             DefaultServiceName,
		Description:      "Go Proxy Server",
		ExecPath:         execPath,
		Args:             args,
		WorkingDirectory: workDir,
	}
}

// ValidateName rejects path-like or unsafe service names before filesystem use.
func ValidateName(name string) error {
	if !serviceNamePattern.MatchString(name) {
		return fmt.Errorf("invalid service name %q: use only letters, digits, dot, underscore, and dash", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("invalid service name %q: traversal sequences are not allowed", name)
	}
	return nil
}

func compactDetail(detail string) string {
	lines := strings.Split(detail, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
		if len(parts) == 4 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) < len(nonEmptyLines(lines)) {
		parts = append(parts, "...")
	}
	return strings.Join(parts, "; ")
}

func nonEmptyLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// NewManager returns the platform-specific service manager implementation.
func NewManager() Manager {
	return newManagerForRuntimeOS(runtime.GOOS)
}
