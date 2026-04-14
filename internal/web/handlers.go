package web

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	applogger "github.com/apeming/go-proxy-server/internal/logger"
)

// StartServer starts the web management server
func (wm *Manager) StartServer() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/admin/session", wm.handleAdminSession)
	mux.HandleFunc("/api/admin/bootstrap", wm.handleAdminBootstrap)
	mux.HandleFunc("/api/admin/login", wm.handleAdminLogin)
	mux.HandleFunc("/api/admin/logout", wm.handleAdminLogout)
	mux.HandleFunc("/api/status", wm.requireAdminAuth(wm.handleStatus))
	mux.HandleFunc("/api/users", wm.requireAdminAuth(wm.handleUsers))
	mux.HandleFunc("/api/whitelist", wm.requireAdminAuth(wm.handleWhitelist))
	mux.HandleFunc("/api/proxy/start", wm.requireAdminAuth(wm.handleProxyStart))
	mux.HandleFunc("/api/proxy/stop", wm.requireAdminAuth(wm.handleProxyStop))
	mux.HandleFunc("/api/proxy/config", wm.requireAdminAuth(wm.handleProxyConfig))
	mux.HandleFunc("/api/config", wm.requireAdminAuth(wm.handleConfig))
	mux.HandleFunc("/api/tunnel/server", wm.requireAdminAuth(wm.handleTunnelServerStatus))
	mux.HandleFunc("/api/tunnel/server/config", wm.requireAdminAuth(wm.handleTunnelServerConfig))
	mux.HandleFunc("/api/tunnel/server/start", wm.requireAdminAuth(wm.handleTunnelServerStart))
	mux.HandleFunc("/api/tunnel/server/stop", wm.requireAdminAuth(wm.handleTunnelServerStop))
	mux.HandleFunc("/api/tunnel/server/certificates/upload", wm.requireAdminAuth(wm.handleTunnelServerUploadCertificates))
	mux.HandleFunc("/api/tunnel/server/certificates/generate", wm.requireAdminAuth(wm.handleTunnelServerGenerateCertificates))
	mux.HandleFunc("/api/tunnel/server/files/client-ca", wm.requireAdminAuth(wm.handleTunnelServerDownloadClientCA))
	mux.HandleFunc("/api/tunnel/client", wm.requireAdminAuth(wm.handleTunnelClientStatus))
	mux.HandleFunc("/api/tunnel/client/config", wm.requireAdminAuth(wm.handleTunnelClientConfig))
	mux.HandleFunc("/api/tunnel/client/ca", wm.requireAdminAuth(wm.handleTunnelClientCAUpload))
	mux.HandleFunc("/api/tunnel/client/start", wm.requireAdminAuth(wm.handleTunnelClientStart))
	mux.HandleFunc("/api/tunnel/client/stop", wm.requireAdminAuth(wm.handleTunnelClientStop))
	mux.HandleFunc("/api/tunnel/clients", wm.requireAdminAuth(wm.handleTunnelClients))
	mux.HandleFunc("/api/tunnel/routes", wm.requireAdminAuth(wm.handleTunnelRoutes))
	mux.HandleFunc("/api/tunnel/sessions", wm.requireAdminAuth(wm.handleTunnelSessions))
	mux.HandleFunc("/api/metrics/realtime", wm.requireAdminAuth(wm.handleMetricsRealtime))
	mux.HandleFunc("/api/metrics/history", wm.requireAdminAuth(wm.handleMetricsHistory))
	mux.HandleFunc("/api/logs/audit", wm.requireAdminAuth(wm.handleAuditLogs))
	mux.HandleFunc("/api/logs/events", wm.requireAdminAuth(wm.handleEventLogs))
	mux.HandleFunc("/api/shutdown", wm.requireAdminAuth(wm.handleShutdown))
	mux.HandleFunc("/", wm.handleIndex)

	addr := fmt.Sprintf("localhost:%d", wm.webPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start web server: %w", err)
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	wm.SetActualPort(actualPort)

	applogger.Info("Web management interface started at http://localhost:%d", actualPort)
	applogger.Info("Open your browser and visit: http://localhost:%d", actualPort)

	wm.webHttpServer = &http.Server{Handler: mux}
	if err := wm.webHttpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

func (wm *Manager) handleIndex(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	staticFS, err := GetStaticFS()
	if err != nil {
		http.Error(w, "Failed to load static files", http.StatusInternalServerError)
		return
	}

	fileServer := http.FileServer(http.FS(staticFS))
	if r.URL.Path != "/" {
		if _, err := staticFS.Open(strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			r.URL.Path = "/"
		}
	}

	fileServer.ServeHTTP(w, r)
}

func (wm *Manager) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	wm.mu.RLock()
	defer wm.mu.RUnlock()

	writeJSON(w, http.StatusOK, wm.buildStatusResponse())
}
