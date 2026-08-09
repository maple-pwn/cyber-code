package runtimeapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"cyber-code/internal/authorization"
)

var teamRequestSequence uint64

// NewTeamHandler composes the runtime and organization administration APIs
// without allowing path-prefix fallthrough between their authorization domains.
func NewTeamHandler(runtimeHandler, adminHandler http.Handler) (http.Handler, error) {
	return NewObservedTeamHandler(runtimeHandler, adminHandler, nil)
}

func NewObservedTeamHandler(runtimeHandler, adminHandler http.Handler, observer TeamObserver) (http.Handler, error) {
	return NewObservedTeamHandlerWithSCIM(runtimeHandler, adminHandler, nil, observer)
}

// NewObservedTeamHandlerWithSCIM mounts provisioning only when an explicitly
// enabled SCIM handler is supplied by the deployment.
func NewObservedTeamHandlerWithSCIM(runtimeHandler, adminHandler, scimHandler http.Handler, observer TeamObserver) (http.Handler, error) {
	if runtimeHandler == nil || adminHandler == nil {
		return nil, fmt.Errorf("runtime and admin handlers are required")
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		statusWriter := &teamStatusWriter{ResponseWriter: writer, status: http.StatusOK}
		writer = statusWriter
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		requestID := strings.TrimSpace(request.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 || strings.ContainsAny(requestID, "\r\n") {
			requestID = "team-" + strconv.FormatUint(atomic.AddUint64(&teamRequestSequence, 1), 10)
		}
		writer.Header().Set("X-Request-ID", requestID)
		if observer != nil {
			defer func() {
				observer.Observe(TeamObservation{RequestID: requestID, Path: request.URL.Path, Method: request.Method, Status: statusWriter.status, Duration: time.Since(started)})
			}()
		}
		switch request.URL.Path {
		case "/healthz", "/readyz":
			if request.Method != http.MethodGet {
				writer.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"ok":true}` + "\n"))
		case "/runtime":
			runtimeHandler.ServeHTTP(writer, request)
		case "/admin":
			adminHandler.ServeHTTP(writer, request)
		default:
			if scimHandler != nil && strings.HasPrefix(request.URL.Path, "/scim/v2/") {
				scimHandler.ServeHTTP(writer, request)
				return
			}
			http.NotFound(writer, request)
		}
	}), nil
}

type teamStatusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *teamStatusWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *teamStatusWriter) Write(data []byte) (int, error) {
	return writer.ResponseWriter.Write(data)
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
	return NewObservedTeamHandlerForRemote(runtime, service, authenticator, nil)
}

func NewObservedTeamHandlerForRemote(runtime *RemoteServer, service *authorization.AdminService, authenticator RemoteAuthenticator, observer TeamObserver) (http.Handler, error) {
	if runtime == nil || service == nil {
		return nil, fmt.Errorf("remote runtime and admin service are required")
	}
	adminAuthenticator, err := NewRemoteAdminAuthenticator(authenticator)
	if err != nil {
		return nil, err
	}
	return NewObservedTeamHandler(runtime, authorization.NewAdminHandler(service, adminAuthenticator), observer)
}
