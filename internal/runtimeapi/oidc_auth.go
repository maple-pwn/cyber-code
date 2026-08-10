package runtimeapi

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type OIDCIdentity struct {
	Principal     string
	TenantID      string
	SessionID     string
	Role          string
	TaskContextID string
	ControllerID  string
	Capabilities  []string
	ExpiresAt     time.Time
}

type OIDCKeyFunc func(context.Context, string, string) (crypto.PublicKey, error)

type OIDCRemoteAuthenticator struct {
	issuer   string
	audience string
	key      OIDCKeyFunc
	clock    func() time.Time
}

func NewOIDCRemoteAuthenticator(issuer, audience string, key OIDCKeyFunc) (*OIDCRemoteAuthenticator, error) {
	parsed, err := url.Parse(strings.TrimSpace(issuer))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || key == nil || strings.TrimSpace(audience) == "" {
		return nil, fmt.Errorf("OIDC issuer, audience, and key resolver are required")
	}
	return &OIDCRemoteAuthenticator{issuer: strings.TrimRight(parsed.String(), "/"), audience: audience, key: key, clock: time.Now}, nil
}

// NewDiscoveredOIDCRemoteAuthenticator wires a bounded discovery/JWKS resolver
// into the remote authenticator.
func NewDiscoveredOIDCRemoteAuthenticator(audience string, options OIDCJWKResolverOptions) (*OIDCRemoteAuthenticator, error) {
	resolver, err := NewOIDCJWKResolver(options)
	if err != nil {
		return nil, err
	}
	return NewOIDCRemoteAuthenticator(resolver.issuer, audience, resolver.Resolve)
}

func (authenticator *OIDCRemoteAuthenticator) Authenticate(ctx context.Context, token string) (RemoteClaims, error) {
	identity, err := authenticator.Verify(ctx, token)
	if err != nil {
		return RemoteClaims{}, err
	}
	return RemoteClaims{
		Principal: identity.Principal, TenantID: identity.TenantID, SessionID: identity.SessionID,
		Role: identity.Role, TaskContextID: identity.TaskContextID, ControllerID: identity.ControllerID,
		Capabilities: append([]string(nil), identity.Capabilities...),
	}, nil
}

func (authenticator *OIDCRemoteAuthenticator) Verify(ctx context.Context, token string) (OIDCIdentity, error) {
	if authenticator == nil || authenticator.key == nil {
		return OIDCIdentity{}, errors.New("OIDC authenticator unavailable")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return OIDCIdentity{}, errors.New("invalid OIDC token")
	}
	decode := func(value string, target any) error {
		data, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil {
			return err
		}
		if base64.RawURLEncoding.EncodeToString(data) != value {
			return errors.New("non-canonical base64url")
		}
		return json.Unmarshal(data, target)
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	var claims struct {
		Issuer       string          `json:"iss"`
		Audience     json.RawMessage `json:"aud"`
		Subject      string          `json:"sub"`
		Expires      int64           `json:"exp"`
		TenantID     string          `json:"tenant_id"`
		SessionID    string          `json:"sid"`
		Role         string          `json:"role"`
		TaskContext  string          `json:"task_context_id"`
		Controller   string          `json:"controller_id"`
		Capabilities []string        `json:"capabilities"`
	}
	if err := decode(parts[0], &header); err != nil {
		return OIDCIdentity{}, errors.New("invalid OIDC token")
	}
	if err := decode(parts[1], &claims); err != nil {
		return OIDCIdentity{}, errors.New("invalid OIDC token")
	}
	if header.Algorithm != "RS256" && header.Algorithm != "EdDSA" {
		return OIDCIdentity{}, errors.New("unsupported OIDC token algorithm")
	}
	key, err := authenticator.key(ctx, header.KeyID, header.Algorithm)
	if err != nil {
		return OIDCIdentity{}, errors.New("OIDC signing key unavailable")
	}
	signingInput := []byte(parts[0] + "." + parts[1])
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || base64.RawURLEncoding.EncodeToString(signature) != parts[2] || !verifyOIDCSignature(header.Algorithm, key, signingInput, signature) {
		return OIDCIdentity{}, errors.New("invalid OIDC token signature")
	}
	if claims.Issuer != authenticator.issuer || claims.Expires <= authenticator.clock().Unix() {
		return OIDCIdentity{}, errors.New("OIDC token issuer or expiry is invalid")
	}
	if !audienceContains(claims.Audience, authenticator.audience) {
		return OIDCIdentity{}, errors.New("OIDC token audience is invalid")
	}
	identity := OIDCIdentity{Principal: claims.Subject, TenantID: claims.TenantID, SessionID: claims.SessionID, Role: claims.Role, TaskContextID: claims.TaskContext, ControllerID: claims.Controller, Capabilities: claims.Capabilities, ExpiresAt: time.Unix(claims.Expires, 0).UTC()}
	if !validText(identity.Principal) || !validRuntimeIdentifier(identity.TenantID) || !validRuntimeIdentifier(identity.SessionID) || !validRuntimeIdentifier(identity.TaskContextID) || !validRuntimeIdentifier(identity.ControllerID) || !validStrings(identity.Capabilities) {
		return OIDCIdentity{}, errors.New("OIDC identity claims are invalid")
	}
	return identity, nil
}

func verifyOIDCSignature(algorithm string, key crypto.PublicKey, input, signature []byte) bool {
	switch algorithm {
	case "EdDSA":
		publicKey, ok := key.(ed25519.PublicKey)
		return ok && ed25519.Verify(publicKey, input, signature)
	case "RS256":
		publicKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return false
		}
		digest := sha256.Sum256(input)
		return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature) == nil
	default:
		return false
	}
}

func audienceContains(raw json.RawMessage, expected string) bool {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == expected
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return false
	}
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
