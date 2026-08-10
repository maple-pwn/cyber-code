package scim

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"cyber-code/internal/authorization"
)

type Service struct {
	policy *authorization.Policy
}

func NewService(policy *authorization.Policy) *Service {
	return &Service{policy: policy}
}

func (service *Service) Create(tenantID, actor string, input User) (User, error) {
	if service == nil || service.policy == nil || !validSCIMText(tenantID) || !validSCIMText(actor) || !validSCIMText(input.UserName) {
		return User{}, ErrInvalid
	}
	role := input.Role
	if role == "" {
		role = authorization.RoleViewer
	}
	active := true
	if input.Active != nil {
		active = *input.Active
	}
	externalID := strings.TrimSpace(input.ExternalID)
	displayName := strings.TrimSpace(input.DisplayName)
	for _, existing := range service.policy.Members(tenantID) {
		if externalID != "" && existing.ExternalID == externalID && existing.Principal != input.UserName {
			return User{}, ErrConflict
		}
		if existing.Principal == input.UserName && existing.ExternalID == externalID && existing.DisplayName == displayName && existing.Role == role && existing.Active == active {
			return service.user(existing), nil
		}
	}
	member, err := service.policy.ProvisionMember(tenantID, actor, input.UserName, externalID, displayName, role, active)
	if err != nil {
		return User{}, ErrInvalid
	}
	return service.user(member), nil
}

func (service *Service) Get(tenantID, id string) (User, error) {
	for _, member := range service.policy.Members(tenantID) {
		if scimID(tenantID, member.Principal) == id {
			return service.user(member), nil
		}
	}
	return User{}, ErrNotFound
}

func (service *Service) List(tenantID, filter string, startIndex, count int) ([]User, int, error) {
	if startIndex <= 0 {
		startIndex = 1
	}
	if count <= 0 {
		count = 100
	}
	if count > 200 {
		count = 200
	}
	attribute, expected, err := parseFilter(filter)
	if err != nil {
		return nil, 0, err
	}
	users := make([]User, 0)
	for _, member := range service.policy.Members(tenantID) {
		if attribute == "userName" && member.Principal != expected || attribute == "externalId" && member.ExternalID != expected {
			continue
		}
		users = append(users, service.user(member))
	}
	sort.Slice(users, func(i, j int) bool { return users[i].UserName < users[j].UserName })
	total := len(users)
	start := startIndex - 1
	if start >= total {
		return []User{}, total, nil
	}
	end := start + count
	if end > total {
		end = total
	}
	return users[start:end], total, nil
}

func (service *Service) Patch(tenantID, actor, id string, operations []PatchOperation) (User, error) {
	return service.patch(tenantID, actor, id, "", operations)
}

func (service *Service) PatchAtVersion(tenantID, actor, id, version string, operations []PatchOperation) (User, error) {
	return service.patch(tenantID, actor, id, version, operations)
}

func (service *Service) patch(tenantID, actor, id, version string, operations []PatchOperation) (User, error) {
	user, err := service.Get(tenantID, id)
	if err != nil {
		return User{}, err
	}
	for _, operation := range operations {
		if !strings.EqualFold(operation.Operation, "replace") {
			return User{}, ErrInvalid
		}
		switch strings.ToLower(strings.TrimSpace(operation.Path)) {
		case "active":
			value, ok := operation.Value.(bool)
			if !ok {
				return User{}, ErrInvalid
			}
			user.Active = &value
		case "displayname":
			value, ok := operation.Value.(string)
			if !ok {
				return User{}, ErrInvalid
			}
			user.DisplayName = value
		case "role":
			value, ok := operation.Value.(string)
			if !ok {
				return User{}, ErrInvalid
			}
			user.Role = authorization.Role(value)
		default:
			return User{}, ErrInvalid
		}
	}
	if version != "" && version != user.Meta.Version {
		return User{}, ErrConflict
	}
	revision, err := revisionFromVersion(user.Meta.Version)
	if err != nil {
		return User{}, ErrConflict
	}
	active := user.Active != nil && *user.Active
	member, err := service.policy.ProvisionMemberAtRevision(tenantID, actor, user.UserName, user.ExternalID, user.DisplayName, user.Role, active, revision)
	if err != nil {
		if err == authorization.ErrRevisionConflict {
			return User{}, ErrConflict
		}
		return User{}, ErrInvalid
	}
	return service.user(member), nil
}

func (service *Service) Delete(tenantID, actor, id string) error {
	return service.DeleteAtVersion(tenantID, actor, id, "")
}

func (service *Service) DeleteAtVersion(tenantID, actor, id, version string) error {
	user, err := service.Get(tenantID, id)
	if err != nil {
		return err
	}
	if version != "" && version != user.Meta.Version {
		return ErrConflict
	}
	revision, err := revisionFromVersion(user.Meta.Version)
	if err != nil {
		return ErrConflict
	}
	_, err = service.policy.ProvisionMemberAtRevision(tenantID, actor, user.UserName, user.ExternalID, user.DisplayName, user.Role, false, revision)
	if err == authorization.ErrRevisionConflict {
		return ErrConflict
	}
	return err
}

func (service *Service) user(member authorization.Member) User {
	active := member.Active
	id := scimID(member.TenantID, member.Principal)
	return User{Schemas: []string{UserSchema}, ID: id, ExternalID: member.ExternalID, UserName: member.Principal, DisplayName: member.DisplayName, Active: &active, Role: member.Role, Meta: Meta{ResourceType: "User", Created: member.ProvisionedAt, LastModified: member.UpdatedAt, Version: `W/"` + strconv.FormatUint(member.Revision, 10) + `"`, Location: "/scim/v2/Users/" + id}}
}

func revisionFromVersion(version string) (uint64, error) {
	if !strings.HasPrefix(version, `W/"`) || !strings.HasSuffix(version, `"`) {
		return 0, ErrConflict
	}
	value := strings.TrimSuffix(strings.TrimPrefix(version, `W/"`), `"`)
	return strconv.ParseUint(value, 10, 64)
}

func scimID(tenantID, principal string) string {
	digest := sha256.Sum256([]byte(tenantID + "\x00" + principal))
	return hex.EncodeToString(digest[:16])
}

func parseFilter(filter string) (string, string, error) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return "", "", nil
	}
	parts := strings.SplitN(filter, " eq ", 2)
	if len(parts) != 2 || (parts[0] != "userName" && parts[0] != "externalId") {
		return "", "", ErrInvalid
	}
	value, err := strconv.Unquote(strings.TrimSpace(parts[1]))
	if err != nil || !validSCIMText(value) {
		return "", "", ErrInvalid
	}
	return parts[0], value, nil
}

func validSCIMText(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n")
}
