package runtimeapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cyber-code/internal/authorization"
)

func TestTeamHandlerRoutesRuntimeAndAdminExactly(t *testing.T) {
	runtimeCalls, adminCalls := 0, 0
	handler, err := NewTeamHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { runtimeCalls++; w.WriteHeader(http.StatusNoContent) }), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { adminCalls++; w.WriteHeader(http.StatusAccepted) }))
	if err != nil {
		t.Fatal(err)
	}
	for path, status := range map[string]int{"/runtime": http.StatusNoContent, "/admin": http.StatusAccepted, "/runtime/forged": http.StatusNotFound, "/": http.StatusNotFound} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != status {
			t.Fatalf("%s status = %d, want %d", path, response.Code, status)
		}
	}
	if runtimeCalls != 1 || adminCalls != 1 {
		t.Fatalf("calls runtime=%d admin=%d", runtimeCalls, adminCalls)
	}
}

func TestTeamHandlerRequiresBothSecurityBoundaries(t *testing.T) {
	if _, err := NewTeamHandler(nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})); err == nil {
		t.Fatal("accepted missing runtime handler")
	}
	if _, err := NewTeamHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil); err == nil {
		t.Fatal("accepted missing admin handler")
	}
}

func TestTeamHandlerExposesHealthAndBoundedRequestIDs(t *testing.T) {
	metrics := &TeamMetrics{}
	handler, err := NewObservedTeamHandler(http.NotFoundHandler(), http.NotFoundHandler(), metrics)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("X-Request-ID", "request-123")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != "{\"ok\":true}\n" {
			t.Fatalf("%s = %d %q", path, response.Code, response.Body.String())
		}
		if response.Header().Get("X-Request-ID") != "request-123" {
			t.Fatalf("request ID = %q", response.Header().Get("X-Request-ID"))
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	request.Header.Set("X-Request-ID", strings.Repeat("x", 129))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || !strings.HasPrefix(response.Header().Get("X-Request-ID"), "team-") {
		t.Fatalf("invalid request ID response = %d id=%q", response.Code, response.Header().Get("X-Request-ID"))
	}
	notFound := httptest.NewRecorder()
	handler.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/missing", nil))
	snapshot := metrics.Snapshot()
	if snapshot.Requests != 4 || snapshot.Health != 3 || snapshot.NotFound != 1 || snapshot.Failures != 2 {
		t.Fatalf("metrics = %+v", snapshot)
	}
}

func TestRemoteTeamHandlerUsesPolicyForAdminClaims(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeService := NewService(store, "runtime-team", "owner@example.test", nil)
	auth := &testRemoteAuthenticator{claims: RemoteClaims{
		Principal: "owner@example.test", TenantID: "tenant-a", SessionID: "session-owner",
		Role: "owner", TaskContextID: "context-team", ControllerID: "controller-team",
		Capabilities: []string{"events", "snapshot", "commands"},
	}}
	runtime, err := NewRemoteServer(RemoteServerOptions{
		Service: runtimeService, Authenticator: auth, Workspace: "/workspace",
		AllowedOrigins: []string{"https://app.example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	policy := authorization.NewPolicy([]authorization.Member{
		{TenantID: "tenant-a", Principal: "owner@example.test", Role: authorization.RoleOwner, Active: true},
		{TenantID: "tenant-b", Principal: "other@example.test", Role: authorization.RoleOwner, Active: true},
	})
	policy.SetClock(func() time.Time { return clock })
	if err := policy.RegisterSession(authorization.Session{ID: "session-owner", TenantID: "tenant-a", Principal: "owner@example.test", ExpiresAt: clock.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := policy.ElevateSession("session-owner", clock.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	handler, err := NewTeamHandlerForRemote(runtime, authorization.NewAdminService(policy), auth)
	if err != nil {
		t.Fatal(err)
	}
	adminRequest := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "https://runtime.example.test/admin", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer remote-secret")
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if response := adminRequest(`{"action":"members"}`); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "owner@example.test") {
		t.Fatalf("owner admin query = %d %s", response.Code, response.Body.String())
	}

	auth.claims.Role = "operator"
	if response := adminRequest(`{"action":"members"}`); response.Code != http.StatusOK {
		t.Fatalf("role claim should not override policy, got %d %s", response.Code, response.Body.String())
	}
	if err := policy.RevokeSession("session-owner"); err != nil {
		t.Fatal(err)
	}
	if response := adminRequest(`{"action":"members"}`); response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "session_revoked") {
		t.Fatalf("revoked session = %d %s", response.Code, response.Body.String())
	}

	auth.claims.SessionID = "session-other"
	auth.claims.TenantID = "tenant-b"
	if err := policy.RegisterSession(authorization.Session{ID: "session-other", TenantID: "tenant-b", Principal: "other@example.test", ExpiresAt: clock.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if response := adminRequest(`{"action":"members"}`); response.Code != http.StatusForbidden {
		t.Fatalf("cross-principal claims = %d %s", response.Code, response.Body.String())
	}

	auth.claims.Principal = "other@example.test"
	if response := adminRequest(`{"action":"members"}`); response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "step_up_required") {
		t.Fatalf("cross-tenant non-step-up query = %d %s", response.Code, response.Body.String())
	}
}
