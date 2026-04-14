package web

import (
	"net/http"

	"github.com/apeming/go-proxy-server/internal/activity"
)

type proxyStartRequest struct {
	Type       string `json:"type"`
	Port       int    `json:"port"`
	BindListen bool   `json:"bindListen"`
}

type proxyStopRequest struct {
	Type string `json:"type"`
}

type proxyConfigRequest struct {
	Type       string `json:"type"`
	Port       int    `json:"port"`
	BindListen bool   `json:"bindListen"`
	AutoStart  bool   `json:"autoStart"`
}

func (wm *Manager) handleProxyStart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req proxyStartRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	server, err := wm.getProxyServer(req.Type)
	if err != nil {
		wm.recordAudit(r, "proxy.start", "proxy", req.Type, activity.AuditStatusFailure, "Failed to start proxy", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if server.Running.Load() {
		wm.recordAudit(r, "proxy.start", "proxy", req.Type, activity.AuditStatusFailure, "Failed to start proxy because it is already running", nil)
		http.Error(w, "Proxy already running", http.StatusBadRequest)
		return
	}
	if err := wm.startProxy(server, req.Port, req.BindListen); err != nil {
		wm.recordAudit(r, "proxy.start", "proxy", req.Type, activity.AuditStatusFailure, "Failed to start proxy", map[string]any{"error": err.Error(), "port": req.Port, "bindListen": req.BindListen})
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	wm.recordAudit(r, "proxy.start", "proxy", req.Type, activity.AuditStatusSuccess, "Proxy started", map[string]any{"port": req.Port, "bindListen": req.BindListen})
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func (wm *Manager) handleProxyStop(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req proxyStopRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	server, err := wm.getProxyServer(req.Type)
	if err != nil {
		wm.recordAudit(r, "proxy.stop", "proxy", req.Type, activity.AuditStatusFailure, "Failed to stop proxy", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !server.Running.Load() {
		wm.recordAudit(r, "proxy.stop", "proxy", req.Type, activity.AuditStatusFailure, "Failed to stop proxy because it is not running", nil)
		http.Error(w, "Proxy not running", http.StatusBadRequest)
		return
	}

	wm.stopProxy(server)
	wm.recordAudit(r, "proxy.stop", "proxy", req.Type, activity.AuditStatusSuccess, "Proxy stopped", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func (wm *Manager) handleProxyConfig(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req proxyConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	server, err := wm.getProxyServer(req.Type)
	if err != nil {
		wm.recordAudit(r, "proxy.update_config", "proxy", req.Type, activity.AuditStatusFailure, "Failed to update proxy configuration", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := wm.updateProxyConfig(server, req); err != nil {
		wm.recordAudit(r, "proxy.update_config", "proxy", req.Type, activity.AuditStatusFailure, "Failed to update proxy configuration", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	wm.recordAudit(r, "proxy.update_config", "proxy", req.Type, activity.AuditStatusSuccess, "Proxy configuration updated", map[string]any{"port": req.Port, "bindListen": req.BindListen, "autoStart": req.AutoStart})
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}
