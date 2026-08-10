package scim

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const maxSCIMBodyBytes int64 = 1 << 20

type Identity struct {
	TenantID  string
	Principal string
}

type Authenticator interface {
	Authenticate(context.Context, string) (Identity, error)
}

type StaticAuthenticator struct{ credentials map[string]Identity }

func NewStaticAuthenticator(credentials map[string]Identity) *StaticAuthenticator {
	copy := make(map[string]Identity, len(credentials))
	for token, identity := range credentials {
		copy[token] = identity
	}
	return &StaticAuthenticator{credentials: copy}
}

func (authenticator *StaticAuthenticator) Authenticate(_ context.Context, token string) (Identity, error) {
	if authenticator == nil {
		return Identity{}, ErrUnauthorized
	}
	for candidate, identity := range authenticator.credentials {
		if len(candidate) == len(token) && subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1 && validSCIMText(identity.TenantID) && validSCIMText(identity.Principal) {
			return identity, nil
		}
	}
	return Identity{}, ErrUnauthorized
}

type HandlerOptions struct {
	Enabled       bool
	Service       *Service
	Authenticator Authenticator
}

type Handler struct{ options HandlerOptions }

func NewHandler(options HandlerOptions) *Handler { return &Handler{options: options} }

func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/scim+json")
	if handler == nil || !handler.options.Enabled {
		http.NotFound(writer, request)
		return
	}
	identity, err := handler.authenticate(request)
	if err != nil {
		handler.writeError(writer, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	if handler.options.Service == nil {
		handler.writeError(writer, http.StatusServiceUnavailable, "", "SCIM service unavailable")
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/scim/v2/Users")
	if path == request.URL.Path || (path != "" && !strings.HasPrefix(path, "/")) {
		http.NotFound(writer, request)
		return
	}
	id := strings.TrimPrefix(path, "/")
	switch request.Method {
	case http.MethodPost:
		if id != "" {
			http.NotFound(writer, request)
			return
		}
		var input User
		if !handler.decode(writer, request, &input) {
			return
		}
		if !containsSchema(input.Schemas, UserSchema) {
			handler.writeError(writer, http.StatusBadRequest, "invalidValue", "SCIM User schema is required")
			return
		}
		user, err := handler.options.Service.Create(identity.TenantID, identity.Principal, input)
		if err != nil {
			handler.serviceError(writer, err)
			return
		}
		writer.Header().Set("Location", user.Meta.Location)
		writer.Header().Set("ETag", user.Meta.Version)
		handler.writeJSON(writer, http.StatusCreated, user)
	case http.MethodGet:
		if id != "" {
			user, err := handler.options.Service.Get(identity.TenantID, id)
			if err != nil {
				handler.serviceError(writer, err)
				return
			}
			writer.Header().Set("ETag", user.Meta.Version)
			handler.writeJSON(writer, http.StatusOK, user)
			return
		}
		start, _ := strconv.Atoi(request.URL.Query().Get("startIndex"))
		count, _ := strconv.Atoi(request.URL.Query().Get("count"))
		users, total, err := handler.options.Service.List(identity.TenantID, request.URL.Query().Get("filter"), start, count)
		if err != nil {
			handler.serviceError(writer, err)
			return
		}
		if start <= 0 {
			start = 1
		}
		handler.writeJSON(writer, http.StatusOK, ListResponse{Schemas: []string{ListSchema}, TotalResults: total, StartIndex: start, ItemsPerPage: len(users), Resources: users})
	case http.MethodPatch:
		if id == "" {
			http.NotFound(writer, request)
			return
		}
		var input PatchRequest
		if !handler.decode(writer, request, &input) {
			return
		}
		if len(input.Operations) == 0 {
			handler.writeError(writer, http.StatusBadRequest, "invalidValue", "SCIM patch operations are required")
			return
		}
		if !containsSchema(input.Schemas, PatchSchema) {
			handler.writeError(writer, http.StatusBadRequest, "invalidValue", "SCIM PatchOp schema is required")
			return
		}
		user, err := handler.options.Service.PatchAtVersion(identity.TenantID, identity.Principal, id, request.Header.Get("If-Match"), input.Operations)
		if err != nil {
			handler.serviceError(writer, err)
			return
		}
		writer.Header().Set("ETag", user.Meta.Version)
		handler.writeJSON(writer, http.StatusOK, user)
	case http.MethodDelete:
		if id == "" {
			http.NotFound(writer, request)
			return
		}
		if err := handler.options.Service.DeleteAtVersion(identity.TenantID, identity.Principal, id, request.Header.Get("If-Match")); err != nil {
			handler.serviceError(writer, err)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	default:
		writer.Header().Set("Allow", "GET, POST, PATCH, DELETE")
		handler.writeError(writer, http.StatusMethodNotAllowed, "", "method not allowed")
	}
}

func (handler *Handler) authenticate(request *http.Request) (Identity, error) {
	const prefix = "Bearer "
	value := request.Header.Get("Authorization")
	if !strings.HasPrefix(value, prefix) || handler.options.Authenticator == nil {
		return Identity{}, ErrUnauthorized
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	if token == "" || len(token) > 4096 {
		return Identity{}, ErrUnauthorized
	}
	return handler.options.Authenticator.Authenticate(request.Context(), token)
}

func (handler *Handler) decode(writer http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxSCIMBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		handler.writeError(writer, http.StatusBadRequest, "invalidValue", "invalid SCIM request")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		handler.writeError(writer, http.StatusBadRequest, "invalidValue", "invalid SCIM request")
		return false
	}
	return true
}

func (handler *Handler) serviceError(writer http.ResponseWriter, err error) {
	if errors.Is(err, ErrConflict) {
		handler.writeError(writer, http.StatusPreconditionFailed, "mutability", "resource revision conflict")
		return
	}
	if errors.Is(err, ErrNotFound) {
		handler.writeError(writer, http.StatusNotFound, "", "resource not found")
		return
	}
	handler.writeError(writer, http.StatusBadRequest, "invalidValue", "invalid SCIM request")
}

func (handler *Handler) writeError(writer http.ResponseWriter, status int, scimType, detail string) {
	handler.writeJSON(writer, status, ErrorResponse{Schemas: []string{ErrorSchema}, Status: strconv.Itoa(status), ScimType: scimType, Detail: detail})
}

func (handler *Handler) writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func containsSchema(schemas []string, expected string) bool {
	for _, schema := range schemas {
		if schema == expected {
			return true
		}
	}
	return false
}
