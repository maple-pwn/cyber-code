package runtimeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testRemoteAuthenticator struct {
	claims RemoteClaims
	err    error
}

func (a *testRemoteAuthenticator) Authenticate(context.Context, string) (RemoteClaims, error) {
	return a.claims, a.err
}

func TestRemoteHandlerRequiresTLSBearerAndExactOrigin(t *testing.T) {
	server, auth := newTestRemoteServer(t)
	request := RemoteRequest{
		ID: "1", Type: "handshake",
		Handshake: &HandshakeRequest{SupportedProtocolVersions: []int{1}, AfterCursor: 0},
	}

	for name, test := range map[string]struct {
		mutate func(*http.Request)
		want   int
	}{
		"plain HTTP":     {func(request *http.Request) { request.TLS = nil }, http.StatusUpgradeRequired},
		"missing bearer": {func(request *http.Request) { request.Header.Del("Authorization") }, http.StatusUnauthorized},
		"wrong origin":   {func(request *http.Request) { request.Header.Set("Origin", "https://evil.example") }, http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			httpRequest := remoteHTTPRequest(t, request)
			test.mutate(httpRequest)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httpRequest)
			if response.Code != test.want {
				t.Fatalf("status = %d body=%s, want %d", response.Code, response.Body.String(), test.want)
			}
		})
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(response, remoteHTTPRequest(t, request))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"mode":"remote"`) {
		t.Fatalf("handshake status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "remote-secret") {
		t.Fatalf("response leaked bearer: %s", response.Body.String())
	}
	if auth.claims.Principal != "operator@example.test" {
		t.Fatalf("claims changed: %+v", auth.claims)
	}
}

func TestRemoteHandlerReauthenticatesRevocationAndCapabilityLoss(t *testing.T) {
	server, auth := newTestRemoteServer(t)
	handshake := RemoteRequest{ID: "1", Type: "handshake", Handshake: &HandshakeRequest{SupportedProtocolVersions: []int{1}, AfterCursor: 0}}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, remoteHTTPRequest(t, handshake))
	if response.Code != http.StatusOK {
		t.Fatalf("initial handshake = %d %s", response.Code, response.Body.String())
	}

	auth.err = ErrRemoteCredentialRevoked
	response = httptest.NewRecorder()
	server.ServeHTTP(response, remoteHTTPRequest(t, RemoteRequest{ID: "2", Type: "events", AfterCursor: 0}))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked response = %d %s", response.Code, response.Body.String())
	}

	auth.err = nil
	auth.claims.Capabilities = []string{"snapshot"}
	response = httptest.NewRecorder()
	server.ServeHTTP(response, remoteHTTPRequest(t, RemoteRequest{ID: "3", Type: "events", AfterCursor: 0}))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "capability_lost") {
		t.Fatalf("capability response = %d %s", response.Code, response.Body.String())
	}
}

func TestRemoteHandlerRejectsClientSelectedTaskID(t *testing.T) {
	server, _ := newTestRemoteServer(t)
	httpRequest := remoteHTTPRequest(t, RemoteRequest{ID: "forged", Type: "events", AfterCursor: 0})
	httpRequest.Body = io.NopCloser(strings.NewReader(`{"id":"forged","type":"events","afterCursor":0,"taskId":"task-victim"}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httpRequest)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("client-selected task ID accepted: %d %s", response.Code, response.Body.String())
	}
}

