package runtime

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"

	"github.com/BurntSushi/toml"

	appconfig "github.com/apeming/go-proxy-server/internal/config"
	"github.com/apeming/go-proxy-server/internal/tunnel"
	webvalidation "github.com/apeming/go-proxy-server/internal/web"
)

// Config describes the runtime TOML configuration.
type Config struct {
	Web          WebConfig          `toml:"web"`
	Socks        ProxyConfig        `toml:"socks"`
	HTTP         ProxyConfig        `toml:"http"`
	ExitBindings []ExitBinding      `toml:"exit_bindings"`
	TunnelServer TunnelServerConfig `toml:"tunnel_server"`
	TunnelClient TunnelClientConfig `toml:"tunnel_client"`
}

const defaultConfigFileContent = `[web]
enabled = false
port = 8080

[socks]
enabled = true
port = 1080
bind_listen = false

[http]
enabled = false
port = 8081
bind_listen = false

[tunnel_server]
enabled = false
engine = "classic"
listen = ":7000"
public_bind = "0.0.0.0"
token = ""
cert = ""
key = ""
allow_insecure = false
auto_port_start = 0
auto_port_end = 0

[tunnel_client]
enabled = false
engine = "classic"
server = ""
token = ""
client = ""
ca = ""
server_name = ""
insecure_skip_verify = false
allow_insecure = false
`

// WebConfig configures the built-in web service.
type WebConfig struct {
	Enabled bool `toml:"enabled"`
	Port    int  `toml:"port"`
}

// ProxyConfig configures the socks and http proxy services.
type ProxyConfig struct {
	Enabled    bool `toml:"enabled"`
	Port       int  `toml:"port"`
	BindListen bool `toml:"bind_listen"`
}

// ExitBinding maps a proxy ingress local IP to an outbound source IP.
type ExitBinding struct {
	Name            string `toml:"name"`
	IngressLocalIP  string `toml:"ingress_local_ip"`
	OutboundLocalIP string `toml:"outbound_local_ip"`
}

// TunnelServerConfig configures the tunnel server service.
type TunnelServerConfig struct {
	Enabled       bool   `toml:"enabled"`
	Engine        string `toml:"engine"`
	Listen        string `toml:"listen"`
	PublicBind    string `toml:"public_bind"`
	Token         string `toml:"token"`
	Cert          string `toml:"cert"`
	Key           string `toml:"key"`
	AllowInsecure bool   `toml:"allow_insecure"`
	AutoPortStart int    `toml:"auto_port_start"`
	AutoPortEnd   int    `toml:"auto_port_end"`
}

// TunnelClientConfig configures the tunnel client service.
type TunnelClientConfig struct {
	Enabled            bool   `toml:"enabled"`
	Engine             string `toml:"engine"`
	Server             string `toml:"server"`
	Token              string `toml:"token"`
	Client             string `toml:"client"`
	CA                 string `toml:"ca"`
	ServerName         string `toml:"server_name"`
	InsecureSkipVerify bool   `toml:"insecure_skip_verify"`
	AllowInsecure      bool   `toml:"allow_insecure"`
}

// Overrides contains CLI values that override TOML configuration.
type Overrides struct {
	WebPort         *int
	SocksPort       *int
	SocksBindListen *bool
	HTTPPort        *int
	HTTPBindListen  *bool
}

// DefaultConfig returns the built-in runtime defaults.
func DefaultConfig() Config {
	return Config{
		Web:   WebConfig{Port: 8080},
		Socks: ProxyConfig{Port: 1080},
		HTTP:  ProxyConfig{Port: 8081},
		TunnelServer: TunnelServerConfig{
			Engine:     "classic",
			Listen:     ":7000",
			PublicBind: "0.0.0.0",
		},
		TunnelClient: TunnelClientConfig{
			Engine: "classic",
		},
	}
}

// LoadConfig loads a TOML runtime config, using the default path when path is empty.
func LoadConfig(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return Config{}, fmt.Errorf("resolve default runtime config path: %w", err)
		}
	}

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("runtime config not found: %w", err)
		}
		return Config{}, fmt.Errorf("stat runtime config: %w", err)
	}

	cfg := DefaultConfig()
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode runtime config: %w", err)
	}

	return cfg, nil
}

// EnsureDefaultConfigFile writes a runnable default runtime config if path is missing.
func EnsureDefaultConfigFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("runtime config path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create runtime config directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			info, statErr := os.Stat(path)
			if statErr != nil {
				return fmt.Errorf("runtime config path is not usable: %w", statErr)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("runtime config path exists but is not a regular file: %s", path)
			}
			return nil
		}
		return fmt.Errorf("create runtime config: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString(defaultConfigFileContent); err != nil {
		return fmt.Errorf("write runtime config: %w", err)
	}
	return nil
}

