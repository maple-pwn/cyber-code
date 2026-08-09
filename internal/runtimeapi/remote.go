package runtimeapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"cyber-code/internal/authorization"
)

var ErrRemoteCredentialRevoked = errors.New("remote credential revoked")

type RemoteClaims struct {
	Principal string
	TenantID  string
	SessionID string
	// Role is audit metadata. Capabilities are the per-request authorization boundary.
	Role          string
	TaskContextID string
	ControllerID  string
	Capabilities  []string
}

type RemoteAuthenticator interface {
	Authenticate(context.Context, string) (RemoteClaims, error)
}

type RemoteServerOptions struct {
	Service         *Service
	Authenticator   RemoteAuthenticator
	Workspace       string
	AllowedOrigins  []string
	MaxMessageBytes int
	Authorization   *authorization.Policy
}

type RemoteRequest struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Handshake   *HandshakeRequest `json:"handshake,omitempty"`
	Command     *CommandEnvelope  `json:"command,omitempty"`
	AfterCursor int               `json:"afterCursor,omitempty"`
}

type RemoteServer struct {
	service         *Service
	workspace       string
	authenticator   RemoteAuthenticator
	allowedOrigins  map[string]struct{}
	maxMessageBytes int
	authorization   *authorization.Policy

	mu       sync.Mutex
	runtimes map[string]*LocalServer
}

func NewRemoteServer(options RemoteServerOptions) (*RemoteServer, error) {
	if options.Service == nil || options.Authenticator == nil {
		return nil, fmt.Errorf("remote runtime service and authenticator are required")
	}
	if len(options.AllowedOrigins) == 0 {
		return nil, fmt.Errorf("remote runtime allowed origins are required")
	}
	origins := make(map[string]struct{}, len(options.AllowedOrigins))
	for _, origin := range options.AllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || !validRemoteOrigin(parsed) || parsed.User != nil ||
			(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid remote runtime origin")
		}
		origins[strings.TrimSuffix(parsed.String(), "/")] = struct{}{}
	}
	limit := options.MaxMessageBytes
	if limit == 0 {
		limit = defaultLocalMessageLimit
	}
	if limit < 1 {
		return nil, fmt.Errorf("remote runtime message limit must be positive")
	}
	return &RemoteServer{
		service: options.Service, workspace: options.Workspace, authenticator: options.Authenticator,
		allowedOrigins: origins, maxMessageBytes: limit,
		authorization: options.Authorization,
		runtimes:      make(map[string]*LocalServer),
	}, nil
}

func validRemoteOrigin(origin *url.URL) bool {
	if origin.Host == "" {
		return false
	}
	switch origin.Scheme {
	case "https":
		return true
	case "tauri":
		return origin.Host == "localhost"
	case "http":
		return origin.Host == "tauri.localhost"
	default:
		return false
	}
}

