package permissions

import (
	"sync"
	"time"

	"cyber-code/internal/security"
)

// AuditRecord intentionally excludes commands, file contents, and credentials.
type AuditRecord struct {
	Time         time.Time          `json:"time"`
	Tool         string             `json:"tool"`
	Action       string             `json:"action"`
	Behavior     PermissionBehavior `json:"behavior"`
	Reason       string             `json:"reason"`
	RuleID       string             `json:"rule_id,omitempty"`
	PathCount    int                `json:"path_count,omitempty"`
	NetworkCount int                `json:"network_count,omitempty"`
}

type AuditSink interface {
	Record(AuditRecord) error
}

// AuditLog is a bounded, concurrency-safe in-memory audit sink.
type AuditLog struct {
	mu      sync.RWMutex
	limit   int
	records []AuditRecord
}

func NewAuditLog(limit int) *AuditLog {
	if limit <= 0 {
		limit = 1000
	}
	return &AuditLog{limit: limit}
}

func (log *AuditLog) Record(record AuditRecord) error {
	if log == nil {
		return nil
	}
	redactor := security.NewRedactor()
	record.Tool = redactor.Text(record.Tool)
	record.Action = redactor.Text(record.Action)
	record.Reason = redactor.Text(record.Reason)
	record.RuleID = redactor.Text(record.RuleID)
	log.mu.Lock()
	defer log.mu.Unlock()
	if len(log.records) == log.limit {
		copy(log.records, log.records[1:])
		log.records[len(log.records)-1] = record
		return nil
	}
	log.records = append(log.records, record)
	return nil
}

func (log *AuditLog) Records() []AuditRecord {
	if log == nil {
		return nil
	}
	log.mu.RLock()
	defer log.mu.RUnlock()
	return append([]AuditRecord(nil), log.records...)
}
