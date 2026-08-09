package scim

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cyber-code/internal/authorization"
)

type testAuthenticator map[string]Identity

func (authenticator testAuthenticator) Authenticate(_ context.Context, token string) (Identity, error) {
	identity, ok := authenticator[token]
	if !ok {
		return Identity{}, ErrUnauthorized
	}
	return identity, nil
}

func TestHandlerRequiresFeatureFlagAndTenantBoundBearer(t *testing.T) {
	service := NewService(authorization.NewPolicy(nil))
	disabled := NewHandler(HandlerOptions{Service: service, Authenticator: testAuthenticator{"token-a": {TenantID: "tenant-a", Principal: "scim-a"}}})
	request := httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)
	response := httptest.NewRecorder()
	disabled.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled status = %d", response.Code)
	}

	handler := NewHandler(HandlerOptions{Enabled: true, Service: service, Authenticator: testAuthenticator{"token-a": {TenantID: "tenant-a", Principal: "scim-a"}}})
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	body := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"alice@example.test","externalId":"directory-1","active":true}`
	create := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", strings.NewReader(body))
	create.Header.Set("Authorization", "Bearer token-a")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated || created.Header().Get("ETag") == "" {
		t.Fatalf("create status=%d headers=%v body=%s", created.Code, created.Header(), created.Body.String())
	}
	var user User
	if err := json.Unmarshal(created.Body.Bytes(), &user); err != nil || user.ID == "" {
		t.Fatalf("created user=%#v err=%v", user, err)
	}
	patch := httptest.NewRequest(http.MethodPatch, "/scim/v2/Users/"+user.ID, strings.NewReader(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"active","value":false}]}`))
	patch.Header.Set("Authorization", "Bearer token-a")
	patched := httptest.NewRecorder()
	handler.ServeHTTP(patched, patch)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patched.Code, patched.Body.String())
	}
	stale := httptest.NewRequest(http.MethodPatch, "/scim/v2/Users/"+user.ID, strings.NewReader(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"active","value":true}]}`))
	stale.Header.Set("Authorization", "Bearer token-a")
	stale.Header.Set("If-Match", user.Meta.Version)
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale patch status=%d body=%s", staleResponse.Code, staleResponse.Body.String())
	}
}
