package activity

import (
	"context"
	"encoding/json"
	"fmt"
	stdlog "log"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/models"
)

type AuditStatus string

type Severity string

const (
	AuditStatusSuccess AuditStatus = "success"
	AuditStatusFailure AuditStatus = "failure"

	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

type AuditRecord struct {
	OccurredAt time.Time
	ActorType  string
	ActorID    string
	Action     string
	TargetType string
	TargetID   string
	Status     AuditStatus
	SourceIP   string
	UserAgent  string
	Message    string
	Details    map[string]any
}

type EventRecord struct {
	OccurredAt time.Time
	Category   string
	EventType  string
	Severity   Severity
	Source     string
	Message    string
	Details    map[string]any
}

type Recorder interface {
	RecordAudit(AuditRecord)
	RecordEvent(EventRecord)
	Close(context.Context) error
}

var (
	recorderMu sync.RWMutex
	recorder   Recorder
)

func SetRecorder(r Recorder) {
	recorderMu.Lock()
	defer recorderMu.Unlock()
	recorder = r
}

func Close(ctx context.Context) error {
	recorderMu.Lock()
	current := recorder
	recorder = nil
	recorderMu.Unlock()
	if current == nil {
		return nil
	}
	return current.Close(ctx)
}

func RecordAudit(record AuditRecord) {
	recorderMu.RLock()
	current := recorder
	recorderMu.RUnlock()
	if current == nil {
		return
	}
	if record.OccurredAt.IsZero() {
		record.OccurredAt = time.Now()
	}
	current.RecordAudit(record)
}

func RecordEvent(record EventRecord) {
	recorderMu.RLock()
	current := recorder
	recorderMu.RUnlock()
	if current == nil {
		return
	}
	if record.OccurredAt.IsZero() {
		record.OccurredAt = time.Now()
	}
	current.RecordEvent(record)
}

type queueItem struct {
	audit *AuditRecord
	event *EventRecord
}

type DBRecorder struct {
	db     *gorm.DB
	queue  chan queueItem
	closed atomic.Bool
	wg     sync.WaitGroup
	drops  atomic.Int64
}

func NewDBRecorder(db *gorm.DB, bufferSize int) *DBRecorder {
	if bufferSize <= 0 {
		bufferSize = 1024
	}
	r := &DBRecorder{
		db:    db,
		queue: make(chan queueItem, bufferSize),
	}
	r.wg.Add(1)
	go r.run()
	return r
}

func (r *DBRecorder) RecordAudit(record AuditRecord) {
	if r == nil || r.closed.Load() {
		return
	}
	item := queueItem{audit: &record}
	select {
	case r.queue <- item:
	default:
		if err := r.persistAudit(record); err != nil {
			stdlog.Printf("[activity] persist audit synchronously failed: %v", err)
		}
	}
}

func (r *DBRecorder) RecordEvent(record EventRecord) {
	if r == nil || r.closed.Load() {
		return
	}
	item := queueItem{event: &record}
	select {
	case r.queue <- item:
	default:
		if record.Severity == SeverityWarn || record.Severity == SeverityError {
			if err := r.persistEvent(record); err != nil {
				stdlog.Printf("[activity] persist event synchronously failed: %v", err)
			}
			return
		}
		dropped := r.drops.Add(1)
		if dropped == 1 || dropped%100 == 0 {
			stdlog.Printf("[activity] event queue full, dropped %d low-priority events", dropped)
		}
	}
}

func (r *DBRecorder) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if !r.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(r.queue)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.wg.Wait()
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *DBRecorder) run() {
	defer r.wg.Done()
	for item := range r.queue {
		var err error
		switch {
		case item.audit != nil:
			err = r.persistAudit(*item.audit)
		case item.event != nil:
			err = r.persistEvent(*item.event)
		}
		if err != nil {
			stdlog.Printf("[activity] persist failed: %v", err)
		}
	}
}

func (r *DBRecorder) persistAudit(record AuditRecord) error {
	details, err := marshalDetails(record.Details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}
	return r.db.Create(&models.AuditLog{
		OccurredAt: record.OccurredAt,
		ActorType:  record.ActorType,
		ActorID:    record.ActorID,
		Action:     record.Action,
		TargetType: record.TargetType,
		TargetID:   record.TargetID,
		Status:     string(record.Status),
		SourceIP:   record.SourceIP,
		UserAgent:  record.UserAgent,
		Message:    record.Message,
		Details:    details,
	}).Error
}

func (r *DBRecorder) persistEvent(record EventRecord) error {
	details, err := marshalDetails(record.Details)
	if err != nil {
		return fmt.Errorf("marshal event details: %w", err)
	}
	return r.db.Create(&models.EventLog{
		OccurredAt: record.OccurredAt,
		Category:   record.Category,
		EventType:  record.EventType,
		Severity:   string(record.Severity),
		Source:     record.Source,
		Message:    record.Message,
		Details:    details,
	}).Error
}

func marshalDetails(details map[string]any) (string, error) {
	if len(details) == 0 {
		return "", nil
	}
	data, err := json.Marshal(details)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
