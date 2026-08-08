package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type AdminAuthenticator interface {
	Authenticate(context.Context, string) (Request, error)
}

type AdminHandler struct {
	service       *AdminService
	authenticator AdminAuthenticator
}

func NewAdminHandler(service *AdminService, authenticator AdminAuthenticator) *AdminHandler {
	return &AdminHandler{service: service, authenticator: authenticator}
}

type adminRequest struct {
	Action     string      `json:"action"`
	Principal  string      `json:"principal,omitempty"`
	Role       Role        `json:"role,omitempty"`
	Invitation *Invitation `json:"invitation,omitempty"`
	SessionID  string      `json:"sessionId,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	Until      time.Time   `json:"until,omitempty"`
}

func (h *AdminHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	if request.Method != http.MethodPost {
		writeAdminError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	token, ok := adminBearer(request.Header.Get("Authorization"))
	if !ok || h.authenticator == nil {
		writeAdminError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}
	actor, err := h.authenticator.Authenticate(request.Context(), token)
	if err != nil {
		writeAdminError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}
	var input adminRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeAdminError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeAdminError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if h.service == nil {
		writeAdminError(writer, http.StatusInternalServerError, "admin_unavailable")
		return
	}
	switch input.Action {
	case "invite":
		if input.Invitation == nil {
			writeAdminError(writer, http.StatusBadRequest, "invitation_required")
			return
		}
		err = h.service.Invite(actor, *input.Invitation)
	case "accept_invitation":
		if input.Invitation == nil {
			writeAdminError(writer, http.StatusBadRequest, "invitation_required")
			return
		}
		err = h.service.AcceptInvitation(actor, input.Invitation.ID)
	case "role":
		err = h.service.UpdateRole(actor, input.Principal, input.Role)
	case "revoke":
		err = h.service.RevokeMember(actor, input.Principal)
	case "members":
		var members []Member
		members, err = h.service.Members(actor)
		if err == nil {
			writeAdminJSON(writer, http.StatusOK, map[string]any{"ok": true, "members": members})
		}
		if err == nil {
			return
		}
	case "invitations":
		var invitations []Invitation
		invitations, err = h.service.Invitations(actor)
		if err == nil {
			writeAdminJSON(writer, http.StatusOK, map[string]any{"ok": true, "invitations": invitations})
		}
		if err == nil {
			return
		}
	case "audit":
		var decisions []Decision
		decisions, err = h.service.Audit(actor)
		if err == nil {
			writeAdminJSON(writer, http.StatusOK, map[string]any{"ok": true, "decisions": decisions})
		}
		if err == nil {
			return
		}
	case "emergency_grant":
		err = h.service.GrantEmergencyAccess(actor, input.SessionID, input.Reason, input.Until)
	case "emergency_revoke":
		err = h.service.RevokeEmergencyAccess(actor, input.SessionID)
	default:
		writeAdminError(writer, http.StatusBadRequest, "unknown_action")
		return
	}
	if err != nil {
		writeAdminError(writer, adminStatus(err), err.Error())
		return
	}
	writeAdminJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func adminBearer(value string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	return token, token != ""
}
func adminStatus(err error) int {
	if errors.Is(err, ErrTenantDenied) || errors.Is(err, ErrMembershipRevoked) || errors.Is(err, ErrCapabilityDenied) ||
		errors.Is(err, ErrStepUpRequired) || errors.Is(err, ErrSessionMissing) || errors.Is(err, ErrSessionRevoked) || errors.Is(err, ErrSessionExpired) {
		return http.StatusForbidden
	}
	return http.StatusBadRequest
}
func writeAdminError(writer http.ResponseWriter, status int, code string) {
	writeAdminJSON(writer, status, map[string]any{"ok": false, "error": code})
}
func writeAdminJSON(writer http.ResponseWriter, status int, value any) {
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
