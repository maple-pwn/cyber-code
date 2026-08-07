package runtimeapi

import (
	"fmt"
	"net/http"
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
