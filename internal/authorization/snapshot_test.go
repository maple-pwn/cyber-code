package authorization

import (
	"testing"
	"time"
)

func TestAuthorizationSnapshotRoundTripAndTamperRejection(t *testing.T) {
	clock := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "owner", Role: RoleOwner, Active: true}})
	policy.SetClock(func() time.Time { return clock })
	if err := policy.RegisterSession(Session{ID: "session-1", TenantID: "tenant-a", Principal: "owner", ExpiresAt: clock.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	_ = policy.Authorize(Request{TenantID: "tenant-a", Principal: "owner", SessionID: "session-1", Capability: CapabilityEvidenceRead})
	snapshot := policy.Snapshot()
	restored, err := NewPolicyFromSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Members("tenant-a")) != 1 || len(restored.Decisions()) != 1 {
		t.Fatalf("restored snapshot incomplete")
	}
	snapshot.Decisions[0].Principal = "mallory"
	if _, err := NewPolicyFromSnapshot(snapshot); err != ErrAuditTampered {
		t.Fatalf("tampered snapshot = %v", err)
	}
}
