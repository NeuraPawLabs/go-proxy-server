package web

import (
	"fmt"
	"strings"
	"time"

	"github.com/apeming/go-proxy-server/internal/models"
	"gorm.io/gorm"
)

func (wm *Manager) listAuditLogs(filters auditLogFilters) ([]auditLogResponse, int64, error) {
	query := wm.db.Model(&models.AuditLog{})
	query = applyTimeWindow(query, filters.From, filters.To)
	if filters.Action != "" {
		query = query.Where("action = ?", filters.Action)
	}
	if filters.Status != "" {
		query = query.Where("status = ?", filters.Status)
	}
	if filters.TargetType != "" {
		query = query.Where("target_type = ?", filters.TargetType)
	}
	if filters.Search != "" {
		like := "%" + strings.TrimSpace(filters.Search) + "%"
		query = query.Where("message LIKE ? OR actor_id LIKE ? OR target_id LIKE ?", like, like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}

	var rows []models.AuditLog
	offset := (filters.Page - 1) * filters.Limit
	if err := query.Order("occurred_at DESC").Offset(offset).Limit(filters.Limit).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list audit logs: %w", err)
	}

	items := make([]auditLogResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, auditLogModelToResponse(row))
	}
	return items, total, nil
}

func (wm *Manager) listEventLogs(filters eventLogFilters) ([]eventLogResponse, int64, error) {
	query := wm.db.Model(&models.EventLog{})
	query = applyTimeWindow(query, filters.From, filters.To)
	if filters.Category != "" {
		query = query.Where("category = ?", filters.Category)
	}
	if filters.Severity != "" {
		query = query.Where("severity = ?", filters.Severity)
	}
	if filters.Source != "" {
		query = query.Where("source = ?", filters.Source)
	}
	if filters.EventType != "" {
		query = query.Where("event_type = ?", filters.EventType)
	}
	if filters.Search != "" {
		like := "%" + strings.TrimSpace(filters.Search) + "%"
		query = query.Where("message LIKE ? OR source LIKE ? OR event_type LIKE ?", like, like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count event logs: %w", err)
	}

	var rows []models.EventLog
	offset := (filters.Page - 1) * filters.Limit
	if err := query.Order("occurred_at DESC").Offset(offset).Limit(filters.Limit).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list event logs: %w", err)
	}

	items := make([]eventLogResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, eventLogModelToResponse(row))
	}
	return items, total, nil
}

func applyTimeWindow(query *gorm.DB, from, to *time.Time) *gorm.DB {
	if from != nil {
		query = query.Where("occurred_at >= ?", *from)
	}
	if to != nil {
		query = query.Where("occurred_at <= ?", *to)
	}
	return query
}
