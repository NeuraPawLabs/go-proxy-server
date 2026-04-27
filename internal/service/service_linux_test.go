//go:build linux

package service

import (
	"strings"
	"testing"
)

func TestLinuxUnitIncludesRunCommandAndConfigPath(t *testing.T) {
	spec := ServiceSpec{
		Name:             "go-proxy-server",
		Description:      "Go Proxy Server",
		ExecPath:         "/usr/local/bin/go-proxy-server",
		Args:             []string{"run", "-config", "/etc/go-proxy-server/config.toml"},
		WorkingDirectory: "/var/lib/go-proxy-server",
	}

	unit := renderSystemdUnit(spec)
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/go-proxy-server run -config /etc/go-proxy-server/config.toml") {
		t.Fatalf("unit missing run command: %q", unit)
	}
}

func TestValidateNameRejectsPathLikeAndUnsafeNames(t *testing.T) {
	cases := []string{
		"",
		"../go-proxy-server",
		"go/proxy",
		"go\\proxy",
		"go proxy",
		".hidden",
		"go..proxy",
	}
	for _, name := range cases {
		if err := ValidateName(name); err == nil {
			t.Fatalf("ValidateName(%q) = nil, want error", name)
		}
	}
}

func TestValidateNameAcceptsSafeLabels(t *testing.T) {
	cases := []string{
		"go-proxy-server",
		"go_proxy.server-1",
		"server1",
	}
	for _, name := range cases {
		if err := ValidateName(name); err != nil {
			t.Fatalf("ValidateName(%q) = %v, want nil", name, err)
		}
	}
}