func (s *RemoteServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.TLS == nil {
		s.writeError(writer, http.StatusUpgradeRequired, "", "tls_required")
		return
	}
	origin := strings.TrimSuffix(request.Header.Get("Origin"), "/")
	if _, allowed := s.allowedOrigins[origin]; !allowed {
		s.writeError(writer, http.StatusForbidden, "", "origin_forbidden")
		return
	}
	writer.Header().Set("Access-Control-Allow-Origin", origin)
	writer.Header().Set("Vary", "Origin")
	if request.Method == http.MethodOptions {
		writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		writer.Header().Set("Access-Control-Allow-Methods", http.MethodPost)
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if request.Method != http.MethodPost {
		s.writeError(writer, http.StatusMethodNotAllowed, "", "method_not_allowed")
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		s.writeError(writer, http.StatusUnsupportedMediaType, "", "content_type_required")
		return
	}
	token, ok := remoteBearer(request.Header.Get("Authorization"))
	if !ok {
		s.writeError(writer, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	claims, err := s.authenticator.Authenticate(request.Context(), token)
	if err != nil || !validText(claims.Principal) || !validText(claims.Role) ||
		!validRuntimeIdentifier(claims.TaskContextID) || !validRuntimeIdentifier(claims.ControllerID) ||
		!validStrings(claims.Capabilities) || claims.Principal != s.service.principal {
		s.writeError(writer, http.StatusUnauthorized, "", "unauthorized")
		return
	}

	var remoteRequest RemoteRequest
	reader := http.MaxBytesReader(writer, request.Body, int64(s.maxMessageBytes))
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&remoteRequest); err != nil {
		var limitError *http.MaxBytesError
		if errors.As(err, &limitError) {
			s.writeError(writer, http.StatusRequestEntityTooLarge, remoteRequest.ID, "request_too_large")
			return
		}
		s.writeError(writer, http.StatusBadRequest, remoteRequest.ID, "invalid_request")
		return
	}
	if !validText(remoteRequest.ID) {
		s.writeError(writer, http.StatusBadRequest, remoteRequest.ID, "invalid_request")
		return
	}
	if s.authorization != nil {
		capability, ok := authorizedCapability(remoteRequest)
		if !ok || !validRuntimeIdentifier(claims.TenantID) || !validRuntimeIdentifier(claims.SessionID) {
			s.writeError(writer, http.StatusForbidden, remoteRequest.ID, "tenant_access_denied")
			return
		}
		if capability != "" {
			if err := s.authorization.Authorize(authorization.Request{TenantID: claims.TenantID, Principal: claims.Principal, SessionID: claims.SessionID, Capability: capability}); err != nil {
				s.writeError(writer, http.StatusForbidden, remoteRequest.ID, err.Error())
				return
			}
		}
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		var limitError *http.MaxBytesError
		if errors.As(err, &limitError) {
			s.writeError(writer, http.StatusRequestEntityTooLarge, remoteRequest.ID, "request_too_large")
			return
		}
		s.writeError(writer, http.StatusBadRequest, remoteRequest.ID, "invalid_request")
		return
	}
	if capability := remoteCapability(remoteRequest.Type); capability != "" && !slices.Contains(claims.Capabilities, capability) {
		s.writeError(writer, http.StatusForbidden, remoteRequest.ID, "capability_lost")
		return
	}
	runtime, err := s.runtimeFor(claims)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, remoteRequest.ID, "runtime_unavailable")
		return
	}

	if remoteRequest.Type == "handshake" {
		if remoteRequest.Handshake == nil {
			s.writeError(writer, http.StatusBadRequest, remoteRequest.ID, "invalid_handshake")
			return
		}
		capabilities := SelectCapabilities(remoteRequest.Handshake.SupportedCapabilities, claims.Capabilities)
		handshake := HandshakeResponse{
			ProtocolVersion: ProtocolVersion, RuntimeID: s.service.runtimeID,
			Principal: claims.Principal, Role: claims.Role, Capabilities: capabilities,
			Source: SourceMetadata{
				Mode: SourceModeRemote, RuntimeID: s.service.runtimeID, Principal: claims.Principal,
				Capabilities: append([]string(nil), capabilities...),
			},
		}
		if _, err := NegotiateHandshake(*remoteRequest.Handshake, handshake); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, ErrIncompatible) {
				status = http.StatusUpgradeRequired
			}
			s.writeError(writer, status, remoteRequest.ID, err.Error())
			return
		}
		s.writeJSON(writer, http.StatusOK, LocalResponse{ID: remoteRequest.ID, Type: "handshake", Handshake: &handshake})
		return
	}

	response := runtime.handleAuthorizedForClient(request.Context(), LocalRequest{
		ID: remoteRequest.ID, Type: remoteRequest.Type, Handshake: remoteRequest.Handshake,
		Command: remoteRequest.Command, AfterCursor: remoteRequest.AfterCursor,
	}, claims.ControllerID)
	s.writeJSON(writer, http.StatusOK, response)
}

func (s *RemoteServer) runtimeFor(claims RemoteClaims) (*LocalServer, error) {
	digest := sha256.Sum256([]byte(claims.TaskContextID))
	contextKey := fmt.Sprintf("%x", digest[:8])
	s.mu.Lock()
	defer s.mu.Unlock()
	if runtime, exists := s.runtimes[contextKey]; exists {
		return runtime, nil
	}
	runtime, err := newRuntimeServer(LocalServerOptions{
		Service: s.service, Bearer: "internal-remote-dispatch", Role: claims.Role,
		Workspace: s.workspace, MaxMessageBytes: s.maxMessageBytes,
		StateFileName: "remote-runtime-" + contextKey + ".json",
		TaskPrefix:    "task-" + contextKey + "-",
		Source: SourceMetadata{
			Mode: SourceModeRemote, RuntimeID: s.service.runtimeID, Principal: s.service.principal,
			Capabilities: []string{"events", "snapshot", "commands"},
		},
	}, SourceModeRemote)
	if err != nil {
		return nil, err
	}
	s.runtimes[contextKey] = runtime
	return runtime, nil
}

func (s *RemoteServer) writeError(writer http.ResponseWriter, status int, id, code string) {
	s.writeJSON(writer, status, LocalResponse{ID: id, Type: "error", ErrorCode: code})
}

func (s *RemoteServer) writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func remoteBearer(header string) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	return token, ok && scheme == "Bearer" && validText(token) && !strings.ContainsAny(token, " \t\r\n")
}

func remoteCapability(requestType string) string {
	switch requestType {
	case "events":
		return "events"
	case "snapshot":
		return "snapshot"
	case "command":
		return "commands"
	default:
		return ""
	}
}

func authorizedCapability(request RemoteRequest) (authorization.Capability, bool) {
	switch request.Type {
	case "handshake", "health", "close":
		return "", true
	case "events", "snapshot":
		return authorization.CapabilityEvidenceRead, true
	case "command":
		if request.Command == nil {
			return "", false
		}
		var command struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(request.Command.Command, &command) != nil {
			return "", false
		}
		switch command.Type {
		case "task.create":
			return authorization.CapabilityTaskCreate, true
		case "scope.confirm":
			return authorization.CapabilityScopeConfirm, true
		case "approval.respond":
			return authorization.CapabilityApproval, true
		case "control.take":
			return authorization.CapabilityControlTake, true
		default:
			return authorization.CapabilityTaskCreate, true
		}
	default:
		return "", false
	}
}
