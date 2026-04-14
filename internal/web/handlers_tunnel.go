package web

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/apeming/go-proxy-server/internal/activity"
	"github.com/apeming/go-proxy-server/internal/tunnel"
)

type tunnelRouteUpsertRequest struct {
	ClientName        string   `json:"clientName"`
	Name              string   `json:"name"`
	Protocol          string   `json:"protocol"`
	TargetAddr        string   `json:"targetAddr"`
	PublicPort        int      `json:"publicPort"`
	IPWhitelist       []string `json:"ipWhitelist"`
	UDPIdleTimeoutSec int      `json:"udpIdleTimeoutSec"`
	UDPMaxPayload     int      `json:"udpMaxPayload"`
	Enabled           bool     `json:"enabled"`
}

type tunnelRouteDeleteRequest struct {
	ClientName string `json:"clientName"`
	Name       string `json:"name"`
}

type tunnelClientDeleteRequest struct {
	ClientName string `json:"clientName"`
}

type tunnelServerStartRequest struct {
	Engine             string `json:"engine"`
	ListenAddr         string `json:"listenAddr"`
	PublicBind         string `json:"publicBind"`
	ClientEndpoint     string `json:"clientEndpoint"`
	Token              string `json:"token"`
	AutoStart          bool   `json:"autoStart"`
	AutoPortRangeStart int    `json:"autoPortRangeStart"`
	AutoPortRangeEnd   int    `json:"autoPortRangeEnd"`
}

type tunnelServerStopRequest struct {
	Engine string `json:"engine"`
}

type tunnelServerGenerateCertificatesRequest struct {
	CommonName string   `json:"commonName"`
	Hosts      []string `json:"hosts"`
	ValidDays  int      `json:"validDays"`
}

