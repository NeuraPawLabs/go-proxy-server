package web

import (
	"net/http"
	"os"
	"strings"

	"github.com/apeming/go-proxy-server/internal/activity"
)

type adminPasswordRequest struct {
	Password       string `json:"password"`
	BootstrapToken string `json:"bootstrapToken"`
	LotNumber      string `json:"lot_number"`
	CaptchaOutput  string `json:"captcha_output"`
	PassToken      string `json:"pass_token"`
	GenTime        string `json:"gen_time"`
}

type adminSessionResponse struct {
	Authenticated   bool   `json:"authenticated"`
	BootstrapNeeded bool   `json:"bootstrapNeeded"`
	GeetestID       string `json:"geetestId,omitempty"`
	CaptchaError    string `json:"captchaError,omitempty"`
}

type geetestConfig struct {
	ID  string
	Key string
}

func (cfg geetestConfig) Enabled() bool {
	return cfg.ID != "" && cfg.Key != ""
}

func (cfg geetestConfig) Partial() bool {
	return (cfg.ID == "") != (cfg.Key == "")
}

func (wm *Manager) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	configured, err := wm.adminPasswordConfigured()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	geetestCfg := loadGeetestConfig()

	resp := adminSessionResponse{
		Authenticated:   configured && wm.isAuthenticated(r),
		BootstrapNeeded: !configured,
	}
	if !configured {
		if _, err := wm.ensureAdminBootstrapToken(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if geetestCfg.Enabled() {
		resp.GeetestID = geetestCfg.ID
	} else if geetestCfg.Partial() && configured {
		resp.CaptchaError = "captcha configuration incomplete"
	}

	writeJSON(w, http.StatusOK, resp)
}

func (wm *Manager) handleAdminBootstrap(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req adminPasswordRequest
	if !decodeJSON(w, r, &req) {
		wm.recordAudit(r, "admin.bootstrap", "admin", adminActorID, activity.AuditStatusFailure, "Admin bootstrap rejected due to invalid request payload", nil)
		return
	}
	if err := wm.bootstrapAdminPassword(req.Password, req.BootstrapToken); err != nil {
		status := http.StatusBadRequest
		if err == errInvalidBootstrapToken {
			status = http.StatusForbidden
		}
		if err == errAdminPasswordConfigured {
			status = http.StatusConflict
		}
		wm.recordAudit(r, "admin.bootstrap", "admin", adminActorID, activity.AuditStatusFailure, "Admin bootstrap rejected", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), status)
		return
	}
	if err := wm.issueAdminSession(w); err != nil {
		wm.recordAudit(r, "admin.bootstrap", "admin", adminActorID, activity.AuditStatusFailure, "Admin bootstrap failed while creating session", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	wm.recordAudit(r, "admin.bootstrap", "admin", adminActorID, activity.AuditStatusSuccess, "Admin password initialized", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func (wm *Manager) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	configured, err := wm.adminPasswordConfigured()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !configured {
		wm.recordAudit(r, "admin.login", "admin", adminActorID, activity.AuditStatusFailure, "Admin login rejected because bootstrap is required", nil)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "bootstrap required", "bootstrapNeeded": true})
		return
	}

	var req adminPasswordRequest
	if !decodeJSON(w, r, &req) {
		wm.recordAudit(r, "admin.login", "admin", adminActorID, activity.AuditStatusFailure, "Admin login rejected due to invalid request payload", nil)
		return
	}

	geetestCfg := loadGeetestConfig()
	if geetestCfg.Partial() {
		wm.recordAudit(r, "admin.login", "admin", adminActorID, activity.AuditStatusFailure, "Admin login blocked by incomplete captcha configuration", nil)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "captcha configuration incomplete"})
		return
	}
	if geetestCfg.Enabled() {
		if req.LotNumber == "" || req.CaptchaOutput == "" || req.PassToken == "" || req.GenTime == "" {
			wm.recordAudit(r, "admin.login", "admin", adminActorID, activity.AuditStatusFailure, "Admin login missing captcha verification", nil)
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "captcha verification required"})
			return
		}

		valid, err := newGeetestValidator(geetestCfg.ID, geetestCfg.Key).validate(geetestValidateRequest{
			LotNumber:     req.LotNumber,
			CaptchaOutput: req.CaptchaOutput,
			PassToken:     req.PassToken,
			GenTime:       req.GenTime,
		})
		if err != nil {
			wm.recordAudit(r, "admin.login", "admin", adminActorID, activity.AuditStatusFailure, "Admin login captcha verification failed", map[string]any{"error": err.Error()})
			wm.recordEvent("auth", "captcha_verification_error", activity.SeverityWarn, "web_admin", "Captcha verification error during admin login", map[string]any{"source_ip": requestSourceIP(r)})
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "captcha verification failed"})
			return
		}
		if !valid {
			wm.recordAudit(r, "admin.login", "admin", adminActorID, activity.AuditStatusFailure, "Admin login captcha verification rejected", nil)
			wm.recordEvent("auth", "captcha_verification_failed", activity.SeverityWarn, "web_admin", "Captcha verification failed during admin login", map[string]any{"source_ip": requestSourceIP(r)})
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "captcha verification failed"})
			return
		}
	}

	ok, err := wm.verifyAdminPassword(req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		wm.recordAudit(r, "admin.login", "admin", adminActorID, activity.AuditStatusFailure, "Admin login failed due to invalid password", nil)
		wm.recordEvent("auth", "admin_login_failed", activity.SeverityWarn, "web_admin", "Admin login failed", map[string]any{"source_ip": requestSourceIP(r)})
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid password"})
		return
	}
	if err := wm.issueAdminSession(w); err != nil {
		wm.recordAudit(r, "admin.login", "admin", adminActorID, activity.AuditStatusFailure, "Admin login failed while creating session", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	wm.recordAudit(r, "admin.login", "admin", adminActorID, activity.AuditStatusSuccess, "Admin login succeeded", nil)
	wm.recordEvent("auth", "admin_login_succeeded", activity.SeverityInfo, "web_admin", "Admin login succeeded", map[string]any{"source_ip": requestSourceIP(r)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func (wm *Manager) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	wm.clearAdminSession(w, r)
	wm.recordAudit(r, "admin.logout", "admin", adminActorID, activity.AuditStatusSuccess, "Admin logged out", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func loadGeetestConfig() geetestConfig {
	return geetestConfig{
		ID:  lookupGeetestValue("GEETEST_ID"),
		Key: lookupGeetestValue("GEETEST_KEY"),
	}
}

func lookupGeetestValue(envKey string) string {
	return strings.TrimSpace(os.Getenv(envKey))
}
