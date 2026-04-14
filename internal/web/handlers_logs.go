package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apeming/go-proxy-server/internal/models"
)

type paginatedLogsResponse[T any] struct {
	Items   []T   `json:"items"`
	Total   int64 `json:"total"`
	Page    int   `json:"page"`
	Limit   int   `json:"limit"`
	Pages   int   `json:"pages"`
	HasMore bool  `json:"hasMore"`
}

type auditLogResponse struct {
	ID         uint           `json:"id"`
	OccurredAt time.Time      `json:"occurredAt"`
	ActorType  string         `json:"actorType"`
	ActorID    string         `json:"actorId"`
	Action     string         `json:"action"`
	TargetType string         `json:"targetType"`
	TargetID   string         `json:"targetId"`
	Status     string         `json:"status"`
	SourceIP   string         `json:"sourceIp"`
	UserAgent  string         `json:"userAgent"`
	Message    string         `json:"message"`
	Details    map[string]any `json:"details,omitempty"`
}

type eventLogResponse struct {
	ID         uint           `json:"id"`
	OccurredAt time.Time      `json:"occurredAt"`
	Category   string         `json:"category"`
	EventType  string         `json:"eventType"`
	Severity   string         `json:"severity"`
	Source     string         `json:"source"`
	Message    string         `json:"message"`
	Details    map[string]any `json:"details,omitempty"`
}

type auditLogFilters struct {
	Page       int
	Limit      int
	Action     string
	Status     string
	TargetType string
	Search     string
	From       *time.Time
	To         *time.Time
}

type eventLogFilters struct {
	Page      int
	Limit     int
	Category  string
	Severity  string
	Source    string
	EventType string
	Search    string
	From      *time.Time
	To        *time.Time
}

func (wm *Manager) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	filters := auditLogFilters{
		Page:       parsePositiveInt(r.URL.Query().Get("page"), 1),
		Limit:      clamp(parsePositiveInt(r.URL.Query().Get("limit"), 50), 1, 200),
		Action:     strings.TrimSpace(r.URL.Query().Get("action")),
		Status:     strings.TrimSpace(r.URL.Query().Get("status")),
		TargetType: strings.TrimSpace(r.URL.Query().Get("targetType")),
		Search:     strings.TrimSpace(r.URL.Query().Get("search")),
		From:       parseTimeFilter(r.URL.Query().Get("from")),
		To:         parseTimeFilter(r.URL.Query().Get("to")),
	}

	items, total, err := wm.listAuditLogs(filters)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, buildPaginatedLogsResponse(items, total, filters.Page, filters.Limit))
}

func (wm *Manager) handleEventLogs(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	filters := eventLogFilters{
		Page:      parsePositiveInt(r.URL.Query().Get("page"), 1),
		Limit:     clamp(parsePositiveInt(r.URL.Query().Get("limit"), 50), 1, 200),
		Category:  strings.TrimSpace(r.URL.Query().Get("category")),
		Severity:  strings.TrimSpace(r.URL.Query().Get("severity")),
		Source:    strings.TrimSpace(r.URL.Query().Get("source")),
		EventType: strings.TrimSpace(r.URL.Query().Get("eventType")),
		Search:    strings.TrimSpace(r.URL.Query().Get("search")),
		From:      parseTimeFilter(r.URL.Query().Get("from")),
		To:        parseTimeFilter(r.URL.Query().Get("to")),
	}

	items, total, err := wm.listEventLogs(filters)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, buildPaginatedLogsResponse(items, total, filters.Page, filters.Limit))
}

func buildPaginatedLogsResponse[T any](items []T, total int64, page, limit int) paginatedLogsResponse[T] {
	pages := 0
	if total > 0 {
		pages = int((total + int64(limit) - 1) / int64(limit))
	}
	return paginatedLogsResponse[T]{
		Items:   items,
		Total:   total,
		Page:    page,
		Limit:   limit,
		Pages:   pages,
		HasMore: int64(page*limit) < total,
	}
}

func auditLogModelToResponse(item models.AuditLog) auditLogResponse {
	return auditLogResponse{
		ID:         item.ID,
		OccurredAt: item.OccurredAt,
		ActorType:  item.ActorType,
		ActorID:    item.ActorID,
		Action:     item.Action,
		TargetType: item.TargetType,
		TargetID:   item.TargetID,
		Status:     item.Status,
		SourceIP:   item.SourceIP,
		UserAgent:  item.UserAgent,
		Message:    item.Message,
		Details:    decodeLogDetails(item.Details),
	}
}

func eventLogModelToResponse(item models.EventLog) eventLogResponse {
	return eventLogResponse{
		ID:         item.ID,
		OccurredAt: item.OccurredAt,
		Category:   item.Category,
		EventType:  item.EventType,
		Severity:   item.Severity,
		Source:     item.Source,
		Message:    item.Message,
		Details:    decodeLogDetails(item.Details),
	}
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func parseTimeFilter(raw string) *time.Time {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &parsed
}
