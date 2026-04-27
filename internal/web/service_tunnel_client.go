package web

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/apeming/go-proxy-server/internal/config"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
	"github.com/apeming/go-proxy-server/internal/tunnel"
)

const managedTunnelClientNameMaxLength = 64

type managedTunnelClientConfig struct {
	Engine             string `json:"engine"`
	ServerAddr         string `json:"serverAddr"`
	ClientName         string `json:"clientName"`
	Token              string `json:"token"`
	CAFile             string `json:"caFile"`
	UseManagedServerCA bool   `json:"useManagedServerCa"`
	ServerName         string `json:"serverName"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	AllowInsecure      bool   `json:"allowInsecure"`
	AutoStart          bool   `json:"autoStart"`
}

type managedTunnelClientStatusResponse struct {
	Running                  bool                                `json:"running"`
	Engine                   string                              `json:"engine"`
	Connected                bool                                `json:"connected"`
	LastError                string                              `json:"lastError"`
	ConnectedAt              *time.Time                          `json:"connectedAt,omitempty"`
	EffectiveCAFile          string                              `json:"effectiveCaFile,omitempty"`
	ManagedServerCAAvailable bool                                `json:"managedServerCaAvailable"`
	Certificates             managedTunnelClientCertificateState `json:"certificates"`
	Config                   managedTunnelClientConfig           `json:"config"`
	Routes                   []tunnel.ManagedClientRoute         `json:"routes"`
}

func defaultManagedTunnelClientConfig() managedTunnelClientConfig {
	return managedTunnelClientConfig{Engine: tunnel.EngineClassic}
}

func normalizeManagedTunnelClientConfig(cfg managedTunnelClientConfig) managedTunnelClientConfig {
	cfg.Engine = tunnelEngineOrDefault(cfg.Engine)
	cfg.ServerAddr = strings.TrimSpace(cfg.ServerAddr)
	cfg.ClientName = strings.TrimSpace(cfg.ClientName)
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.CAFile = strings.TrimSpace(cfg.CAFile)
	cfg.ServerName = strings.TrimSpace(cfg.ServerName)
	return cfg
}

func validateManagedTunnelClientConfig(cfg managedTunnelClientConfig) error {
	if err := validateTunnelEngine(cfg.Engine); err != nil {
		return err
	}
	if cfg.AllowInsecure {
		return fmt.Errorf("managed tunnel client does not support insecure plaintext mode")
	}
	if cfg.InsecureSkipVerify {
		return fmt.Errorf("managed tunnel client does not support skipping hostname verification")
	}
	if cfg.ClientName != "" {
		if err := validateManagedTunnelClientName(cfg.ClientName); err != nil {
			return err
		}
	}
	if cfg.ServerAddr != "" {
		if err := validateManagedTunnelServerAddr(cfg.ServerAddr); err != nil {
			return err
		}
	}
	return nil
}

func validateManagedTunnelClientRuntimeConfig(cfg managedTunnelClientConfig) error {
	if err := validateManagedTunnelClientConfig(cfg); err != nil {
		return err
	}
	if cfg.ServerAddr == "" {
		return fmt.Errorf("tunnel server address is required")
	}
	if cfg.ClientName == "" {
		return fmt.Errorf("tunnel client name is required")
	}
	if cfg.Token == "" {
		return fmt.Errorf("tunnel token is required")
	}
	return nil
}

// ValidateTunnelClientRuntimeConfig validates runtime tunnel-client settings before startup.
func ValidateTunnelClientRuntimeConfig(engine, server, token, client, ca, serverName string, insecureSkipVerify, allowInsecure bool) error {
	if allowInsecure && (ca != "" || serverName != "" || insecureSkipVerify) {
		return fmt.Errorf("cannot combine -allow-insecure with TLS verification flags")
	}

	cfg := managedTunnelClientConfig{
		Engine:             strings.TrimSpace(engine),
		ServerAddr:         strings.TrimSpace(server),
		Token:              strings.TrimSpace(token),
		ClientName:         strings.TrimSpace(client),
		CAFile:             strings.TrimSpace(ca),
		ServerName:         strings.TrimSpace(serverName),
		InsecureSkipVerify: insecureSkipVerify,
		AllowInsecure:      allowInsecure,
	}
	if err := validateTunnelEngine(cfg.Engine); err != nil {
		return err
	}
	if cfg.ServerAddr == "" {
		return fmt.Errorf("tunnel server address is required")
	}
	if cfg.ClientName == "" {
		return fmt.Errorf("tunnel client name is required")
	}
	if cfg.Token == "" {
		return fmt.Errorf("tunnel token is required")
	}
	if err := validateManagedTunnelClientName(cfg.ClientName); err != nil {
		return err
	}
	if err := validateManagedTunnelServerAddr(cfg.ServerAddr); err != nil {
		return err
	}
	return nil
}

func (wm *Manager) loadManagedTunnelClientConfig() (managedTunnelClientConfig, error) {
	raw, err := config.GetSystemConfig(wm.db, config.KeyTunnelClientConfig)
	if err != nil {
		return managedTunnelClientConfig{}, err
	}
	if raw == "" {
		return defaultManagedTunnelClientConfig(), nil
	}

	cfg := defaultManagedTunnelClientConfig()
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return managedTunnelClientConfig{}, fmt.Errorf("decode tunnel client config: %w", err)
	}
	return normalizeManagedTunnelClientConfig(cfg), nil
}

func (wm *Manager) saveManagedTunnelClientConfig(cfg managedTunnelClientConfig) error {
	cfg = normalizeManagedTunnelClientConfig(cfg)
	if err := validateManagedTunnelClientConfig(cfg); err != nil {
		return err
	}
	return wm.saveManagedTunnelClientConfigNormalized(cfg)
}

func (wm *Manager) saveManagedTunnelClientConfigForRuntime(cfg managedTunnelClientConfig) error {
	cfg = normalizeManagedTunnelClientConfig(cfg)
	if err := validateTunnelEngine(cfg.Engine); err != nil {
		return err
	}
	if cfg.ServerAddr == "" {
		return fmt.Errorf("tunnel server address is required")
	}
	if cfg.ClientName == "" {
		return fmt.Errorf("tunnel client name is required")
	}
	if cfg.Token == "" {
		return fmt.Errorf("tunnel token is required")
	}
	return wm.saveManagedTunnelClientConfigNormalized(cfg)
}

func (wm *Manager) saveManagedTunnelClientConfigNormalized(cfg managedTunnelClientConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode tunnel client config: %w", err)
	}
	return config.SetSystemConfig(wm.db, config.KeyTunnelClientConfig, string(data))
}

func (wm *Manager) resolveManagedTunnelClientCAFile(cfg managedTunnelClientConfig) (string, bool, error) {
	if cfg.CAFile != "" {
		return cfg.CAFile, false, nil
	}
	if cfg.UseManagedServerCA {
		_, _, caPath, err := managedTunnelServerTLSPaths()
		if err != nil {
			return "", false, err
		}
		if !fileExists(caPath) {
			return "", false, fmt.Errorf("managed tunnel server CA file is not available")
		}
		return caPath, true, nil
	}
	caPath, err := wm.resolveManagedTunnelClientUploadedCA()
	if err != nil {
		return "", false, err
	}
	return caPath, false, nil
}

func (wm *Manager) loadManagedTunnelClientTLSConfig(cfg managedTunnelClientConfig) (*tls.Config, string, error) {
	if cfg.AllowInsecure {
		return nil, "", nil
	}
	caFile := cfg.CAFile
	if !cfg.InsecureSkipVerify {
		var err error
		caFile, _, err = wm.resolveManagedTunnelClientCAFile(cfg)
		if err != nil {
			return nil, "", err
		}
	}
	clientTLS, err := tunnel.LoadClientTLSConfig(cfg.ServerAddr, caFile, cfg.ServerName, cfg.InsecureSkipVerify, cfg.AllowInsecure)
	if err != nil {
		return nil, "", err
	}
	return clientTLS, caFile, nil
}

func (wm *Manager) getManagedTunnelClientStatus() (managedTunnelClientStatusResponse, error) {
	cfg, err := wm.loadManagedTunnelClientConfig()
	if err != nil {
		return managedTunnelClientStatusResponse{}, err
	}
	displayCfg := cfg
	displayCfg.AllowInsecure = false
	displayCfg.InsecureSkipVerify = false

	certificates, err := wm.getManagedTunnelClientCertificateState()
	if err != nil {
		return managedTunnelClientStatusResponse{}, err
	}

	effectiveCAFile := ""
	managedCAAvailable := false
	if _, _, err := wm.resolveManagedTunnelClientCAFile(managedTunnelClientConfig{UseManagedServerCA: true}); err == nil {
		managedCAAvailable = true
	}
	if cfg.CAFile != "" {
		effectiveCAFile = cfg.CAFile
	} else if cfg.UseManagedServerCA {
		if caFile, _, err := wm.resolveManagedTunnelClientCAFile(cfg); err == nil {
			effectiveCAFile = caFile
		}
	} else if certificates.Ready {
		if caFile, err := wm.resolveManagedTunnelClientUploadedCA(); err == nil {
			effectiveCAFile = caFile
		}
	}

	wm.mu.RLock()
	defer wm.mu.RUnlock()

	routes := make([]tunnel.ManagedClientRoute, 0, len(wm.tunnelClientRoutes))
	routes = append(routes, wm.tunnelClientRoutes...)
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].Name < routes[j].Name
	})

	status := managedTunnelClientStatusResponse{
		Running:                  wm.tunnelClient != nil,
		Engine:                   cfg.Engine,
		Connected:                wm.tunnelClientConnected,
		LastError:                wm.tunnelClientError,
		ConnectedAt:              cloneTimePtr(wm.tunnelClientConnectedAt),
		EffectiveCAFile:          effectiveCAFile,
		ManagedServerCAAvailable: managedCAAvailable,
		Certificates:             certificates,
		Config:                   displayCfg,
		Routes:                   routes,
	}
	return status, nil
}

func (wm *Manager) startManagedTunnelClient(cfg managedTunnelClientConfig) error {
	cfg = normalizeManagedTunnelClientConfig(cfg)
	if err := validateManagedTunnelClientRuntimeConfig(cfg); err != nil {
		return err
	}
	clientTLS, _, err := wm.loadManagedTunnelClientTLSConfig(cfg)
	if err != nil {
		applogger.Warn("Managed tunnel client TLS configuration failed for %s -> %s: %v", cfg.ClientName, cfg.ServerAddr, err)
		return err
	}
	return wm.startManagedTunnelClientWithTLS(cfg, clientTLS)
}

func (wm *Manager) startManagedTunnelClientRuntime(cfg managedTunnelClientConfig, clientTLS *tls.Config) error {
	cfg = normalizeManagedTunnelClientConfig(cfg)
	if err := validateTunnelEngine(cfg.Engine); err != nil {
		return err
	}
	if cfg.ServerAddr == "" {
		return fmt.Errorf("tunnel server address is required")
	}
	if cfg.ClientName == "" {
		return fmt.Errorf("tunnel client name is required")
	}
	if cfg.Token == "" {
		return fmt.Errorf("tunnel token is required")
	}
	if err := wm.saveManagedTunnelClientConfigForRuntime(cfg); err != nil {
		return err
	}
	return wm.startManagedTunnelClientWithTLS(cfg, clientTLS)
}

func (wm *Manager) startManagedTunnelClientWithTLS(cfg managedTunnelClientConfig, clientTLS *tls.Config) error {
	applogger.Info(
		"Managed tunnel client TLS ready: client=%s managed_ca=%t insecure_skip_verify=%t allow_insecure=%t",
		cfg.ClientName,
		cfg.UseManagedServerCA,
		cfg.InsecureSkipVerify,
		cfg.AllowInsecure,
	)
	if err := wm.saveManagedTunnelClientConfigNormalized(cfg); err != nil {
		return err
	}

	wm.mu.Lock()
	if wm.tunnelClient != nil {
		wm.mu.Unlock()
		return fmt.Errorf("tunnel client is already running")
	}

	ctx, cancel := context.WithCancel(wm.shutdownCtx)
	client := newManagedTunnelClientRuntime(cfg, clientTLS)
	client.setConnectedHandler(func(clientName string) {
		now := time.Now()
		wm.mu.Lock()
		defer wm.mu.Unlock()
		if wm.tunnelClient != client {
			return
		}
		wm.tunnelClientConnected = true
		wm.tunnelClientConnectedAt = &now
		wm.tunnelClientError = ""
	})
	client.setDisconnectedHandler(func(clientName string, err error) {
		wm.mu.Lock()
		defer wm.mu.Unlock()
		if wm.tunnelClient != client {
			return
		}
		wm.tunnelClientConnected = false
		wm.tunnelClientRoutes = nil
		if err != nil {
			wm.tunnelClientError = err.Error()
		}
	})
	client.setRoutesChangedHandler(func(clientName string, routes []tunnel.ManagedClientRoute) {
		wm.mu.Lock()
		defer wm.mu.Unlock()
		if wm.tunnelClient != client {
			return
		}
		wm.tunnelClientRoutes = append([]tunnel.ManagedClientRoute(nil), routes...)
	})

	wm.tunnelClient = client
	wm.tunnelClientCancel = cancel
	wm.tunnelClientEngine = cfg.Engine
	wm.tunnelClientError = ""
	wm.tunnelClientConnected = false
	wm.tunnelClientConnectedAt = nil
	wm.tunnelClientRoutes = nil
	applogger.Info(
		"Managed tunnel client starting: client=%s server=%s auto_start=%t managed_ca=%t insecure_skip_verify=%t allow_insecure=%t",
		cfg.ClientName,
		cfg.ServerAddr,
		cfg.AutoStart,
		cfg.UseManagedServerCA,
		cfg.InsecureSkipVerify,
		cfg.AllowInsecure,
	)
	go wm.watchManagedTunnelClient(ctx, client)
	wm.mu.Unlock()
	return nil
}

func (wm *Manager) watchManagedTunnelClient(ctx context.Context, client managedTunnelClientCallbacks) {
	err := client.Run(ctx)

	wm.mu.Lock()
	if wm.tunnelClient == client {
		wm.tunnelClient = nil
		wm.tunnelClientCancel = nil
		wm.tunnelClientEngine = ""
		wm.tunnelClientConnected = false
		wm.tunnelClientRoutes = nil
		if err != nil {
			wm.tunnelClientError = err.Error()
		} else if ctx.Err() == nil {
			wm.tunnelClientError = ""
		}
	}
	wm.mu.Unlock()

	if err != nil && ctx.Err() == nil {
		applogger.Error("Managed tunnel client stopped unexpectedly: %v", err)
	}
}

type managedTunnelClientCallbacks interface {
	managedTunnelClientRuntime
	setConnectedHandler(func(clientName string))
	setDisconnectedHandler(func(clientName string, err error))
	setRoutesChangedHandler(func(clientName string, routes []tunnel.ManagedClientRoute))
}

type classicManagedTunnelClient struct{ *tunnel.ManagedClient }

func (c classicManagedTunnelClient) setConnectedHandler(fn func(clientName string)) {
	c.OnConnected = fn
}

func (c classicManagedTunnelClient) setDisconnectedHandler(fn func(clientName string, err error)) {
	c.OnDisconnected = fn
}

func (c classicManagedTunnelClient) setRoutesChangedHandler(fn func(clientName string, routes []tunnel.ManagedClientRoute)) {
	c.OnRoutesChanged = fn
}

type quicManagedTunnelClient struct{ *tunnel.QUICManagedClient }

func (c quicManagedTunnelClient) setConnectedHandler(fn func(clientName string)) {
	c.OnConnected = fn
}

func (c quicManagedTunnelClient) setDisconnectedHandler(fn func(clientName string, err error)) {
	c.OnDisconnected = fn
}

func (c quicManagedTunnelClient) setRoutesChangedHandler(fn func(clientName string, routes []tunnel.ManagedClientRoute)) {
	c.OnRoutesChanged = fn
}

func newManagedTunnelClientRuntime(cfg managedTunnelClientConfig, tlsConfig *tls.Config) managedTunnelClientCallbacks {
	switch tunnelEngineOrDefault(cfg.Engine) {
	case tunnel.EngineQUIC:
		client := tunnel.NewQUICManagedClient(cfg.ServerAddr, cfg.Token, cfg.ClientName)
		client.TLSConfig = tlsConfig
		return quicManagedTunnelClient{client}
	default:
		client := tunnel.NewManagedClient(cfg.ServerAddr, cfg.Token, cfg.ClientName)
		client.TLSConfig = tlsConfig
		client.AllowInsecure = cfg.AllowInsecure
		return classicManagedTunnelClient{client}
	}
}

func (wm *Manager) stopManagedTunnelClient() error {
	wm.mu.RLock()
	cancel := wm.tunnelClientCancel
	running := wm.tunnelClient != nil
	wm.mu.RUnlock()

	if !running {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	return nil
}

func (wm *Manager) StartConfiguredTunnelClient() {
	cfg, err := wm.loadManagedTunnelClientConfig()
	if err != nil {
		applogger.Error("Failed to load managed tunnel client config: %v", err)
		return
	}
	if !cfg.AutoStart {
		return
	}
	if err := wm.startManagedTunnelClient(cfg); err != nil {
		wm.mu.Lock()
		wm.tunnelClientError = err.Error()
		wm.mu.Unlock()
		applogger.Error("Failed to auto-start managed tunnel client: %v", err)
	}
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func validateManagedTunnelServerAddr(value string) error {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("tunnel server address must be in host:port format")
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("tunnel server address host is required")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("tunnel server address port must be within 1-65535")
	}
	return nil
}

// StartTunnelClientRuntime starts a managed tunnel client from runtime config.
func (wm *Manager) StartTunnelClientRuntime(engine, server, token, client, ca, serverName string, insecureSkipVerify, allowInsecure bool) error {
	if err := ValidateTunnelClientRuntimeConfig(engine, server, token, client, ca, serverName, insecureSkipVerify, allowInsecure); err != nil {
		return err
	}
	cfg := managedTunnelClientConfig{
		Engine:             engine,
		ServerAddr:         server,
		Token:              token,
		ClientName:         client,
		CAFile:             ca,
		UseManagedServerCA: ca == "" && !insecureSkipVerify,
		ServerName:         serverName,
		InsecureSkipVerify: insecureSkipVerify,
		AllowInsecure:      allowInsecure,
	}
	if allowInsecure {
		return wm.startManagedTunnelClientRuntime(cfg, nil)
	}
	clientTLS, _, err := wm.loadManagedTunnelClientTLSConfig(cfg)
	if err != nil {
		return err
	}
	return wm.startManagedTunnelClientRuntime(cfg, clientTLS)
}

func validateManagedTunnelClientName(value string) error {
	if len(value) > managedTunnelClientNameMaxLength {
		return fmt.Errorf("tunnel client name must be %d characters or fewer", managedTunnelClientNameMaxLength)
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_':
		default:
			return fmt.Errorf("tunnel client name may only contain letters, numbers, dot, dash, and underscore")
		}
	}
	return nil
}