func TestRemoteContextsOwnIndependentActiveTasksAndStateFiles(t *testing.T) {
	server, auth := newTestRemoteServer(t)
	auth.claims.TaskContextID = "context-a"
	auth.claims.ControllerID = "controller-a"
	createA := mustLocalEnvelope(t, "create-a", map[string]any{"type": "task.create", "objective": "A", "runtimeId": "runtime-remote"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, remoteHTTPRequest(t, RemoteRequest{ID: "create-a", Type: "command", Command: &createA}))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"accepted"`) {
		t.Fatalf("create A = %d %s", response.Code, response.Body.String())
	}

	auth.claims.TaskContextID = "context-b"
	auth.claims.ControllerID = "controller-b"
	createB := mustLocalEnvelope(t, "create-b", map[string]any{"type": "task.create", "objective": "B", "runtimeId": "runtime-remote"})
	response = httptest.NewRecorder()
	server.ServeHTTP(response, remoteHTTPRequest(t, RemoteRequest{ID: "create-b", Type: "command", Command: &createB}))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"accepted"`) {
		t.Fatalf("create B = %d %s", response.Code, response.Body.String())
	}

	auth.claims.TaskContextID = "context-a"
	auth.claims.ControllerID = "controller-a"
	response = httptest.NewRecorder()
	server.ServeHTTP(response, remoteHTTPRequest(t, RemoteRequest{ID: "events-a", Type: "events", AfterCursor: 0}))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"title":"A"`) || strings.Contains(response.Body.String(), `"title":"B"`) {
		t.Fatalf("context A events crossed boundary: %d %s", response.Code, response.Body.String())
	}

	matches, err := filepath.Glob(filepath.Join(server.service.Store().Root(), "remote-runtime-*.json"))
	if err != nil || len(matches) != 2 {
		t.Fatalf("remote state files = %v err=%v", matches, err)
	}
	if _, err := os.Stat(filepath.Join(server.service.Store().Root(), "local-runtime-state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote contexts reused local state: %v", err)
	}
}

func TestRemoteHandlerSupportsCORSVersionSkewLeaseTransferAndBodyLimit(t *testing.T) {
	server, auth := newTestRemoteServer(t)

	preflight := httptest.NewRequest(http.MethodOptions, "https://runtime.example.test/v1/runtime", nil)
	preflight.Header.Set("Origin", "https://app.example.test")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, preflight)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "https://app.example.test" {
		t.Fatalf("preflight = %d headers=%v", response.Code, response.Header())
	}

	skew := RemoteRequest{ID: "skew", Type: "handshake", Handshake: &HandshakeRequest{SupportedProtocolVersions: []int{2}, AfterCursor: 0}}
	response = httptest.NewRecorder()
	server.ServeHTTP(response, remoteHTTPRequest(t, skew))
	if response.Code != http.StatusUpgradeRequired || !strings.Contains(response.Body.String(), "incompatible") {
		t.Fatalf("version skew = %d %s", response.Code, response.Body.String())
	}

	create := mustLocalEnvelope(t, "remote-create", map[string]any{"type": "task.create", "objective": "Audit", "runtimeId": "runtime-remote"})
	response = httptest.NewRecorder()
	server.ServeHTTP(response, remoteHTTPRequest(t, RemoteRequest{ID: "create", Type: "command", Command: &create}))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"accepted"`) {
		t.Fatalf("remote command create = %d %s", response.Code, response.Body.String())
	}

	for _, request := range []RemoteRequest{
		{ID: "take-1", Type: "command", Command: remoteEnvelope(t, "take-1", map[string]any{"type": "control.take", "expectedRevision": 0})},
		{ID: "take-2", Type: "command", Command: remoteEnvelope(t, "take-2", map[string]any{"type": "control.take", "expectedRevision": 1})},
	} {
		if request.ID == "take-1" {
			auth.claims.ControllerID = "controller-a"
		} else {
			auth.claims.ControllerID = "controller-b"
		}
		response = httptest.NewRecorder()
		server.ServeHTTP(response, remoteHTTPRequest(t, request))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"accepted"`) {
			t.Fatalf("remote command %s = %d %s", request.ID, response.Code, response.Body.String())
		}
	}
	stale := RemoteRequest{ID: "take-stale", Type: "command", Command: remoteEnvelope(t, "take-stale", map[string]any{"type": "control.take", "expectedRevision": 0})}
	response = httptest.NewRecorder()
	server.ServeHTTP(response, remoteHTTPRequest(t, stale))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"rejected"`) ||
		!strings.Contains(response.Body.String(), `"errorCode":"stale_control_revision"`) {
		t.Fatalf("stale lease revision = %d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	server.ServeHTTP(response, remoteHTTPRequest(t, RemoteRequest{ID: "lease-events", Type: "events", AfterCursor: 0}))
	for _, expected := range []string{
		`"type":"control.acquired"`, `"clientId":"controller-a"`,
		`"type":"control.transferred"`, `"clientId":"controller-b"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("lease events missing %s: %s", expected, response.Body.String())
		}
	}

	limited, _ := newTestRemoteServerWithLimit(t, 32)
	oversized := remoteHTTPRequest(t, RemoteRequest{ID: "large", Type: "health"})
	oversized.Body = io.NopCloser(strings.NewReader(`{"id":"` + strings.Repeat("x", 64) + `","type":"health"}`))
	response = httptest.NewRecorder()
	limited.ServeHTTP(response, oversized)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request = %d %s", response.Code, response.Body.String())
	}
}

func TestLocalAndRemoteRuntimeStateRemainIsolatedInOneStore(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, "runtime-shared", "operator@example.test", nil)
	local, err := NewLocalServer(LocalServerOptions{
		Service: service, Bearer: "local-secret", Role: "owner", Workspace: "/local",
		Source: SourceMetadata{
			Mode: SourceModeLocal, RuntimeID: "runtime-shared", Principal: "operator@example.test",
			Capabilities: []string{"events", "snapshot", "commands"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	localCreate := mustLocalEnvelope(t, "local-create", map[string]any{"type": "task.create", "objective": "Local", "runtimeId": "runtime-shared"})
	if response := local.Handle(context.Background(), LocalRequest{ID: "local", Type: "command", Bearer: "local-secret", Command: &localCreate}); response.Receipt == nil || response.Receipt.Status != "accepted" {
		t.Fatalf("local create = %+v", response)
	}

	auth := &testRemoteAuthenticator{claims: RemoteClaims{
		Principal: "operator@example.test", Role: "operator", TaskContextID: "remote-context", ControllerID: "remote-controller",
		Capabilities: []string{"events", "snapshot", "commands"},
	}}
	remote, err := NewRemoteServer(RemoteServerOptions{
		Service: service, Authenticator: auth, Workspace: "/remote",
		AllowedOrigins: []string{"https://app.example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	remoteCreate := mustLocalEnvelope(t, "remote-create", map[string]any{"type": "task.create", "objective": "Remote", "runtimeId": "runtime-shared"})
	response := httptest.NewRecorder()
	remote.ServeHTTP(response, remoteHTTPRequest(t, RemoteRequest{ID: "remote", Type: "command", Command: &remoteCreate}))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"accepted"`) {
		t.Fatalf("remote create = %d %s", response.Code, response.Body.String())
	}

	localEvents := local.Handle(context.Background(), LocalRequest{ID: "local-events", Type: "events", Bearer: "local-secret", AfterCursor: 0})
	encodedLocal, _ := json.Marshal(localEvents)
	if !bytes.Contains(encodedLocal, []byte(`"title":"Local"`)) || bytes.Contains(encodedLocal, []byte(`"title":"Remote"`)) {
		t.Fatalf("local events crossed source boundary: %s", encodedLocal)
	}
	response = httptest.NewRecorder()
	remote.ServeHTTP(response, remoteHTTPRequest(t, RemoteRequest{ID: "remote-events", Type: "events", AfterCursor: 0}))
	if !strings.Contains(response.Body.String(), `"title":"Remote"`) || strings.Contains(response.Body.String(), `"title":"Local"`) {
		t.Fatalf("remote events crossed source boundary: %s", response.Body.String())
	}
}

func TestRemoteServerAcceptsOnlyExactWebAndDesktopOrigins(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, "runtime-remote", "operator@example.test", nil)
	auth := &testRemoteAuthenticator{claims: RemoteClaims{
		Principal: "operator@example.test", Role: "operator", TaskContextID: "context-origin-test", ControllerID: "controller-origin-test",
		Capabilities: []string{"events"},
	}}
	for _, origins := range [][]string{
		{"https://app.example.test"},
		{"tauri://localhost"},
		{"http://tauri.localhost"},
	} {
		if _, err := NewRemoteServer(RemoteServerOptions{
			Service: service, Authenticator: auth, AllowedOrigins: origins,
		}); err != nil {
			t.Fatalf("valid origin %q rejected: %v", origins[0], err)
		}
	}
	for _, origin := range []string{"http://app.example.test", "tauri://attacker", "file://localhost"} {
		if _, err := NewRemoteServer(RemoteServerOptions{
			Service: service, Authenticator: auth, AllowedOrigins: []string{origin},
		}); err == nil {
			t.Fatalf("unsafe origin %q accepted", origin)
		}
	}
}

