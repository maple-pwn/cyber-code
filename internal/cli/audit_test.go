package cli

import (
	"path/filepath"
	"testing"

	"cyber-code/internal/permissions"
)

func TestPersistentAuditMergesRecordsFromIndependentInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")
	first, err := newPersistentAuditLog(path, 1000)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newPersistentAuditLog(path, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Record(permissions.AuditRecord{Tool: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := second.Record(permissions.AuditRecord{Tool: "second"}); err != nil {
		t.Fatal(err)
	}
	var records []permissions.AuditRecord
	if err := readStateFile(path, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Tool != "first" || records[1].Tool != "second" {
		t.Fatalf("audit records = %#v", records)
	}
}
