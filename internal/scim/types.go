package scim

import (
	"errors"
	"time"

	"cyber-code/internal/authorization"
)

const (
	UserSchema  = "urn:ietf:params:scim:schemas:core:2.0:User"
	PatchSchema = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	ListSchema  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	ErrorSchema = "urn:ietf:params:scim:api:messages:2.0:Error"
)

var (
	ErrNotFound     = errors.New("SCIM resource not found")
	ErrInvalid      = errors.New("invalid SCIM request")
	ErrUnauthorized = errors.New("SCIM bearer is unauthorized")
	ErrConflict     = errors.New("SCIM resource revision conflict")
)

type Meta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created,omitempty"`
	LastModified time.Time `json:"lastModified,omitempty"`
	Version      string    `json:"version,omitempty"`
	Location     string    `json:"location,omitempty"`
}

type User struct {
	Schemas     []string           `json:"schemas"`
	ID          string             `json:"id,omitempty"`
	ExternalID  string             `json:"externalId,omitempty"`
	UserName    string             `json:"userName"`
	DisplayName string             `json:"displayName,omitempty"`
	Active      *bool              `json:"active,omitempty"`
	Role        authorization.Role `json:"role,omitempty"`
	Meta        Meta               `json:"meta"`
}

type PatchOperation struct {
	Operation string `json:"op"`
	Path      string `json:"path"`
	Value     any    `json:"value"`
}

type PatchRequest struct {
	Schemas    []string         `json:"schemas"`
	Operations []PatchOperation `json:"Operations"`
}

type ListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []User   `json:"Resources"`
}

type ErrorResponse struct {
	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
	Detail   string   `json:"detail"`
}
