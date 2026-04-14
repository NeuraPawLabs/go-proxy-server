package web

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/apeming/go-proxy-server/internal/activity"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
)

type configResponse struct {
	Timeout  configTimeoutResponse  `json:"timeout"`
	Limiter  configLimiterResponse  `json:"limiter"`
	System   configSystemResponse   `json:"system"`
	Security configSecurityResponse `json:"security"`
}

type configTimeoutResponse struct {
	Connect   int `json:"connect"`
	IdleRead  int `json:"idleRead"`
	IdleWrite int `json:"idleWrite"`
}

type configLimiterResponse struct {
	MaxConcurrentConnections      int32 `json:"maxConcurrentConnections"`
	MaxConcurrentConnectionsPerIP int32 `json:"maxConcurrentConnectionsPerIP"`
}

type configSystemResponse struct {
	AutostartEnabled   bool   `json:"autostartEnabled"`
	RegistryEnabled    bool   `json:"registryEnabled"`
	AutostartSupported bool   `json:"autostartSupported"`
	Platform           string `json:"platform"`
}

type configSecurityResponse struct {
	AllowPrivateIPAccess bool `json:"allowPrivateIPAccess"`
}

type configUpdateRequest struct {
	Timeout  *configTimeoutUpdate  `json:"timeout"`
	Limiter  *configLimiterUpdate  `json:"limiter"`
	System   *configSystemUpdate   `json:"system"`
	Security *configSecurityUpdate `json:"security"`
}

type configTimeoutUpdate struct {
	Connect   int `json:"connect"`
	IdleRead  int `json:"idleRead"`
	IdleWrite int `json:"idleWrite"`
}

type configLimiterUpdate struct {
	MaxConcurrentConnections      int32 `json:"maxConcurrentConnections"`
	MaxConcurrentConnectionsPerIP int32 `json:"maxConcurrentConnectionsPerIP"`
}

type configSystemUpdate struct {
	AutostartEnabled bool `json:"autostartEnabled"`
}

type configSecurityUpdate struct {
	AllowPrivateIPAccess bool `json:"allowPrivateIPAccess"`
}

func (wm *Manager) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		response, err := wm.buildConfigResponse()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, response)

	case http.MethodPost:
		var req configUpdateRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := wm.applyConfigUpdate(req); err != nil {
			wm.recordAudit(r, "config.update", "system_config", "global", activity.AuditStatusFailure, "Failed to update system configuration", map[string]any{
				"error":    err.Error(),
				"sections": describeUpdatedConfigSections(req),
			})
			http.Error(w, err.Error(), statusCodeForConfigError(err))
			return
		}
		wm.recordAudit(r, "config.update", "system_config", "global", activity.AuditStatusSuccess, "System configuration updated", map[string]any{
			"sections": describeUpdatedConfigSections(req),
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (wm *Manager) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Application is shutting down..."})
	wm.recordAudit(r, "system.shutdown", "application", "go-proxy-server", activity.AuditStatusSuccess, "Application shutdown requested from admin UI", nil)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := wm.ShutdownApplication(); err != nil {
			applogger.Error("Error during shutdown: %v", err)
		}
		applogger.Info("Application shutdown complete")
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}()
}

func describeUpdatedConfigSections(req configUpdateRequest) string {
	sections := make([]string, 0, 4)
	if req.Timeout != nil {
		sections = append(sections, "timeout")
	}
	if req.Limiter != nil {
		sections = append(sections, "limiter")
	}
	if req.System != nil {
		sections = append(sections, "system")
	}
	if req.Security != nil {
		sections = append(sections, "security")
	}
	return strings.Join(sections, ",")
}
