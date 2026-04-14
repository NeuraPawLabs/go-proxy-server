package web

import (
	"net/http"
	"time"

	"github.com/apeming/go-proxy-server/internal/activity"
)

type userResponse struct {
	ID        uint      `json:"id"`
	IP        string    `json:"ip"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (wm *Manager) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users, err := wm.listUsers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, users)

	case http.MethodPost:
		var req struct {
			IP       string `json:"ip"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := wm.addUser(req.IP, req.Username, req.Password); err != nil {
			wm.recordAudit(r, "user.create", "user", req.Username, activity.AuditStatusFailure, "Failed to create proxy user", map[string]any{"error": err.Error()})
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		wm.recordAudit(r, "user.create", "user", req.Username, activity.AuditStatusSuccess, "Proxy user created", map[string]any{"ip": req.IP})
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})

	case http.MethodDelete:
		var req struct {
			Username string `json:"username"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := wm.deleteUser(req.Username); err != nil {
			wm.recordAudit(r, "user.delete", "user", req.Username, activity.AuditStatusFailure, "Failed to delete proxy user", map[string]any{"error": err.Error()})
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		wm.recordAudit(r, "user.delete", "user", req.Username, activity.AuditStatusSuccess, "Proxy user deleted", nil)
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (wm *Manager) handleWhitelist(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, wm.listWhitelist())

	case http.MethodPost:
		var req struct {
			IP string `json:"ip"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := wm.addWhitelistIP(req.IP); err != nil {
			wm.recordAudit(r, "whitelist.add", "whitelist_ip", req.IP, activity.AuditStatusFailure, "Failed to add whitelist IP", map[string]any{"error": err.Error()})
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		wm.recordAudit(r, "whitelist.add", "whitelist_ip", req.IP, activity.AuditStatusSuccess, "Whitelist IP added", nil)
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})

	case http.MethodDelete:
		var req struct {
			IP string `json:"ip"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := wm.deleteWhitelistIP(req.IP); err != nil {
			wm.recordAudit(r, "whitelist.delete", "whitelist_ip", req.IP, activity.AuditStatusFailure, "Failed to delete whitelist IP", map[string]any{"error": err.Error()})
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		wm.recordAudit(r, "whitelist.delete", "whitelist_ip", req.IP, activity.AuditStatusSuccess, "Whitelist IP deleted", nil)
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