// ApplyOverrides merges CLI override values on top of a loaded config.
func (cfg Config) ApplyOverrides(overrides Overrides) Config {
	out := cfg
	if overrides.WebPort != nil {
		out.Web.Enabled = true
		out.Web.Port = *overrides.WebPort
	}
	if overrides.SocksPort != nil {
		out.Socks.Enabled = true
		out.Socks.Port = *overrides.SocksPort
	}
	if overrides.SocksBindListen != nil {
		out.Socks.Enabled = true
		out.Socks.BindListen = *overrides.SocksBindListen
	}
	if overrides.HTTPPort != nil {
		out.HTTP.Enabled = true
		out.HTTP.Port = *overrides.HTTPPort
	}
	if overrides.HTTPBindListen != nil {
		out.HTTP.Enabled = true
		out.HTTP.BindListen = *overrides.HTTPBindListen
	}
	return out
}

// Validate rejects a runtime config that does not enable any services.
func (cfg Config) Validate() error {
	if !cfg.Web.Enabled && !cfg.Socks.Enabled && !cfg.HTTP.Enabled && !cfg.TunnelServer.Enabled && !cfg.TunnelClient.Enabled {
		return fmt.Errorf("no enabled services in runtime config")
	}
	if err := validateExitBindings(cfg.ExitBindings); err != nil {
		return err
	}
	if cfg.TunnelServer.Enabled {
		if err := validateTunnelEngine("tunnel server", cfg.TunnelServer.Engine); err != nil {
			return err
		}
		if err := webvalidation.ValidateTunnelServerRuntimeConfig(
			cfg.TunnelServer.Engine,
			cfg.TunnelServer.Token,
			cfg.TunnelServer.Cert,
			cfg.TunnelServer.Key,
			cfg.TunnelServer.AllowInsecure,
			cfg.TunnelServer.AutoPortStart,
			cfg.TunnelServer.AutoPortEnd,
		); err != nil {
			return err
		}
	}
	if cfg.TunnelClient.Enabled {
		if err := validateTunnelEngine("tunnel client", cfg.TunnelClient.Engine); err != nil {
			return err
		}
		if err := webvalidation.ValidateTunnelClientRuntimeConfig(
			cfg.TunnelClient.Engine,
			cfg.TunnelClient.Server,
			cfg.TunnelClient.Token,
			cfg.TunnelClient.Client,
			cfg.TunnelClient.CA,
			cfg.TunnelClient.ServerName,
			cfg.TunnelClient.InsecureSkipVerify,
			cfg.TunnelClient.AllowInsecure,
		); err != nil {
			switch {
			case strings.Contains(err.Error(), "tunnel server address is required"),
				strings.Contains(err.Error(), "tunnel client name is required"),
				strings.Contains(err.Error(), "tunnel token is required"):
				return fmt.Errorf("tunnel client requires server, token, and client")
			case strings.Contains(err.Error(), "cannot combine -allow-insecure with TLS verification flags"):
				return fmt.Errorf("allow_insecure cannot be combined with ca, server_name, or insecure_skip_verify")
			default:
				return err
			}
		}
	}
	return nil
}

func validateExitBindings(bindings []ExitBinding) error {
	seen := make(map[string]struct{}, len(bindings))
	for i, binding := range bindings {
		ingress := strings.TrimSpace(binding.IngressLocalIP)
		outbound := strings.TrimSpace(binding.OutboundLocalIP)
		if net.ParseIP(ingress) == nil {
			return fmt.Errorf("exit_bindings[%d] invalid ingress_local_ip: %q", i, binding.IngressLocalIP)
		}
		ip := net.ParseIP(outbound)
		if ip == nil || ip.IsUnspecified() {
			return fmt.Errorf("exit_bindings[%d] invalid outbound_local_ip: %q", i, binding.OutboundLocalIP)
		}
		if _, ok := seen[ingress]; ok {
			return fmt.Errorf("exit_bindings[%d] duplicate ingress_local_ip: %q", i, binding.IngressLocalIP)
		}
		seen[ingress] = struct{}{}
	}
	return nil
}

func validateTunnelEngine(kind, engine string) error {
	switch strings.TrimSpace(engine) {
	case "", tunnel.EngineClassic, tunnel.EngineQUIC:
		return nil
	default:
		return fmt.Errorf("unsupported %s engine: %s", kind, engine)
	}
}

// DefaultConfigPath returns the OS-specific default runtime config path.
func DefaultConfigPath() (string, error) {
	return defaultConfigPathForOS(stdruntime.GOOS)
}

func defaultConfigPathForOS(goos string) (string, error) {
	configDir, err := appconfig.ConfigDirForOS(goos)
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "config.toml"), nil
}