func newTestRemoteServer(t *testing.T) (*RemoteServer, *testRemoteAuthenticator) {
	return newTestRemoteServerWithLimit(t, 0)
}

func newTestRemoteServerWithLimit(t *testing.T, limit int) (*RemoteServer, *testRemoteAuthenticator) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, "runtime-remote", "operator@example.test", nil)
	auth := &testRemoteAuthenticator{claims: RemoteClaims{
		Principal: "operator@example.test", Role: "operator",
		TaskContextID: "context-default", ControllerID: "controller-default",
		Capabilities: []string{"events", "snapshot", "commands"},
	}}
	server, err := NewRemoteServer(RemoteServerOptions{
		Service: service, Authenticator: auth, Workspace: "/workspace",
		AllowedOrigins: []string{"https://app.example.test"}, MaxMessageBytes: limit,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, auth
}

func remoteEnvelope(t *testing.T, key string, command any) *CommandEnvelope {
	t.Helper()
	envelope := mustLocalEnvelope(t, key, command)
	return &envelope
}

func remoteHTTPRequest(t *testing.T, request RemoteRequest) *http.Request {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	httpRequest := httptest.NewRequest(http.MethodPost, "https://runtime.example.test/v1/runtime", bytes.NewReader(body))
	httpRequest.Header.Set("Authorization", "Bearer remote-secret")
	httpRequest.Header.Set("Origin", "https://app.example.test")
	httpRequest.Header.Set("Content-Type", "application/json")
	return httpRequest
}
