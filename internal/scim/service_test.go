package scim

import (
	"testing"

	"cyber-code/internal/authorization"
)

func TestServiceCreatesUpdatesFiltersAndDeactivatesUsers(t *testing.T) {
	policy := authorization.NewPolicy(nil)
	service := NewService(policy)
	created, err := service.Create("tenant-a", "scim@example.test", User{UserName: "alice@example.test", ExternalID: "directory-1", DisplayName: "Alice", Active: boolPointer(true), Role: authorization.RoleOperator})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.UserName != "alice@example.test" || created.Meta.ResourceType != "User" {
		t.Fatalf("created user = %#v", created)
	}
	duplicate, err := service.Create("tenant-a", "scim@example.test", User{UserName: "alice@example.test", ExternalID: "directory-1", DisplayName: "Alice", Active: boolPointer(true), Role: authorization.RoleOperator})
	if err != nil || duplicate.Meta.Version != created.Meta.Version {
		t.Fatalf("idempotent duplicate = %#v err=%v", duplicate, err)
	}
	updated, err := service.Create("tenant-a", "scim@example.test", User{UserName: "alice@example.test", ExternalID: "directory-1", DisplayName: "Alice Updated", Active: boolPointer(true), Role: authorization.RoleAuditor})
	if err != nil || updated.ID != created.ID || updated.Role != authorization.RoleAuditor {
		t.Fatalf("idempotent update = %#v err=%v", updated, err)
	}
	users, total, err := service.List("tenant-a", `externalId eq "directory-1"`, 1, 10)
	if err != nil || total != 1 || len(users) != 1 || users[0].DisplayName != "Alice Updated" {
		t.Fatalf("filtered users=%#v total=%d err=%v", users, total, err)
	}
	patched, err := service.Patch("tenant-a", "scim@example.test", created.ID, []PatchOperation{{Operation: "replace", Path: "active", Value: false}})
	if err != nil || patched.Active == nil || *patched.Active {
		t.Fatalf("patched user=%#v err=%v", patched, err)
	}
	if err := service.Delete("tenant-a", "scim@example.test", created.ID); err != nil {
		t.Fatal(err)
	}
	members := policy.Members("tenant-a")
	if len(members) != 1 || members[0].Active {
		t.Fatalf("members after delete = %#v", members)
	}
	decisions := policy.DecisionsForTenant("tenant-a")
	if len(decisions) < 3 {
		t.Fatalf("SCIM mutations were not audited: %#v", decisions)
	}
}

func TestServiceDoesNotCrossTenantBoundaries(t *testing.T) {
	policy := authorization.NewPolicy(nil)
	service := NewService(policy)
	created, err := service.Create("tenant-a", "scim-a", User{UserName: "alice@example.test", ExternalID: "directory-1", Active: boolPointer(true), Role: authorization.RoleViewer})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get("tenant-b", created.ID); err == nil {
		t.Fatal("tenant-b read tenant-a SCIM user")
	}
	if err := service.Delete("tenant-b", "scim-b", created.ID); err == nil {
		t.Fatal("tenant-b deactivated tenant-a SCIM user")
	}
}

func boolPointer(value bool) *bool { return &value }
