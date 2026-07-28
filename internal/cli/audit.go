package cli

import (
	"fmt"
	"sync"

	"cyber-code/internal/permissions"
)

type persistentAuditLog struct {
	mu    sync.Mutex
	path  string
	limit int
}

func newPersistentAuditLog(path string, limit int) (*persistentAuditLog, error) {
	persistent := &persistentAuditLog{path: path, limit: limit}
	if err := withStateFileLock(path, func() error {
		log, err := loadBoundedAuditLog(path, limit)
		if err != nil {
			return err
		}
		return writeStateFile(path, log.Records())
	}); err != nil {
		return nil, fmt.Errorf("initialize permission audit log: %w", err)
	}
	return persistent, nil
}

func (log *persistentAuditLog) Record(record permissions.AuditRecord) error {
	if log == nil {
		return nil
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	return withStateFileLock(log.path, func() error {
		current, err := loadBoundedAuditLog(log.path, log.limit)
		if err != nil {
			return err
		}
		if err := current.Record(record); err != nil {
			return err
		}
		return writeStateFile(log.path, current.Records())
	})
}

func loadBoundedAuditLog(path string, limit int) (*permissions.AuditLog, error) {
	var existing []permissions.AuditRecord
	if err := readStateFile(path, &existing); err != nil {
		return nil, fmt.Errorf("load permission audit log: %w", err)
	}
	log := permissions.NewAuditLog(limit)
	start := 0
	if limit > 0 && len(existing) > limit {
		start = len(existing) - limit
	}
	for _, record := range existing[start:] {
		if err := log.Record(record); err != nil {
			return nil, err
		}
	}
	return log, nil
}
