package web

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apeming/go-proxy-server/internal/config"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
	"github.com/apeming/go-proxy-server/internal/tunnel"
	"gorm.io/gorm"
)

const (
	defaultTunnelClassicListenAddr = ":7000"
	defaultTunnelQUICListenAddr    = ":7443"
	defaultTunnelPublicBind        = "0.0.0.0"
)

type tunnelServerConfig struct {
	Engine             string `json:"engine"`
	ListenAddr         string `json:"listenAddr"`
	PublicBind         string `json:"publicBind"`
	ClientEndpoint     string `json:"clientEndpoint"`
	Token              string `json:"token"`
	CertFile           string `json:"certFile"`
	KeyFile            string `json:"keyFile"`
	AllowInsecure      bool   `json:"allowInsecure"`
	AutoStart          bool   `json:"autoStart"`
	AutoPortRangeStart int    `json:"autoPortRangeStart"`
	AutoPortRangeEnd   int    `json:"autoPortRangeEnd"`
}

type tunnelServerConfigs struct {
	Classic tunnelServerConfig `json:"classic"`
	QUIC    tunnelServerConfig `json:"quic"`
}

type tunnelServerEngineStatus struct {
	Running          bool               `json:"running"`
	Engine           string             `json:"engine"`
	ActualListenAddr string             `json:"actualListenAddr"`
	LastError        string             `json:"lastError"`
	Config           tunnelServerConfig `json:"config"`
}

type tunnelServerStatusResponse struct {
	Classic      tunnelServerEngineStatus     `json:"classic"`
	QUIC         tunnelServerEngineStatus     `json:"quic"`
	Certificates tunnelServerCertificateState `json:"certificates"`
}

func defaultManagedTunnelServerConfig(engine string) tunnelServerConfig {
	cfg := tunnelServerConfig{
		Engine:     tunnelEngineOrDefault(engine),
		PublicBind: defaultTunnelPublicBind,
	}
	switch cfg.Engine {
	case tunnel.EngineQUIC:
		cfg.ListenAddr = defaultTunnelQUICListenAddr
	default:
		cfg.Engine = tunnel.EngineClassic
		cfg.ListenAddr = defaultTunnelClassicListenAddr
	}
	return cfg
}

func defaultManagedTunnelServerConfigs() tunnelServerConfigs {
	return tunnelServerConfigs{
		Classic: defaultManagedTunnelServerConfig(tunnel.EngineClassic),
		QUIC:    defaultManagedTunnelServerConfig(tunnel.EngineQUIC),
	}
}

func normalizeManagedTunnelServerConfig(cfg tunnelServerConfig) tunnelServerConfig {
	cfg.Engine = tunnelEngineOrDefault(cfg.Engine)
	if cfg.ListenAddr == "" {
		switch cfg.Engine {
		case tunnel.EngineQUIC:
			cfg.ListenAddr = defaultTunnelQUICListenAddr
		default:
			cfg.ListenAddr = defaultTunnelClassicListenAddr
		}
	}
	if cfg.PublicBind == "" {
		cfg.PublicBind = defaultTunnelPublicBind
	}
	return cfg
}

func normalizeManagedTunnelServerConfigs(cfgs tunnelServerConfigs) tunnelServerConfigs {
	cfgs.Classic = normalizeManagedTunnelServerConfig(cfgs.Classic)
	cfgs.Classic.Engine = tunnel.EngineClassic
	cfgs.QUIC = normalizeManagedTunnelServerConfig(cfgs.QUIC)
	cfgs.QUIC.Engine = tunnel.EngineQUIC
	return cfgs
}

func validateManagedTunnelServerConfig(cfg tunnelServerConfig) error {
	if err := validateTunnelEngine(cfg.Engine); err != nil {
		return err
	}
	if cfg.AllowInsecure && tunnelEngineOrDefault(cfg.Engine) == tunnel.EngineQUIC {
		return fmt.Errorf("quic tunnel server does not support insecure mode")
	}
	if cfg.AutoPortRangeStart == 0 && cfg.AutoPortRangeEnd == 0 {
		return nil
	}
	if cfg.AutoPortRangeStart <= 0 || cfg.AutoPortRangeEnd <= 0 {
		return fmt.Errorf("auto port range start and end must both be set")
	}
	if cfg.AutoPortRangeStart > 65535 || cfg.AutoPortRangeEnd > 65535 {
		return fmt.Errorf("auto port range must be within 1-65535")
	}
	if cfg.AutoPortRangeStart > cfg.AutoPortRangeEnd {
		return fmt.Errorf("auto port range start must be less than or equal to end")
	}
	return nil
}

