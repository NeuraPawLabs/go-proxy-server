package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/auth"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
	"github.com/apeming/go-proxy-server/internal/proxy"
	"github.com/apeming/go-proxy-server/internal/tunnel"
)

// ProxyServer represents a running proxy server
type ProxyServer struct {
	Type       string // "socks5" or "http"
	Port       int
	BindListen bool
	AutoStart  bool // Whether to auto-start on application launch
	Listener   net.Listener
	Running    atomic.Bool
}

type managedTunnelServerRuntime interface {
	Wait() error
	ListActiveSessions() []tunnel.ManagedSessionSnapshot
	GetControlAddr() string
	Engine() string
}

type managedTunnelClientRuntime interface {
	Run(ctx context.Context) error
}

// Manager manages the web interface and proxy servers
type Manager struct {
	db                      *gorm.DB
	socksServer             *ProxyServer
	httpServer              *ProxyServer
	mu                      sync.RWMutex
	authReloadOnce          sync.Once
	adminAuth               *adminAuthManager
	adminBootstrapToken     string
	tunnelServers           map[string]managedTunnelServerRuntime
	tunnelCancels           map[string]context.CancelFunc
	tunnelErrors            map[string]string
	tunnelClient            managedTunnelClientRuntime
	tunnelClientCancel      context.CancelFunc
	tunnelClientEngine      string
	tunnelClientError       string
	tunnelClientConnected   bool
	tunnelClientConnectedAt *time.Time
	tunnelClientRoutes      []tunnel.ManagedClientRoute
	webPort                 int
	actualPort              int // Actual port being used (after binding)
	webHttpServer           *http.Server
	shutdownCtx             context.Context
	shutdownCancel          context.CancelFunc
}

// NewManager creates a new web manager
func NewManager(db *gorm.DB, webPort int) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	manager := &Manager{
		db:             db,
		webPort:        webPort,
		shutdownCtx:    ctx,
		shutdownCancel: cancel,
		adminAuth:      newAdminAuthManager(),
		socksServer:    newProxyServer("socks5"),
		httpServer:     newProxyServer("http"),
		tunnelServers:  make(map[string]managedTunnelServerRuntime),
		tunnelCancels:  make(map[string]context.CancelFunc),
		tunnelErrors:   make(map[string]string),
	}

	for _, server := range manager.proxyServers() {
		manager.loadProxyRuntimeState(server)
	}

	return manager
}

// SyncAuthState reloads credentials and whitelist state required by running proxies.
func (wm *Manager) SyncAuthState() error {
	return auth.SyncState(wm.db)
}

// StartConfiguredProxies starts proxies marked as AutoStart in the persisted configuration.
func (wm *Manager) StartConfiguredProxies() {
	for _, server := range wm.proxyServers() {
		wm.startConfiguredProxy(server)
	}
}

func (wm *Manager) startConfiguredProxy(server *ProxyServer) {
	if !server.AutoStart {
		return
	}

	if err := wm.startProxy(server, server.Port, server.BindListen); err != nil {
		applogger.Error("Failed to auto-start %s proxy: %v", server.Type, err)
	}
}

// startProxy starts a proxy server
func (wm *Manager) startProxy(server *ProxyServer, port int, bindListen bool) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}

	server.Port = port
	server.BindListen = bindListen
	server.Listener = listener
	server.Running.Store(true)

	if err := wm.saveProxyConfig(server); err != nil {
		applogger.Warn("Failed to save %s proxy config to database: %v", server.Type, err)
	}

	wm.authReloadOnce.Do(func() {
		auth.StartStateReloader(wm.shutdownCtx, wm.db, 10*time.Second, nil)
	})

	// Start accepting connections
	go func() {
		for server.Running.Load() {
			conn, err := listener.Accept()
			if err != nil {
				if server.Running.Load() {
					applogger.Error("%s proxy accept error: %v", server.Type, err)
				}
				continue
			}

			if server.Type == "socks5" {
				go proxy.HandleSocks5Connection(conn, bindListen)
			} else if server.Type == "http" {
				go proxy.HandleHTTPConnection(conn, bindListen)
			}
		}
	}()

	applogger.Info("%s proxy started on port %d", server.Type, port)
	return nil
}

// stopProxy stops a running proxy server
func (wm *Manager) stopProxy(server *ProxyServer) {
	server.Running.Store(false)
	if server.Listener != nil {
		server.Listener.Close()
	}
	applogger.Info("%s proxy stopped", server.Type)
}

// AutoStartProxy starts a proxy server automatically on application launch
func (wm *Manager) AutoStartProxy(proxyType string, port int, bindListen bool) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	server, err := wm.getProxyServer(proxyType)
	if err != nil {
		return fmt.Errorf("invalid proxy type: %s", proxyType)
	}

	if server.Running.Load() {
		return fmt.Errorf("%s proxy is already running", proxyType)
	}

	return wm.startProxy(server, port, bindListen)
}

// GetActualPort returns the actual port being used by the web server
func (wm *Manager) GetActualPort() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	return wm.actualPort
}

// SetActualPort sets the actual port being used by the web server
func (wm *Manager) SetActualPort(port int) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.actualPort = port
}

// StopAllProxies stops all running proxy servers
func (wm *Manager) StopAllProxies() {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	for _, server := range wm.proxyServers() {
		if server.Running.Load() {
			wm.stopProxy(server)
		}
	}
}

// Shutdown gracefully shuts down the web server
func (wm *Manager) Shutdown() error {
	// Cancel the shutdown context to stop all goroutines
	if wm.shutdownCancel != nil {
		wm.shutdownCancel()
	}

	// Shutdown the HTTP server gracefully
	if wm.webHttpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return wm.webHttpServer.Shutdown(ctx)
	}

	return nil
}

// ShutdownApplication gracefully shuts down the entire application
func (wm *Manager) ShutdownApplication() error {
	if err := wm.stopManagedTunnelClient(); err != nil {
		return err
	}
	if err := wm.stopManagedTunnelServer(""); err != nil {
		return err
	}

	// Stop all proxy servers first
	wm.StopAllProxies()

	// Close all HTTP transport connections
	proxy.CloseAllTransports()

	// Shutdown the web server
	if err := wm.Shutdown(); err != nil {
		return err
	}

	return nil
}
