package web

import (
	"fmt"

	"github.com/apeming/go-proxy-server/internal/config"
	"github.com/apeming/go-proxy-server/internal/models"
)

func (wm *Manager) getProxyServer(proxyType string) (*ProxyServer, error) {
	switch proxyType {
	case "socks5":
		return wm.socksServer, nil
	case "http":
		return wm.httpServer, nil
	default:
		return nil, errInvalidProxyType(proxyType)
	}
}

func (wm *Manager) proxyServers() []*ProxyServer {
	return []*ProxyServer{wm.socksServer, wm.httpServer}
}

func newProxyServer(proxyType string) *ProxyServer {
	return &ProxyServer{Type: proxyType}
}

func (wm *Manager) loadProxyRuntimeState(server *ProxyServer) {
	proxyConfig, err := config.LoadProxyConfig(wm.db, server.Type)
	if err != nil || proxyConfig == nil {
		return
	}

	server.applyConfig(proxyConfig)
}

func (wm *Manager) updateProxyConfig(server *ProxyServer, req proxyConfigRequest) error {
	server.AutoStart = req.AutoStart
	if !server.Running.Load() {
		server.Port = req.Port
		server.BindListen = req.BindListen
	}

	return wm.saveProxyConfig(server)
}

func (wm *Manager) saveProxyConfig(server *ProxyServer) error {
	return config.SaveProxyConfig(wm.db, &models.ProxyConfig{
		Type:       server.Type,
		Port:       server.Port,
		BindListen: server.BindListen,
		AutoStart:  server.AutoStart,
	})
}

func (server *ProxyServer) applyConfig(proxyConfig *models.ProxyConfig) {
	server.Port = proxyConfig.Port
	server.BindListen = proxyConfig.BindListen
	server.AutoStart = proxyConfig.AutoStart
}

func errInvalidProxyType(proxyType string) error {
	return fmt.Errorf("Invalid proxy type: %s", proxyType)
}
