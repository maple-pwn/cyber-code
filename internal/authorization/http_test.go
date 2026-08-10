package authorization

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type testAdminAuthenticator struct{ request Request }

func (a testAdminAuthenticator) Authenticate(context.Context, string) (Request, error) {
	return a.request, nil
}

func TestAdminHandlerAuthorizesAndScopesManagementRequests(t *testing.T) {
	clock := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "owner", Role: RoleOwner, Active: true}})
	policy.SetClock(func() time.Time { return clock })
	if err := policy.RegisterSession(Session{ID: "session-1", TenantID: "tenant-a", Principal: "owner", ExpiresAt: clock.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := policy.ElevateSession("session-1", clock.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	handler := NewAdminHandler(NewAdminService(policy), testAdminAuthenticator{request: Request{TenantID: "tenant-a", Principal: "owner", SessionID: "session-1"}})
	req := httptest.NewRequest("POST", "/admin", strings.NewReader(`{"action":"invite","invitation":{"id":"invite-1","tenantId":"tenant-a","principal":"bob","role":"viewer","expiresAt":"2026-08-07T13:00:00Z"}}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"ok":true`) {
		t.Fatalf("invite response = %d %s", res.Code, res.Body.String())
	}

	list := httptest.NewRequest("POST", "/admin", strings.NewReader(`{"action":"members"}`))
	list.Header.Set("Authorization", "Bearer token")
	list.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, list)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "owner") {
		t.Fatalf("members response = %d %s", res.Code, res.Body.String())
	}
}

func TestAdminHandlerReturnsForbiddenForUnauthorizedQueries(t *testing.T) {
	clock := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "viewer", Role: RoleViewer, Active: true}})
	policy.SetClock(func() time.Time { return clock })
	if err := policy.RegisterSession(Session{ID: "session-1", TenantID: "tenant-a", Principal: "viewer", ExpiresAt: clock.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	handler := NewAdminHandler(NewAdminService(policy), testAdminAuthenticator{request: Request{TenantID: "tenant-a", Principal: "viewer", SessionID: "session-1"}})
	req := httptest.NewRequest("POST", "/admin", strings.NewReader(`{"action":"members"}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != 403 || !strings.Contains(res.Body.String(), "capability_denied") {
		t.Fatalf("response = %d %s", res.Code, res.Body.String())
	}
}

func TestAdminHandlerGrantsAndRevokesBoundedEmergencyAccess(t *testing.T) {
	clock := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "owner", Role: RoleOwner, Active: true}, {TenantID: "tenant-a", Principal: "auditor", Role: RoleAuditor, Active: true}})
	policy.SetClock(func() time.Time { return clock })
	for _, session := range []Session{{ID: "owner-session", TenantID: "tenant-a", Principal: "owner", ExpiresAt: clock.Add(time.Hour)}, {ID: "auditor-session", TenantID: "tenant-a", Principal: "auditor", ExpiresAt: clock.Add(time.Hour)}} {
		if err := policy.RegisterSession(session); err != nil {
			t.Fatal(err)
		}
	}
	if err := policy.ElevateSession("owner-session", clock.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	handler := NewAdminHandler(NewAdminService(policy), testAdminAuthenticator{request: Request{TenantID: "tenant-a", Principal: "owner", SessionID: "owner-session"}})
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/admin", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	if res := call(`{"action":"emergency_grant","sessionId":"auditor-session","reason":"incident response","until":"2026-08-08T12:15:00Z"}`); res.Code != 200 {
		t.Fatalf("grant = %d %s", res.Code, res.Body.String())
	}
	if err := policy.Authorize(Request{TenantID: "tenant-a", Principal: "auditor", SessionID: "auditor-session", Capability: CapabilityReportExport}); err != nil {
		t.Fatal(err)
	}
	if res := call(`{"action":"emergency_revoke","sessionId":"auditor-session"}`); res.Code != 200 {
		t.Fatalf("revoke = %d %s", res.Code, res.Body.String())
	}
}
