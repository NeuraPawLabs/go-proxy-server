package activity

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/models"
)

func newRecorderTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.AuditLog{}, &models.EventLog{}); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

func TestDBRecorderFlushesQueuedAuditAndEventLogsOnClose(t *testing.T) {
	db := newRecorderTestDB(t)
	recorder := NewDBRecorder(db, 8)

	recorder.RecordAudit(AuditRecord{
		OccurredAt: time.Date(2026, time.March, 30, 8, 0, 0, 0, time.UTC),
		ActorType:  "admin",
		ActorID:    "web_admin",
		Action:     "proxy.start",
		TargetType: "proxy",
		TargetID:   "http:8080",
		Status:     AuditStatusSuccess,
		SourceIP:   "127.0.0.1",
		Message:    "HTTP proxy started",
		Details:    map[string]any{"port": 8080},
	})
	recorder.RecordEvent(EventRecord{
		OccurredAt: time.Date(2026, time.March, 30, 8, 1, 0, 0, time.UTC),
		Category:   "tunnel",
		EventType:  "managed_client_connected",
		Severity:   SeverityInfo,
		Source:     "tunnel_server",
		Message:    "Managed tunnel client connected",
		Details:    map[string]any{"client_name": "node-a"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := recorder.Close(ctx); err != nil {
		t.Fatalf("close recorder: %v", err)
	}

	var auditLogs []models.AuditLog
	if err := db.Find(&auditLogs).Error; err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(auditLogs) != 1 {
		t.Fatalf("unexpected audit log count: got %d want 1", len(auditLogs))
	}
	if auditLogs[0].Action != "proxy.start" || auditLogs[0].Status != string(AuditStatusSuccess) {
		t.Fatalf("unexpected audit log row: %+v", auditLogs[0])
	}

	var eventLogs []models.EventLog
	if err := db.Find(&eventLogs).Error; err != nil {
		t.Fatalf("list event logs: %v", err)
	}
	if len(eventLogs) != 1 {
		t.Fatalf("unexpected event log count: got %d want 1", len(eventLogs))
	}
	if eventLogs[0].EventType != "managed_client_connected" || eventLogs[0].Severity != string(SeverityInfo) {
		t.Fatalf("unexpected event log row: %+v", eventLogs[0])
	}
}
