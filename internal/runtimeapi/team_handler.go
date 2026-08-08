package runtimeapi

import (
	"context"
	"fmt"
	"net/http"

	"cyber-code/internal/authorization"
)

// NewTeamHandler composes the runtime and organization administration APIs
// without allowing path-prefix fallthrough between their authorization domains.
func NewTeamHandler(runtimeHandler, adminHandler http.Handler) (http.Handler, error) {
	if runtimeHandler == nil || adminHandler == nil {
		return nil, fmt.Errorf("runtime and admin handlers are required")
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		switch request.URL.Path {
		case "/runtime":
			runtimeHandler.ServeHTTP(writer, request)
		case "/admin":
			adminHandler.ServeHTTP(writer, request)
		default:
			http.NotFound(writer, request)
		}
	}), nil
}

// RemoteAdminAuthenticator transports authenticated runtime claims into the
// server-side organization policy. Policy checks still run for every request,
// so revocation, expiry, and role changes take effect immediately.
type RemoteAdminAuthenticator struct{ delegate RemoteAuthenticator }

func NewRemoteAdminAuthenticator(delegate RemoteAuthenticator) (*RemoteAdminAuthenticator, error) {
	if delegate == nil {
		return nil, fmt.Errorf("remote authenticator is required")
	}
	return &RemoteAdminAuthenticator{delegate: delegate}, nil
}

func (a *RemoteAdminAuthenticator) Authenticate(ctx context.Context, token string) (authorization.Request, error) {
	if a == nil || a.delegate == nil {
		return authorization.Request{}, fmt.Errorf("remote authenticator is unavailable")
	}
	claims, err := a.delegate.Authenticate(ctx, token)
	if err != nil {
		return authorization.Request{}, err
	}
	if !validText(claims.Principal) || !validRuntimeIdentifier(claims.TenantID) || !validRuntimeIdentifier(claims.SessionID) {
		return authorization.Request{}, fmt.Errorf("invalid authenticated claims")
	}
	return authorization.Request{TenantID: claims.TenantID, Principal: claims.Principal, SessionID: claims.SessionID}, nil
}

// NewTeamHandlerForRemote wires the runtime and tenant administration APIs to
// one authenticator and one policy-backed admin service.
func NewTeamHandlerForRemote(runtime *RemoteServer, service *authorization.AdminService, authenticator RemoteAuthenticator) (http.Handler, error) {
	if runtime == nil || service == nil {
		return nil, fmt.Errorf("remote runtime and admin service are required")
	}
	adminAuthenticator, err := NewRemoteAdminAuthenticator(authenticator)
	if err != nil {
		return nil, err
	}
	return NewTeamHandler(runtime, authorization.NewAdminHandler(service, adminAuthenticator))
}
