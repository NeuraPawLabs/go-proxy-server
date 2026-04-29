package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/apeming/go-proxy-server/internal/config"
	"github.com/apeming/go-proxy-server/internal/tunnel"
)

func TestDefaultConfigPathLinuxUsesXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	t.Setenv("HOME", "/tmp/home")

	got, err := defaultConfigPathForOS("linux")
	if err != nil {
		t.Fatalf("defaultConfigPathForOS: %v", err)
	}

	want := filepath.Join("/tmp/xdg", "go-proxy-server", "config.toml")
	if got != want {
		t.Fatalf("default config path = %q, want %q", got, want)
	}
}

func TestDefaultConfigPathLinuxFallsBackToHomeConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home")

	got, err := defaultConfigPathForOS("linux")
	if err != nil {
		t.Fatalf("defaultConfigPathForOS: %v", err)
	}

	want := filepath.Join("/tmp/home", ".config", "go-proxy-server", "config.toml")
	if got != want {
		t.Fatalf("default config path = %q, want %q", got, want)
	}
}

func TestDefaultConfigPathDarwinUsesApplicationSupport(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")

	got, err := defaultConfigPathForOS("darwin")
	if err != nil {
		t.Fatalf("defaultConfigPathForOS: %v", err)
	}

	want := filepath.Join("/tmp/home", "Library", "Application Support", "go-proxy-server", "config.toml")
	if got != want {
		t.Fatalf("default config path = %q, want %q", got, want)
	}
}

func TestLoadConfigParsesEnabledServices(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := `
[web]
enabled = true
port = 9090

[socks]
enabled = true
port = 1081
bind_listen = true

[[exit_bindings]]
name = "aliyun-eip-a"
ingress_local_ip = "172.16.0.10"
outbound_local_ip = "172.16.0.20"
`
	if err := os.WriteFile(path, []byte(strings.TrimSpace(data)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if !cfg.Web.Enabled || cfg.Web.Port != 9090 {
		t.Fatalf("unexpected web config: %+v", cfg.Web)
	}
	if !cfg.Socks.Enabled || cfg.Socks.Port != 1081 || !cfg.Socks.BindListen {
		t.Fatalf("unexpected socks config: %+v", cfg.Socks)
	}
	if cfg.HTTP.Enabled || cfg.HTTP.Port != 8081 {
		t.Fatalf("unexpected http defaults: %+v", cfg.HTTP)
	}
	if len(cfg.ExitBindings) != 1 {
		t.Fatalf("exit bindings length = %d, want 1", len(cfg.ExitBindings))
	}
	if cfg.ExitBindings[0].Name != "aliyun-eip-a" ||
		cfg.ExitBindings[0].IngressLocalIP != "172.16.0.10" ||
		cfg.ExitBindings[0].OutboundLocalIP != "172.16.0.20" {
		t.Fatalf("unexpected exit binding: %+v", cfg.ExitBindings[0])
	}
}

func TestLoadConfigAllowsEmptyRuntimeForOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	cfg = cfg.ApplyOverrides(Overrides{WebPort: intPtr(9090)})
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate after overrides: %v", err)
	}
	if !cfg.Web.Enabled || cfg.Web.Port != 9090 {
		t.Fatalf("unexpected web config after overrides: %+v", cfg.Web)
	}
}

func TestConfigDirForOSLinuxPrefersXDGConfigHomeWithoutHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	t.Setenv("HOME", "")

	got, err := appconfig.ConfigDirForOS("linux")
	if err != nil {
		t.Fatalf("ConfigDirForOS: %v", err)
	}

	want := filepath.Join("/tmp/xdg", "go-proxy-server")
	if got != want {
		t.Fatalf("config dir = %q, want %q", got, want)
	}
}