type tunnelClientConfigRequest struct {
	Engine             string `json:"engine"`
	ServerAddr         string `json:"serverAddr"`
	ClientName         string `json:"clientName"`
	Token              string `json:"token"`
	UseManagedServerCA bool   `json:"useManagedServerCa"`
	ServerName         string `json:"serverName"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	AllowInsecure      bool   `json:"allowInsecure"`
	AutoStart          bool   `json:"autoStart"`
}

func (wm *Manager) handleTunnelServerStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	status, err := wm.getManagedTunnelServerStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (wm *Manager) handleTunnelServerStart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req tunnelServerStartRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := wm.startManagedTunnelServer(tunnelServerConfig{
		Engine:             req.Engine,
		ListenAddr:         req.ListenAddr,
		PublicBind:         req.PublicBind,
		ClientEndpoint:     req.ClientEndpoint,
		Token:              req.Token,
		AutoStart:          req.AutoStart,
		AutoPortRangeStart: req.AutoPortRangeStart,
		AutoPortRangeEnd:   req.AutoPortRangeEnd,
	}); err != nil {
		wm.recordAudit(r, "tunnel.server.start", "tunnel_server", req.ListenAddr, activity.AuditStatusFailure, "Failed to start managed tunnel server", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wm.recordAudit(r, "tunnel.server.start", "tunnel_server", req.ListenAddr, activity.AuditStatusSuccess, "Managed tunnel server started", map[string]any{"publicBind": req.PublicBind, "clientEndpoint": req.ClientEndpoint, "autoStart": req.AutoStart, "autoPortRangeStart": req.AutoPortRangeStart, "autoPortRangeEnd": req.AutoPortRangeEnd})

	status, err := wm.getManagedTunnelServerStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (wm *Manager) handleTunnelServerStop(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req tunnelServerStopRequest
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}

	engine := tunnelEngineOrDefault(req.Engine)
	if req.Engine == "" {
		engine = "all"
	}
	if err := wm.stopManagedTunnelServer(req.Engine); err != nil {
		wm.recordAudit(r, "tunnel.server.stop", "tunnel_server", engine, activity.AuditStatusFailure, "Failed to stop managed tunnel server", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wm.recordAudit(r, "tunnel.server.stop", "tunnel_server", engine, activity.AuditStatusSuccess, "Managed tunnel server stopped", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func (wm *Manager) handleTunnelServerConfig(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req tunnelServerStartRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := wm.saveManagedTunnelServerConfig(tunnelServerConfig{
		Engine:             req.Engine,
		ListenAddr:         req.ListenAddr,
		PublicBind:         req.PublicBind,
		ClientEndpoint:     req.ClientEndpoint,
		Token:              req.Token,
		AutoStart:          req.AutoStart,
		AutoPortRangeStart: req.AutoPortRangeStart,
		AutoPortRangeEnd:   req.AutoPortRangeEnd,
	}); err != nil {
		wm.recordAudit(r, "tunnel.server.save_config", "tunnel_server", req.ListenAddr, activity.AuditStatusFailure, "Failed to save managed tunnel server configuration", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wm.recordAudit(r, "tunnel.server.save_config", "tunnel_server", req.ListenAddr, activity.AuditStatusSuccess, "Managed tunnel server configuration saved", map[string]any{"publicBind": req.PublicBind, "clientEndpoint": req.ClientEndpoint, "autoStart": req.AutoStart, "autoPortRangeStart": req.AutoPortRangeStart, "autoPortRangeEnd": req.AutoPortRangeEnd})

	status, err := wm.getManagedTunnelServerStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (wm *Manager) handleTunnelServerUploadCertificates(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, fmt.Sprintf("parse multipart form: %v", err), http.StatusBadRequest)
		return
	}

	serverCertName, serverCertPEM, err := readMultipartFile(r, "serverCert", true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	serverKeyName, serverKeyPEM, err := readMultipartFile(r, "serverKey", true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	clientCAName, clientCAPEM, err := readMultipartFile(r, "clientCa", false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := wm.storeManagedTunnelServerCertificates(tunnelServerCertificateUpload{
		ServerCertName: serverCertName,
		ServerCertPEM:  serverCertPEM,
		ServerKeyName:  serverKeyName,
		ServerKeyPEM:   serverKeyPEM,
		ClientCAName:   nonEmpty(clientCAName, managedTunnelClientCAPEM),
		ClientCAPEM:    cloneOrDefaultCAPEM(serverCertPEM, clientCAPEM),
	}, tunnelCertificateSourceUploaded); err != nil {
		wm.recordAudit(r, "tunnel.server.upload_certificates", "tunnel_server", "managed", activity.AuditStatusFailure, "Failed to upload managed tunnel certificates", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wm.recordAudit(r, "tunnel.server.upload_certificates", "tunnel_server", "managed", activity.AuditStatusSuccess, "Managed tunnel certificates uploaded", map[string]any{"serverCert": serverCertName, "serverKey": serverKeyName, "clientCA": clientCAName})

	status, err := wm.getManagedTunnelServerStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (wm *Manager) handleTunnelServerGenerateCertificates(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req tunnelServerGenerateCertificatesRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	generated, err := generateTunnelServerCertificates(req.CommonName, req.Hosts, req.ValidDays)
	if err != nil {
		wm.recordAudit(r, "tunnel.server.generate_certificates", "tunnel_server", "managed", activity.AuditStatusFailure, "Failed to generate managed tunnel certificates", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := wm.storeManagedTunnelServerCertificates(tunnelServerCertificateUpload{
		ServerCertName: managedTunnelServerCertPEM,
		ServerCertPEM:  generated.ServerCertPEM,
		ServerKeyName:  managedTunnelServerKeyPEM,
		ServerKeyPEM:   generated.ServerKeyPEM,
		ClientCAName:   managedTunnelClientCAPEM,
		ClientCAPEM:    generated.ClientCAPEM,
	}, tunnelCertificateSourceGenerated); err != nil {
		wm.recordAudit(r, "tunnel.server.generate_certificates", "tunnel_server", "managed", activity.AuditStatusFailure, "Failed to store generated managed tunnel certificates", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wm.recordAudit(r, "tunnel.server.generate_certificates", "tunnel_server", "managed", activity.AuditStatusSuccess, "Managed tunnel certificates generated", map[string]any{"commonName": req.CommonName, "hosts": req.Hosts, "validDays": req.ValidDays})

	status, err := wm.getManagedTunnelServerStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (wm *Manager) handleTunnelServerDownloadClientCA(w http.ResponseWriter, r *http.Request) {
	wm.handleTunnelServerFileDownload(w, r, "client-ca")
}

func (wm *Manager) handleTunnelClientStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	status, err := wm.getManagedTunnelClientStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (wm *Manager) handleTunnelClientConfig(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req tunnelClientConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	cfg := managedTunnelClientConfig{
		Engine:             req.Engine,
		ServerAddr:         req.ServerAddr,
		ClientName:         req.ClientName,
		Token:              req.Token,
		UseManagedServerCA: req.UseManagedServerCA,
		ServerName:         req.ServerName,
		InsecureSkipVerify: req.InsecureSkipVerify,
		AllowInsecure:      req.AllowInsecure,
		AutoStart:          req.AutoStart,
	}
	if err := wm.saveManagedTunnelClientConfig(cfg); err != nil {
		wm.recordAudit(r, "tunnel.client_mode.save_config", "tunnel_client_mode", req.ClientName, activity.AuditStatusFailure, "Failed to save managed tunnel client configuration", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wm.recordAudit(r, "tunnel.client_mode.save_config", "tunnel_client_mode", req.ClientName, activity.AuditStatusSuccess, "Managed tunnel client configuration saved", map[string]any{"serverAddr": req.ServerAddr, "autoStart": req.AutoStart, "useManagedServerCA": req.UseManagedServerCA, "insecureSkipVerify": req.InsecureSkipVerify, "allowInsecure": req.AllowInsecure})

	status, err := wm.getManagedTunnelClientStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (wm *Manager) handleTunnelClientStart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req tunnelClientConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	cfg := managedTunnelClientConfig{
		Engine:             req.Engine,
		ServerAddr:         req.ServerAddr,
		ClientName:         req.ClientName,
		Token:              req.Token,
		UseManagedServerCA: req.UseManagedServerCA,
		ServerName:         req.ServerName,
		InsecureSkipVerify: req.InsecureSkipVerify,
		AllowInsecure:      req.AllowInsecure,
		AutoStart:          req.AutoStart,
	}
	if err := wm.startManagedTunnelClient(cfg); err != nil {
		wm.recordAudit(r, "tunnel.client_mode.start", "tunnel_client_mode", req.ClientName, activity.AuditStatusFailure, "Failed to start managed tunnel client", map[string]any{"error": err.Error(), "serverAddr": req.ServerAddr})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wm.recordAudit(r, "tunnel.client_mode.start", "tunnel_client_mode", req.ClientName, activity.AuditStatusSuccess, "Managed tunnel client started", map[string]any{"serverAddr": req.ServerAddr, "autoStart": req.AutoStart, "useManagedServerCA": req.UseManagedServerCA, "insecureSkipVerify": req.InsecureSkipVerify, "allowInsecure": req.AllowInsecure})

	status, err := wm.getManagedTunnelClientStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (wm *Manager) handleTunnelClientStop(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if err := wm.stopManagedTunnelClient(); err != nil {
		wm.recordAudit(r, "tunnel.client_mode.stop", "tunnel_client_mode", "managed", activity.AuditStatusFailure, "Failed to stop managed tunnel client", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wm.recordAudit(r, "tunnel.client_mode.stop", "tunnel_client_mode", "managed", activity.AuditStatusSuccess, "Managed tunnel client stopped", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func (wm *Manager) handleTunnelClientCAUpload(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, fmt.Sprintf("parse multipart form: %v", err), http.StatusBadRequest)
		return
	}

	fileName, caPEM, err := readMultipartFile(r, "clientCa", true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := wm.storeManagedTunnelClientCA(fileName, caPEM); err != nil {
		wm.recordAudit(r, "tunnel.client_mode.upload_ca", "tunnel_client_ca", "managed", activity.AuditStatusFailure, "Failed to upload managed tunnel client CA", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wm.recordAudit(r, "tunnel.client_mode.upload_ca", "tunnel_client_ca", "managed", activity.AuditStatusSuccess, "Managed tunnel client CA uploaded", map[string]any{"clientCA": fileName})

	status, err := wm.getManagedTunnelClientStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (wm *Manager) handleTunnelSessions(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	clientName := r.URL.Query().Get("clientName")
	routeName := r.URL.Query().Get("routeName")

	servers := wm.listManagedTunnelServers()
	if len(servers) == 0 {
		writeJSON(w, http.StatusOK, []tunnel.ManagedSessionSnapshot{})
		return
	}

	sessions := make([]tunnel.ManagedSessionSnapshot, 0)
	for _, server := range servers {
		sessions = append(sessions, server.ListActiveSessions()...)
	}
	if clientName != "" || routeName != "" {
		filtered := make([]tunnel.ManagedSessionSnapshot, 0, len(sessions))
		for _, session := range sessions {
			if clientName != "" && session.ClientName != clientName {
				continue
			}
			if routeName != "" && session.RouteName != routeName {
				continue
			}
			filtered = append(filtered, session)
		}
		sessions = filtered
	}

	writeJSON(w, http.StatusOK, sessions)
}

func (wm *Manager) handleTunnelServerFileDownload(w http.ResponseWriter, r *http.Request, kind string) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	cfgs, err := wm.loadManagedTunnelServerConfigs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filePath, downloadName, err := wm.resolveTunnelServerDownload(kind, cfgs.certificateConfig())
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "requested tunnel certificate file is not available", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "requested tunnel certificate file is not available", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	wm.recordAudit(r, "tunnel.server.download_client_ca", "tunnel_certificate", downloadName, activity.AuditStatusSuccess, "Downloaded managed tunnel client CA", nil)
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", downloadName))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	if _, err := w.Write(data); err != nil {
		return
	}
}

func readMultipartFile(r *http.Request, fieldName string, required bool) (string, []byte, error) {
	file, header, err := r.FormFile(fieldName)
	if err != nil {
		if !required && err == http.ErrMissingFile {
			return "", nil, nil
		}
		if err == http.ErrMissingFile {
			return "", nil, fmt.Errorf("%s is required", fieldName)
		}
		return "", nil, fmt.Errorf("read %s: %w", fieldName, err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", nil, fmt.Errorf("read %s: %w", fieldName, err)
	}
	return header.Filename, data, nil
}

func (wm *Manager) handleTunnelClients(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		clients, err := wm.listTunnelClients()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, clients)
	case http.MethodDelete:
		var req tunnelClientDeleteRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := wm.deleteTunnelClient(req.ClientName); err != nil {
			wm.recordAudit(r, "tunnel.client.delete", "tunnel_client", req.ClientName, activity.AuditStatusFailure, "Failed to delete managed tunnel client", map[string]any{"error": err.Error()})
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		wm.recordAudit(r, "tunnel.client.delete", "tunnel_client", req.ClientName, activity.AuditStatusSuccess, "Managed tunnel client deleted", nil)
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (wm *Manager) handleTunnelRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		routes, err := wm.listTunnelRoutes()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, routes)
	case http.MethodPost:
		var req tunnelRouteUpsertRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := wm.saveTunnelRouteWithOptions(req.ClientName, req.Name, req.TargetAddr, req.PublicPort, req.Enabled, req.IPWhitelist, req.Protocol, req.UDPIdleTimeoutSec, req.UDPMaxPayload); err != nil {
			wm.recordAudit(r, "tunnel.route.upsert", "tunnel_route", req.ClientName+"/"+req.Name, activity.AuditStatusFailure, "Failed to save tunnel route", map[string]any{"error": err.Error()})
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		wm.recordAudit(r, "tunnel.route.upsert", "tunnel_route", req.ClientName+"/"+req.Name, activity.AuditStatusSuccess, "Tunnel route saved", map[string]any{"targetAddr": req.TargetAddr, "publicPort": req.PublicPort, "enabled": req.Enabled, "ipWhitelist": req.IPWhitelist})
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
	case http.MethodDelete:
		var req tunnelRouteDeleteRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := wm.deleteTunnelRoute(req.ClientName, req.Name); err != nil {
			wm.recordAudit(r, "tunnel.route.delete", "tunnel_route", req.ClientName+"/"+req.Name, activity.AuditStatusFailure, "Failed to delete tunnel route", map[string]any{"error": err.Error()})
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		wm.recordAudit(r, "tunnel.route.delete", "tunnel_route", req.ClientName+"/"+req.Name, activity.AuditStatusSuccess, "Tunnel route deleted", nil)
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
