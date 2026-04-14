package web

import (
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/apeming/go-proxy-server/internal/activity"
)

const adminActorID = "web_admin"

func (wm *Manager) recordAudit(r *http.Request, action, targetType, targetID string, status activity.AuditStatus, message string, details map[string]any) {
	activity.RecordAudit(activity.AuditRecord{
		OccurredAt: time.Now(),
		ActorType:  "admin",
		ActorID:    adminActorID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Status:     status,
		SourceIP:   requestSourceIP(r),
		UserAgent:  r.UserAgent(),
		Message:    message,
		Details:    details,
	})
}

func (wm *Manager) recordEvent(category, eventType string, severity activity.Severity, source, message string, details map[string]any) {
	activity.RecordEvent(activity.EventRecord{
		OccurredAt: time.Now(),
		Category:   category,
		EventType:  eventType,
		Severity:   severity,
		Source:     source,
		Message:    message,
		Details:    details,
	})
}

func requestSourceIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func decodeLogDetails(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return map[string]any{"raw": raw}
	}
	return payload
}