func TestConfigDirForOSWindowsPrefersAPPDATAWithoutHome(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	t.Setenv("HOME", "")

	got, err := appconfig.ConfigDirForOS("windows")
	if err != nil {
		t.Fatalf("ConfigDirForOS: %v", err)
	}

	want := filepath.Join(`C:\Users\test\AppData\Roaming`, "go-proxy-server")
	if got != want {
		t.Fatalf("config dir = %q, want %q", got, want)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.toml"))
	if err == nil || !strings.Contains(err.Error(), "runtime config not found") {
		t.Fatalf("expected missing runtime config error, got %v", err)
	}
}

func TestApplyOverridesCLIWinsOverTOML(t *testing.T) {
	cfg := Config{
		Web:   WebConfig{Enabled: false, Port: 8080},
		Socks: ProxyConfig{Enabled: true, Port: 1080, BindListen: false},
		HTTP:  ProxyConfig{Enabled: true, Port: 8081, BindListen: false},
	}

	overrides := Overrides{
		WebPort:         intPtr(9090),
		SocksPort:       intPtr(1180),
		SocksBindListen: boolPtr(true),
		HTTPPort:        intPtr(8282),
		HTTPBindListen:  boolPtr(true),
	}

	got := cfg.ApplyOverrides(overrides)

	if !got.Web.Enabled || got.Web.Port != 9090 {
		t.Fatalf("unexpected web config after overrides: %+v", got.Web)
	}
	if !got.Socks.Enabled || got.Socks.Port != 1180 || !got.Socks.BindListen {
		t.Fatalf("unexpected socks config after overrides: %+v", got.Socks)
	}
	if !got.HTTP.Enabled || got.HTTP.Port != 8282 || !got.HTTP.BindListen {
		t.Fatalf("unexpected http config after overrides: %+v", got.HTTP)
	}
}

func TestValidateRejectsEmptyRuntime(t *testing.T) {
	err := (Config{}).Validate()
	if err == nil || !strings.Contains(err.Error(), "no enabled services in runtime config") {
		t.Fatalf("expected no enabled services error, got %v", err)
	}
}

func TestValidateRejectsInvalidExitBindingIP(t *testing.T) {
	cfg := Config{
		Socks: ProxyConfig{Enabled: true, Port: 1080, BindListen: true},
		ExitBindings: []ExitBinding{
			{
				Name:            "bad",
				IngressLocalIP:  "172.16.0.10",
				OutboundLocalIP: "not-an-ip",
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "invalid outbound_local_ip") {
		t.Fatalf("expected invalid outbound_local_ip error, got %v", err)
	}
}

func TestValidateRejectsTunnelServerWithoutToken(t *testing.T) {
	cfg := Config{
		TunnelServer: TunnelServerConfig{
			Enabled:       true,
			AllowInsecure: true,
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "tunnel token is required") {
		t.Fatalf("expected tunnel token error, got %v", err)
	}
}

func TestValidateAllowsTunnelServerWithoutExplicitTLSPaths(t *testing.T) {
	cfg := Config{
		TunnelServer: TunnelServerConfig{
			Enabled: true,
			Token:   "secret",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected tunnel server config without explicit TLS paths to validate, got %v", err)
	}
}

func TestValidateRejectsTunnelServerUnsupportedEngine(t *testing.T) {
	cfg := Config{
		TunnelServer: TunnelServerConfig{
			Enabled: true,
			Engine:  "bogus",
			Token:   "secret",
			Cert:    "/tmp/server.crt",
			Key:     "/tmp/server.key",
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "unsupported tunnel server engine") {
		t.Fatalf("expected tunnel server engine error, got %v", err)
	}
}

func TestValidateRejectsTunnelServerInvalidAutoPortRange(t *testing.T) {
	cfg := Config{
		TunnelServer: TunnelServerConfig{
			Enabled:       true,
			Engine:        tunnel.EngineClassic,
			Token:         "secret",
			Cert:          "/tmp/server.crt",
			Key:           "/tmp/server.key",
			AutoPortStart: 32000,
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "auto port range start and end must both be set") {
		t.Fatalf("expected auto port range error, got %v", err)
	}
}

func TestValidateRejectsInsecureQUICServer(t *testing.T) {
	cfg := Config{
		TunnelServer: TunnelServerConfig{
			Enabled:       true,
			Engine:        tunnel.EngineQUIC,
			Token:         "secret",
			AllowInsecure: true,
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "quic tunnel server does not support insecure mode") {
		t.Fatalf("expected QUIC insecure-mode error, got %v", err)
	}
}

func TestValidateRejectsTunnelServerAllowInsecureWithTLSAssets(t *testing.T) {
	cfg := Config{
		TunnelServer: TunnelServerConfig{
			Enabled:       true,
			Token:         "secret",
			AllowInsecure: true,
			Cert:          "/tmp/server.crt",
			Key:           "/tmp/server.key",
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "allow_insecure cannot be combined with cert or key") {
		t.Fatalf("expected tunnel server insecure mode error, got %v", err)
	}
}

func TestValidateRejectsTunnelServerWithPartialTLSAssets(t *testing.T) {
	cfg := Config{
		TunnelServer: TunnelServerConfig{
			Enabled: true,
			Token:   "secret",
			Cert:    "/tmp/server.crt",
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "tunnel server requires cert and key when explicit TLS paths are provided") {
		t.Fatalf("expected tunnel server partial TLS path error, got %v", err)
	}
}

func TestValidateRejectsTunnelClientWithoutConnectionDetails(t *testing.T) {
	cfg := Config{
		TunnelClient: TunnelClientConfig{
			Enabled: true,
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "tunnel client requires server, token, and client") {
		t.Fatalf("expected tunnel client validation error, got %v", err)
	}
}

func TestValidateRejectsTunnelClientInsecureModeWithVerificationFields(t *testing.T) {
	cfg := Config{
		TunnelClient: TunnelClientConfig{
			Enabled:            true,
			AllowInsecure:      true,
			Server:             "127.0.0.1:7000",
			Token:              "secret",
			Client:             "client-a",
			CA:                 "/tmp/ca.pem",
			ServerName:         "example.com",
			InsecureSkipVerify: true,
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "allow_insecure cannot be combined with ca, server_name, or insecure_skip_verify") {
		t.Fatalf("expected tunnel client mode error, got %v", err)
	}
}

func TestValidateRejectsTunnelClientUnsupportedEngine(t *testing.T) {
	cfg := Config{
		TunnelClient: TunnelClientConfig{
			Enabled: true,
			Engine:  "bogus",
			Server:  "127.0.0.1:7000",
			Token:   "secret",
			Client:  "client-a",
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "unsupported tunnel client engine") {
		t.Fatalf("expected tunnel client engine error, got %v", err)
	}
}

func TestValidateRejectsTunnelClientInvalidAddress(t *testing.T) {
	cfg := Config{
		TunnelClient: TunnelClientConfig{
			Enabled: true,
			Engine:  tunnel.EngineClassic,
			Server:  "bad-address",
			Token:   "secret",
			Client:  "client-a",
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "tunnel server address must be in host:port format") {
		t.Fatalf("expected tunnel client address error, got %v", err)
	}
}

func TestValidateRejectsTunnelClientInvalidName(t *testing.T) {
	cfg := Config{
		TunnelClient: TunnelClientConfig{
			Enabled: true,
			Engine:  tunnel.EngineClassic,
			Server:  "127.0.0.1:7000",
			Token:   "secret",
			Client:  "bad name",
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "tunnel client name may only contain") {
		t.Fatalf("expected tunnel client name error, got %v", err)
	}
}

func TestValidateAcceptsSupportedTunnelEngines(t *testing.T) {
	cfg := Config{
		TunnelServer: TunnelServerConfig{
			Enabled: true,
			Engine:  tunnel.EngineClassic,
			Token:   "secret",
			Cert:    "/tmp/server.crt",
			Key:     "/tmp/server.key",
		},
		TunnelClient: TunnelClientConfig{
			Enabled: true,
			Engine:  tunnel.EngineQUIC,
			Server:  "127.0.0.1:7000",
			Token:   "secret",
			Client:  "client-a",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected supported tunnel engines to validate, got %v", err)
	}
}

func intPtr(v int) *int { return &v }

func boolPtr(v bool) *bool { return &v }