// ValidateTunnelServerRuntimeConfig validates runtime tunnel-server settings before startup.
func ValidateTunnelServerRuntimeConfig(engine, token, cert, key string, allowInsecure bool, autoPortStart, autoPortEnd int) error {
	cfg := tunnelServerConfig{
		Engine:             strings.TrimSpace(engine),
		Token:              strings.TrimSpace(token),
		CertFile:           strings.TrimSpace(cert),
		KeyFile:            strings.TrimSpace(key),
		AllowInsecure:      allowInsecure,
		AutoPortRangeStart: autoPortStart,
		AutoPortRangeEnd:   autoPortEnd,
	}
	if err := validateManagedTunnelServerConfig(cfg); err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("tunnel token is required")
	}
	hasCert := cfg.CertFile != ""
	hasKey := cfg.KeyFile != ""
	if cfg.AllowInsecure && (hasCert || hasKey) {
		return fmt.Errorf("allow_insecure cannot be combined with cert or key")
	}
	if !cfg.AllowInsecure && (hasCert != hasKey) {
		return fmt.Errorf("tunnel server requires cert and key when explicit TLS paths are provided")
	}
	return nil
}

func (wm *Manager) loadManagedTunnelServerConfigs() (tunnelServerConfigs, error) {
	raw, err := config.GetSystemConfig(wm.db, config.KeyTunnelServerConfig)
	if err != nil {
		return tunnelServerConfigs{}, err
	}
	if raw == "" {
		return defaultManagedTunnelServerConfigs(), nil
	}

	var probe struct {
		Classic *tunnelServerConfig `json:"classic"`
		QUIC    *tunnelServerConfig `json:"quic"`
	}
	if err := json.Unmarshal([]byte(raw), &probe); err == nil {
		if probe.Classic != nil || probe.QUIC != nil {
			cfgs := defaultManagedTunnelServerConfigs()
			if err := json.Unmarshal([]byte(raw), &cfgs); err != nil {
				return tunnelServerConfigs{}, fmt.Errorf("decode tunnel server config: %w", err)
			}
			return normalizeManagedTunnelServerConfigs(cfgs), nil
		}
	}

	legacy := defaultManagedTunnelServerConfig(tunnel.EngineClassic)
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		return tunnelServerConfigs{}, fmt.Errorf("decode tunnel server config: %w", err)
	}
	legacy = normalizeManagedTunnelServerConfig(legacy)

	cfgs := defaultManagedTunnelServerConfigs()
	switch legacy.Engine {
	case tunnel.EngineQUIC:
		cfgs.QUIC = legacy
	default:
		cfgs.Classic = legacy
	}
	return normalizeManagedTunnelServerConfigs(cfgs), nil
}

func (wm *Manager) loadManagedTunnelServerConfig(engine string) (tunnelServerConfig, error) {
	cfgs, err := wm.loadManagedTunnelServerConfigs()
	if err != nil {
		return tunnelServerConfig{}, err
	}
	return cfgs.configForEngine(engine), nil
}

func (cfgs tunnelServerConfigs) configForEngine(engine string) tunnelServerConfig {
	switch tunnelEngineOrDefault(engine) {
	case tunnel.EngineQUIC:
		return cfgs.QUIC
	default:
		return cfgs.Classic
	}
}

func (cfgs *tunnelServerConfigs) setConfig(cfg tunnelServerConfig) {
	switch tunnelEngineOrDefault(cfg.Engine) {
	case tunnel.EngineQUIC:
		cfgs.QUIC = normalizeManagedTunnelServerConfig(cfg)
		cfgs.QUIC.Engine = tunnel.EngineQUIC
	default:
		cfgs.Classic = normalizeManagedTunnelServerConfig(cfg)
		cfgs.Classic.Engine = tunnel.EngineClassic
	}
}

