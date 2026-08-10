package runtimeapi

import (
	"context"
	"fmt"
)

// StaticRemoteAuthenticator is a deliberately small deployment authenticator
// for local smoke tests and air-gapped installations. Production deployments
// should replace it with an OIDC-backed implementation.
type StaticRemoteAuthenticator struct {
	token  string
	claims RemoteClaims
}

func NewStaticRemoteAuthenticator(token string, claims RemoteClaims) (*StaticRemoteAuthenticator, error) {
	if !validText(token) || !validText(claims.Principal) || !validRuntimeIdentifier(claims.TenantID) ||
		!validRuntimeIdentifier(claims.SessionID) || !validRuntimeIdentifier(claims.TaskContextID) || !validRuntimeIdentifier(claims.ControllerID) {
		return nil, fmt.Errorf("static remote credentials are invalid")
	}
	return &StaticRemoteAuthenticator{token: token, claims: claims}, nil
}

func (a *StaticRemoteAuthenticator) Authenticate(_ context.Context, token string) (RemoteClaims, error) {
	if a == nil || token == "" || token != a.token {
		return RemoteClaims{}, ErrRemoteCredentialRevoked
	}
	claims := a.claims
	claims.Capabilities = append([]string(nil), a.claims.Capabilities...)
	return claims, nil
}