func (cfgs tunnelServerConfigs) certificateConfig() tunnelServerConfig {
	// The web admin shares one certificate set across classic and QUIC tunnel servers.
	// If a legacy path-based certificate is present on either engine config, prefer the
	// first configured one so certificate inspection and CA download stay deterministic.
	for _, cfg := range []tunnelServerConfig{cfgs.Classic, cfgs.QUIC} {
		if cfg.CertFile != "" || cfg.KeyFile != "" {
			return cfg
		}
	}
	return cfgs.Classic
}

func (wm *Manager) saveManagedTunnelServerConfig(cfg tunnelServerConfig) error {
	cfg = normalizeManagedTunnelServerConfig(cfg)
	if err := validateManagedTunnelServerConfig(cfg); err != nil {
		return err
	}

	cfgs, err := wm.loadManagedTunnelServerConfigs()
	if err != nil {
		cfgs = defaultManagedTunnelServerConfigs()
	}
	existing := cfgs.configForEngine(cfg.Engine)
	if cfg.CertFile == "" {
		cfg.CertFile = existing.CertFile
	}
	if cfg.KeyFile == "" {
		cfg.KeyFile = existing.KeyFile
	}
	cfgs.setConfig(cfg)

	data, err := json.Marshal(cfgs)
	if err != nil {
		return fmt.Errorf("encode tunnel server config: %w", err)
	}
	return config.SetSystemConfig(wm.db, config.KeyTunnelServerConfig, string(data))
}

func (wm *Manager) managedTunnelServerStatusForEngine(engine string, cfg tunnelServerConfig) tunnelServerEngineStatus {
	engine = tunnelEngineOrDefault(engine)

	wm.mu.RLock()
	server := wm.tunnelServers[engine]
	lastError := wm.tunnelErrors[engine]
	wm.mu.RUnlock()

	status := tunnelServerEngineStatus{
		Running:   server != nil,
		Engine:    engine,
		LastError: lastError,
		Config:    cfg,
	}
	if server != nil {
		status.ActualListenAddr = server.GetControlAddr()
	}
	return status
}

func (wm *Manager) getManagedTunnelServerStatus() (tunnelServerStatusResponse, error) {
	cfgs, err := wm.loadManagedTunnelServerConfigs()
	if err != nil {
		return tunnelServerStatusResponse{}, err
	}
	certificates, err := wm.getTunnelServerCertificateState(cfgs.certificateConfig())
	if err != nil {
		return tunnelServerStatusResponse{}, err
	}

	return tunnelServerStatusResponse{
		Classic:      wm.managedTunnelServerStatusForEngine(tunnel.EngineClassic, cfgs.Classic),
		QUIC:         wm.managedTunnelServerStatusForEngine(tunnel.EngineQUIC, cfgs.QUIC),
		Certificates: certificates,
	}, nil
}

func (wm *Manager) startManagedTunnelServer(cfg tunnelServerConfig) error {
	cfg = normalizeManagedTunnelServerConfig(cfg)
	if err := validateManagedTunnelServerConfig(cfg); err != nil {
		return err
	}
	tlsConfig, err := wm.loadManagedTunnelServerTLSConfig(cfg)
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("tunnel token is required")
	}
	if err := wm.saveManagedTunnelServerConfig(cfg); err != nil {
		return err
	}

	engine := tunnelEngineOrDefault(cfg.Engine)

	wm.mu.Lock()
	if wm.tunnelServers[engine] != nil {
		wm.mu.Unlock()
		return fmt.Errorf("%s tunnel server is already running", engine)
	}

	ctx, cancel := context.WithCancel(wm.shutdownCtx)
	server, err := newManagedTunnelServerRuntime(wm.db, cfg, tlsConfig)
	if err != nil {
		cancel()
		wm.mu.Unlock()
		return err
	}
	if err := server.Start(ctx); err != nil {
		cancel()
		wm.mu.Unlock()
		return err
	}

	wm.tunnelServers[engine] = server
	wm.tunnelCancels[engine] = cancel
	wm.tunnelErrors[engine] = ""
	wm.mu.Unlock()

	go wm.watchManagedTunnelServer(engine, server)
	return nil
}

func (wm *Manager) watchManagedTunnelServer(engine string, server managedTunnelServerRuntime) {
	err := server.Wait()

	wm.mu.Lock()
	if wm.tunnelServers[engine] == server {
		delete(wm.tunnelServers, engine)
		delete(wm.tunnelCancels, engine)
		if err != nil {
			wm.tunnelErrors[engine] = err.Error()
		} else {
			wm.tunnelErrors[engine] = ""
		}
	}
	wm.mu.Unlock()

	if err != nil {
		applogger.Error("Managed %s tunnel server stopped unexpectedly: %v", engine, err)
	}
}

type tunnelServerStarter interface {
	managedTunnelServerRuntime
	Start(ctx context.Context) error
}

func newManagedTunnelServerRuntime(db *gorm.DB, cfg tunnelServerConfig, tlsConfig *tls.Config) (tunnelServerStarter, error) {
	switch tunnelEngineOrDefault(cfg.Engine) {
	case tunnel.EngineClassic:
		server := tunnel.NewManagedServer(db, cfg.ListenAddr, cfg.PublicBind, cfg.Token)
		server.TLSConfig = tlsConfig
		server.AllowInsecure = cfg.AllowInsecure
		server.AutoPortRangeStart = cfg.AutoPortRangeStart
		server.AutoPortRangeEnd = cfg.AutoPortRangeEnd
		return server, nil
	case tunnel.EngineQUIC:
		if cfg.AllowInsecure {
			return nil, fmt.Errorf("quic tunnel server does not support insecure mode")
		}
		server := tunnel.NewQUICManagedServer(db, cfg.ListenAddr, cfg.PublicBind, cfg.Token)
		server.TLSConfig = tlsConfig
		server.AutoPortRangeStart = cfg.AutoPortRangeStart
		server.AutoPortRangeEnd = cfg.AutoPortRangeEnd
		return server, nil
	default:
		return nil, fmt.Errorf("unsupported tunnel engine")
	}
}

func (wm *Manager) stopManagedTunnelServer(engine string) error {
	rawEngine := strings.TrimSpace(engine)
	if rawEngine == "" {
		wm.mu.RLock()
		cancels := make([]context.CancelFunc, 0, len(wm.tunnelCancels))
		for _, cancel := range wm.tunnelCancels {
			cancels = append(cancels, cancel)
		}
		wm.mu.RUnlock()
		for _, cancel := range cancels {
			if cancel != nil {
				cancel()
			}
		}
		return nil
	}

	switch strings.ToLower(rawEngine) {
	case tunnel.EngineClassic, tunnel.EngineQUIC:
		wm.mu.RLock()
		cancel := wm.tunnelCancels[strings.ToLower(rawEngine)]
		wm.mu.RUnlock()
		if cancel != nil {
			cancel()
		}
		return nil
	default:
		return fmt.Errorf("unknown tunnel engine: %s", rawEngine)
	}
}

func (wm *Manager) listManagedTunnelServers() []managedTunnelServerRuntime {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	servers := make([]managedTunnelServerRuntime, 0, len(wm.tunnelServers))
	for _, server := range wm.tunnelServers {
		servers = append(servers, server)
	}
	return servers
}

func (wm *Manager) StartConfiguredTunnelServer() {
	cfgs, err := wm.loadManagedTunnelServerConfigs()
	if err != nil {
		applogger.Error("Failed to load managed tunnel server config: %v", err)
		return
	}

	for _, cfg := range []tunnelServerConfig{cfgs.Classic, cfgs.QUIC} {
		if !cfg.AutoStart {
			continue
		}
		if err := wm.startManagedTunnelServer(cfg); err != nil {
			wm.mu.Lock()
			wm.tunnelErrors[tunnelEngineOrDefault(cfg.Engine)] = err.Error()
			wm.mu.Unlock()
			applogger.Error("Failed to auto-start managed %s tunnel server: %v", cfg.Engine, err)
		}
	}
}

// StartTunnelServerRuntime starts a managed tunnel server from runtime config.
func (wm *Manager) StartTunnelServerRuntime(engine, listen, publicBind, token, cert, key string, allowInsecure bool, autoPortStart, autoPortEnd int) error {
	if err := ValidateTunnelServerRuntimeConfig(engine, token, cert, key, allowInsecure, autoPortStart, autoPortEnd); err != nil {
		return err
	}
	return wm.startManagedTunnelServer(tunnelServerConfig{
		Engine:             engine,
		ListenAddr:         listen,
		PublicBind:         publicBind,
		Token:              token,
		CertFile:           cert,
		KeyFile:            key,
		AllowInsecure:      allowInsecure,
		AutoPortRangeStart: autoPortStart,
		AutoPortRangeEnd:   autoPortEnd,
	})
}
